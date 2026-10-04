package muninn

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// history builds n exchanges of about size characters each.
func history(n, size int) []contracts.Message {
	var out []contracts.Message
	start := time.Unix(1_700_000_000, 0)
	for i := 0; i < n; i++ {
		out = append(out,
			contracts.Message{ID: fmt.Sprintf("u%d", i), Role: "user", Content: fmt.Sprintf("question %d %s", i, strings.Repeat("q", size)), CreatedAt: start.Add(time.Duration(2*i) * time.Second)},
			contracts.Message{ID: fmt.Sprintf("a%d", i), Role: "assistant", Content: fmt.Sprintf("answer %d %s", i, strings.Repeat("a", size)), CreatedAt: start.Add(time.Duration(2*i+1) * time.Second)},
		)
	}
	return out
}

func asChat(stored []contracts.Message) []pluginapi.ChatMessage {
	out := make([]pluginapi.ChatMessage, 0, len(stored))
	for _, m := range stored {
		out = append(out, pluginapi.ChatMessage{Role: m.Role, Content: m.Content})
	}
	return out
}

func TestPlanLeavesShortConversationsAlone(t *testing.T) {
	if _, needed := Plan(history(3, 100), 8192, nil, nil); needed {
		t.Fatal("a short conversation does not need a summary")
	}
}

func TestPlanSummarizesOlderTurnsAndKeepsRecentOnes(t *testing.T) {
	stored := history(20, 1000) // ~10k estimated tokens against an 8k window
	through, needed := Plan(stored, 8192, nil, nil)
	if !needed {
		t.Fatal("expected a summary")
	}
	if stored[through].Role != "assistant" {
		t.Fatalf("summary should end on an answer, ended on %s", stored[through].Role)
	}
	kept := size(nil, stored[through+1:])
	if kept > tokens(8192, keepRecent) || kept == 0 {
		t.Fatalf("kept %d tokens verbatim", kept)
	}
	// The same plan is not repeated once its summary exists.
	if _, again := Plan(stored, 8192, &Summary{ThroughMessageID: stored[through].ID}, nil); again {
		t.Fatal("re-planned an existing summary")
	}
}

func TestSummarizerRunsFromSourceAndIsApplied(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	conv := "c1"
	stored := history(20, 1000)
	var prompts [][]pluginapi.ChatMessage
	sum := &Summarizer{Store: s, ModelID: "m", Generate: func(ctx context.Context, msgs []pluginapi.ChatMessage) (string, error) {
		prompts = append(prompts, msgs)
		return "The user asked 15 numbered questions; the assistant answered each.", nil
	}}
	// conversation_summaries references conversations.
	if _, err := s.db.Exec(`INSERT INTO conversations (id, title, created_at, updated_at) VALUES (?, 't', '2026-01-01', '2026-01-01')`, conv); err != nil {
		t.Fatal(err)
	}
	ran, err := sum.Run(ctx, conv, stored, 8192)
	if err != nil || !ran {
		t.Fatalf("run = %v %v", ran, err)
	}
	// The history is longer than one call can read, so it is summarized in
	// chunks; the first starts at the beginning and later ones fold in.
	if len(prompts) < 2 {
		t.Fatalf("calls = %d", len(prompts))
	}
	if !strings.Contains(prompts[0][0].Content, "Keep facts, decisions") || !strings.HasPrefix(prompts[0][1].Content, "Conversation:\n\nUser: question 0") {
		t.Fatalf("first prompt = %.120q", prompts[0][1].Content)
	}
	if !strings.HasPrefix(prompts[1][1].Content, "Earlier summary:\nThe user asked") {
		t.Fatalf("second prompt = %.120q", prompts[1][1].Content)
	}
	got, ok, err := s.GetSummary(ctx, conv)
	if err != nil || !ok || got.MessageCount == 0 || got.ModelID != "m" {
		t.Fatalf("summary = %+v %v %v", got, ok, err)
	}

	// Applied: the summary replaces the covered messages; roles alternate.
	msgs, summarized := WithSummary(asChat(stored), stored, &got)
	if summarized != got.MessageCount {
		t.Fatalf("summarized %d, want %d", summarized, got.MessageCount)
	}
	if msgs[0].Role != "user" || !strings.Contains(msgs[0].Content, "15 numbered questions") || msgs[1].Role != "assistant" || msgs[2].Role != "user" {
		t.Fatalf("assembled = %+v", msgs[:3])
	}
	if len(msgs) != len(stored)-summarized+2 {
		t.Fatalf("assembled %d messages from %d saved", len(msgs), len(stored))
	}
	// Nothing to do on the next turn until more history piles up.
	if ran, _ := sum.Run(ctx, conv, stored, 8192); ran {
		t.Fatal("summarized twice")
	}
}

func TestChunksKeepEveryMessage(t *testing.T) {
	msgs := history(10, 100)
	got := chunks(msgs, 125, nil)
	total := 0
	for _, c := range got {
		if size(nil, c) > 125 {
			t.Fatalf("chunk of %d tokens", size(nil, c))
		}
		total += len(c)
	}
	if total != len(msgs) {
		t.Fatalf("chunks hold %d of %d messages", total, len(msgs))
	}
}

func TestWithSummaryIgnoresAStaleSummary(t *testing.T) {
	stored := history(2, 10)
	msgs, n := WithSummary(asChat(stored), stored, &Summary{ThroughMessageID: "gone", Text: "x"})
	if n != 0 || len(msgs) != len(stored) {
		t.Fatal("a summary of deleted messages must be ignored")
	}
}

// wordCounter counts one token per word, unlike the four-characters
// estimate, so tests can tell which one was used.
func wordCounter(text string) (int, bool) { return len(strings.Fields(text)), true }

func TestPlanCountsWithTheModelsTokenizer(t *testing.T) {
	// About 10k estimated tokens, but each message is only a few words: the
	// tokenizer's count is what decides.
	stored := history(20, 1000)
	if _, needed := Plan(stored, 8192, nil, nil); !needed {
		t.Fatal("the estimate should call for a summary")
	}
	if _, needed := Plan(stored, 8192, nil, wordCounter); needed {
		t.Fatal("the tokenizer's count fits the window; no summary needed")
	}
}

func TestChunksCutLongMessagesByTokens(t *testing.T) {
	long := contracts.Message{Role: "user", Content: strings.Repeat("word ", 100)}
	got := chunks([]contracts.Message{long}, 10, wordCounter)
	if len(got) != 1 {
		t.Fatalf("%d chunks", len(got))
	}
	if n, _ := wordCounter(got[0][0].Content); n > 11 {
		t.Fatalf("cut to %d words", n)
	}
}
