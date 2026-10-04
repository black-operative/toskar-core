package muninn

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/internal/contextusage"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// Summary covers a conversation's older messages, through one message. The
// messages themselves stay saved; the summary is only what the model sees
// in their place.
type Summary struct {
	ConversationID   string    `json:"conversation_id"`
	ThroughMessageID string    `json:"through_message_id"`
	ThroughCreatedAt time.Time `json:"through_created_at"`
	MessageCount     int       `json:"message_count"`
	Text             string    `json:"summary"`
	ModelID          string    `json:"model_id,omitempty"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// GetSummary returns a conversation's summary, if it has one.
func (s *Store) GetSummary(ctx context.Context, conversationID string) (Summary, bool, error) {
	var sum Summary
	var through, updated string
	var model sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT conversation_id, through_message_id, through_created_at, message_count, summary, model_id, updated_at
		FROM conversation_summaries WHERE conversation_id = ?`, conversationID).
		Scan(&sum.ConversationID, &sum.ThroughMessageID, &through, &sum.MessageCount, &sum.Text, &model, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Summary{}, false, nil
	}
	if err != nil {
		return Summary{}, false, err
	}
	sum.ThroughCreatedAt, sum.UpdatedAt, sum.ModelID = parseTS(through), parseTS(updated), model.String
	return sum, true, nil
}

// SaveSummary stores or replaces a conversation's summary.
func (s *Store) SaveSummary(ctx context.Context, sum Summary) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO conversation_summaries (conversation_id, through_message_id, through_created_at, message_count, summary, model_id, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(conversation_id) DO UPDATE SET through_message_id=excluded.through_message_id,
			through_created_at=excluded.through_created_at, message_count=excluded.message_count,
			summary=excluded.summary, model_id=excluded.model_id, updated_at=excluded.updated_at`,
		sum.ConversationID, sum.ThroughMessageID, ts(sum.ThroughCreatedAt), sum.MessageCount, sum.Text, nullable(sum.ModelID), ts(s.now()))
	return err
}

// Window shares for long conversations, as fractions of the model's window
// in tokens. Tokens are counted with the model's tokenizer when it runs
// here, and estimated otherwise (§66).
const (
	// summarizeAt starts summarizing when saved history passes this share.
	summarizeAt = 0.5
	// keepRecent is the share of newest messages always sent verbatim.
	keepRecent = 0.3
	// sourceFits is how much original text one summary call may read.
	sourceFits = 0.6
)

func tokens(window int, share float64) int { return int(float64(window) * share) }

func size(count contextusage.Counter, msgs []contracts.Message) int {
	n := 0
	for _, m := range msgs {
		c, _ := count.Count(m.Content)
		n += c
	}
	return n
}

// WithSummary replaces the messages a summary covers with the summary, as a
// user turn and a short acknowledgement so roles keep alternating. It
// returns the messages to send and how many saved messages were summarized.
func WithSummary(msgs []pluginapi.ChatMessage, stored []contracts.Message, sum *Summary) ([]pluginapi.ChatMessage, int) {
	if sum == nil || sum.Text == "" {
		return msgs, 0
	}
	cut := -1
	for i, m := range stored {
		if m.ID == sum.ThroughMessageID {
			cut = i
			break
		}
	}
	if cut < 0 {
		return msgs, 0
	}
	// msgs mirrors stored minus the current turn; drop the covered prefix.
	covered := 0
	for _, m := range stored[:cut+1] {
		if strings.TrimSpace(m.Content) != "" && (m.Role == "user" || m.Role == "assistant") {
			covered++
		}
	}
	if covered > len(msgs) {
		covered = len(msgs)
	}
	out := []pluginapi.ChatMessage{
		{Role: "user", Content: "Summary of the earlier part of this conversation (the full messages are saved):\n" + sum.Text},
		{Role: "assistant", Content: "Thanks. I have the earlier context."},
	}
	return append(out, msgs[covered:]...), cut + 1
}

// Plan decides whether a conversation needs a new summary, and through
// which message. It keeps the newest messages that fill keepRecent of the
// window verbatim and summarizes everything older.
// count is the model's tokenizer, or nil to estimate.
func Plan(stored []contracts.Message, windowTokens int, prev *Summary, count contextusage.Counter) (through int, needed bool) {
	if windowTokens <= 0 || len(stored) < 4 {
		return 0, false
	}
	if size(count, stored) <= tokens(windowTokens, summarizeAt) {
		return 0, false
	}
	keep, used := len(stored), 0
	for i := len(stored) - 1; i >= 0; i-- {
		n, _ := count.Count(stored[i].Content)
		if used+n > tokens(windowTokens, keepRecent) {
			break
		}
		used += n
		keep = i
	}
	// Start the verbatim part on a user message, so the summary ends on a
	// whole exchange without pushing the kept part past its budget.
	for keep < len(stored) && stored[keep].Role != "user" {
		keep++
	}
	through = keep - 1
	for through >= 0 && stored[through].Role != "assistant" {
		through--
	}
	if through < 1 {
		return 0, false
	}
	if prev != nil && prev.ThroughMessageID == stored[through].ID {
		return 0, false
	}
	return through, true
}

// Generate runs one non-streaming turn for the summarizer.
type Generate func(ctx context.Context, messages []pluginapi.ChatMessage) (string, error)

const summaryInstructions = "You summarize a conversation so an assistant can continue it later. " +
	"Keep facts, decisions, commitments, names, numbers, preferences, and open questions. " +
	"Write at most 200 words of plain sentences about what the user and the assistant said. " +
	"Do not add anything that was not said, and do not address the user."

func transcript(msgs []contracts.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		who := "User"
		if m.Role == "assistant" {
			who = "Assistant"
		}
		if strings.TrimSpace(m.Content) == "" {
			continue
		}
		fmt.Fprintf(&b, "%s: %s\n\n", who, strings.TrimSpace(m.Content))
	}
	return strings.TrimSpace(b.String())
}

// Summarizer keeps long conversations within the model's window.
type Summarizer struct {
	Store    *Store
	Generate Generate
	ModelID  string
	// Count is the model's tokenizer, or nil to estimate.
	Count contextusage.Counter

	running sync.Map
}

// RunWith runs job (a copy of s with its own Generate and ModelID) while
// using s to keep one run per conversation at a time.
func (s *Summarizer) RunWith(ctx context.Context, job *Summarizer, conversationID string, stored []contracts.Message, windowTokens int) (bool, error) {
	if _, busy := s.running.LoadOrStore(conversationID, true); busy {
		return false, nil
	}
	defer s.running.Delete(conversationID)
	return job.run(ctx, conversationID, stored, windowTokens)
}

// Run summarizes a conversation when Plan says it needs it. It reads the
// original messages when they fit, so summaries are regenerated from the
// source rather than summarizing summaries; only a very long history folds
// the previous summary in. One run per conversation at a time.
func (s *Summarizer) Run(ctx context.Context, conversationID string, stored []contracts.Message, windowTokens int) (bool, error) {
	return s.RunWith(ctx, s, conversationID, stored, windowTokens)
}

func (s *Summarizer) run(ctx context.Context, conversationID string, stored []contracts.Message, windowTokens int) (bool, error) {
	prev, ok, err := s.Store.GetSummary(ctx, conversationID)
	if err != nil {
		return false, err
	}
	var prevPtr *Summary
	if ok {
		prevPtr = &prev
	}
	through, needed := Plan(stored, windowTokens, prevPtr, s.Count)
	if !needed {
		return false, nil
	}
	source := stored[:through+1]
	// Read the original messages when they fit, so a summary is rebuilt from
	// the source rather than from a summary. A longer history is read in
	// chunks, each folded into the summary so far; nothing is dropped.
	start, text := 0, ""
	if prevPtr != nil && size(s.Count, source) > tokens(windowTokens, sourceFits) {
		for i, m := range source {
			if m.ID == prev.ThroughMessageID {
				start, text = i+1, prev.Text
			}
		}
	}
	for _, chunk := range chunks(source[start:], tokens(windowTokens, sourceFits), s.Count) {
		body := "Conversation:\n\n" + transcript(chunk)
		if text != "" {
			body = "Earlier summary:\n" + text + "\n\nConversation since then:\n\n" + transcript(chunk)
		}
		text, err = s.Generate(ctx, []pluginapi.ChatMessage{
			{Role: "system", Content: summaryInstructions},
			{Role: "user", Content: body},
		})
		if err != nil {
			return false, err
		}
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return false, fmt.Errorf("the model returned an empty summary")
	}
	last := source[len(source)-1]
	return true, s.Store.SaveSummary(ctx, Summary{ConversationID: conversationID, ThroughMessageID: last.ID,
		ThroughCreatedAt: last.CreatedAt, MessageCount: len(source), Text: text, ModelID: s.ModelID})
}

// chunks splits messages into runs of at most n tokens. A single message
// longer than n is cut to about its first n tokens.
func chunks(msgs []contracts.Message, n int, count contextusage.Counter) [][]contracts.Message {
	var out [][]contracts.Message
	var cur []contracts.Message
	used := 0
	for _, m := range msgs {
		size, _ := count.Count(m.Content)
		if size > n {
			// Cut in proportion to the message's own characters per token.
			r := []rune(m.Content)
			m.Content = string(r[:len(r)*n/size]) + "…"
			size = n
		}
		if used+size > n && len(cur) > 0 {
			out = append(out, cur)
			cur, used = nil, 0
		}
		cur = append(cur, m)
		used += size
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}
