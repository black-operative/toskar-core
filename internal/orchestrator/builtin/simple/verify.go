package simple

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/yeixio/toskar-core/internal/huginn"
	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// Events for answer checks (spec §24).
const (
	EventVerifying = "chat.verifying"
	EventVerified  = "verify.done"
	// EventUnconfirmedAction says the answer claims a change no tool made.
	EventUnconfirmedAction = "verify.action"
	// EventCodeChecked reports the answer's code blocks parsed (#111).
	EventCodeChecked = "verify.code"
	// EventConsistency reports the answer checked for contradictions.
	EventConsistency = "verify.consistency"
	// EventLinksChecked reports the answer's links checked against the
	// sources.
	EventLinksChecked = "verify.links"
)

// checkLinks finds the answer's links to pages no source gave, which a
// model writes from memory and which are often wrong or gone. They go back
// to the model once per correction, and the revision is kept when it has
// fewer; those left are reported for the answer's notice. The check needs
// no model and makes no request.
func checkLinks(ctx context.Context, env pluginapi.ExecutionEnvironment, role string, messages []pluginapi.ChatMessage, answer, evidence string, corrections int) string {
	links := huginn.Links(answer)
	if len(links) == 0 {
		return answer
	}
	sources := []string{evidence}
	for _, m := range messages {
		sources = append(sources, m.Content)
	}
	issues := huginn.UngroundedLinks(answer, sources...)
	remaining := issues
	for pass := 0; pass < corrections && len(remaining) > 0 && ctx.Err() == nil; pass++ {
		env.Emit(EventVerifying, map[string]any{"issues": len(remaining)})
		ask := append(append([]pluginapi.ChatMessage(nil), messages...),
			pluginapi.ChatMessage{Role: "assistant", Content: answer},
			pluginapi.ChatMessage{Role: "user", Content: "These links in your answer are not in the reference material or the conversation, so they may not exist:\n" + huginn.DescribeLinks(remaining) +
				"Rewrite the whole answer for the user without guessed addresses: link only to pages from the reference material or the conversation, and otherwise name the site, or link its home page, and say what to look for there. Reply with the answer only."},
		)
		revised, _, err := generateText(ctx, env, role, ask)
		if err != nil {
			break
		}
		text := tools.ParseModelOutput(revised).Text
		if !substantial(text, answer) {
			break
		}
		again := huginn.UngroundedLinks(text, sources...)
		if len(again) >= len(remaining) {
			break
		}
		answer, remaining = text, again
	}
	env.Emit(EventLinksChecked, map[string]any{"links": len(links), "issues": len(issues), "fixed": len(issues) - len(remaining), "remaining": remaining})
	return answer
}

// checkCode parses the answer's code blocks without running them (#111).
// Code with syntax errors goes back to the model once per correction,
// with the errors named, and the revision is kept when it has fewer.
func checkCode(ctx context.Context, env pluginapi.ExecutionEnvironment, role string, messages []pluginapi.ChatMessage, answer string, corrections int) string {
	blocks := huginn.CodeBlocks(answer)
	if len(blocks) == 0 {
		return answer
	}
	issues := huginn.CheckCode(ctx, answer)
	remaining := issues
	for pass := 0; pass < corrections && len(remaining) > 0 && ctx.Err() == nil; pass++ {
		env.Emit(EventVerifying, map[string]any{"issues": len(remaining)})
		ask := append(append([]pluginapi.ChatMessage(nil), messages...),
			pluginapi.ChatMessage{Role: "assistant", Content: answer},
			pluginapi.ChatMessage{Role: "user", Content: "The code in your answer does not parse:\n" + huginn.DescribeCode(remaining) +
				"Fix the code and rewrite the whole answer for the user, keeping everything else. Reply with the answer only."},
		)
		revised, _, err := generateText(ctx, env, role, ask)
		if err != nil {
			break
		}
		text := tools.ParseModelOutput(revised).Text
		if !substantial(text, answer) || len(huginn.CodeBlocks(text)) == 0 {
			break
		}
		again := huginn.CheckCode(ctx, text)
		if len(again) >= len(remaining) {
			break
		}
		answer, remaining = text, again
	}
	texts := make([]string, 0, len(remaining))
	for _, is := range remaining {
		texts = append(texts, is.Text())
	}
	env.Emit(EventCodeChecked, map[string]any{"blocks": len(blocks), "issues": len(issues), "fixed": len(issues) - len(remaining), "remaining": texts})
	return answer
}

// minConsistencyLen is the shortest answer checked for contradictions; a
// short reply has too little to contradict.
const minConsistencyLen = 400

// checkConsistency asks the reviewer whether the answer contradicts itself
// or the reference material (#111), and when it does, has the answer
// rewritten once with the contradictions named. It costs model calls, so
// only Thorough effort runs it.
func checkConsistency(ctx context.Context, env pluginapi.ExecutionEnvironment, role string, messages []pluginapi.ChatMessage, answer, evidence string) string {
	if utf8.RuneCountInString(answer) < minConsistencyLen {
		return answer
	}
	found := contradictions(ctx, env, role, answer, evidence)
	fixed := 0
	if len(found) > 0 && ctx.Err() == nil {
		env.Emit(EventVerifying, map[string]any{"issues": len(found)})
		ask := append(append([]pluginapi.ChatMessage(nil), messages...),
			pluginapi.ChatMessage{Role: "assistant", Content: answer},
			pluginapi.ChatMessage{Role: "user", Content: "Your answer contradicts itself or the reference material:\n- " + strings.Join(found, "\n- ") +
				"\nRewrite the whole answer so it is consistent, following the reference material where there is any. Reply with the answer only."},
		)
		if revised, _, err := generateText(ctx, env, role, ask); err == nil {
			text := tools.ParseModelOutput(revised).Text
			if substantial(text, answer) {
				again := contradictions(ctx, env, role, text, evidence)
				if len(again) < len(found) {
					answer, fixed = text, len(found)-len(again)
					found = again
				}
			}
		}
	}
	env.Emit(EventConsistency, map[string]any{"found": len(found) + fixed, "fixed": fixed, "remaining": found})
	return answer
}

// contradictions asks the reviewer for the answer's contradictions. An
// answer it can't read counts as none, so a check never blocks an answer.
func contradictions(ctx context.Context, env pluginapi.ExecutionEnvironment, role, answer, evidence string) []string {
	prompt := "Check this answer for contradictions: statements in it that contradict each other"
	if evidence != "" {
		prompt += ", or that contradict the reference material"
	}
	prompt += ". Differences in wording, and details the reference material doesn't cover, are not contradictions. " +
		"Reply only with JSON: {\"contradictions\": [\"short description\", ...]}, an empty list when there are none.\n\nAnswer:\n" + answer
	if evidence != "" {
		prompt += "\n\nReference material:\n" + evidence
	}
	raw, _, err := generateText(ctx, env, role, []pluginapi.ChatMessage{{Role: "user", Content: prompt}})
	if err != nil {
		return nil
	}
	var out struct {
		Contradictions []string `json:"contradictions"`
	}
	start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if start < 0 || end <= start || json.Unmarshal([]byte(raw[start:end+1]), &out) != nil {
		return nil
	}
	var list []string
	for _, c := range out.Contradictions {
		if c = strings.TrimSpace(c); c != "" && len(list) < 5 {
			list = append(list, c)
		}
	}
	return list
}

// verifyAnswer checks an answer's arithmetic and, when the turn used
// reference material or tool results, that its figures appear in them. The
// check needs no model; only an answer with issues is sent back to the model
// once, with the issues named, and the revision is kept if it has fewer.
// Figures that still do not check out are reported for the answer's note.
func verifyAnswer(ctx context.Context, env pluginapi.ExecutionEnvironment, role string, messages []pluginapi.ChatMessage, answer, evidence, prompt string, corrections int) string {
	if !huginn.HasFigures(answer) {
		return answer
	}
	issues := huginn.Check(answer, evidence, prompt)
	if len(issues) == 0 {
		if evidence != "" {
			env.Emit(EventVerified, map[string]any{"issues": 0})
		}
		return answer
	}
	remaining := issues
	// Each pass sends the issues back once; a revision is kept only when it
	// fixes some and is still a real answer (§24). Fast skips the passes and
	// only reports what did not check out.
	for pass := 0; pass < corrections && len(remaining) > 0 && ctx.Err() == nil; pass++ {
		env.Emit(EventVerifying, map[string]any{"issues": len(remaining)})
		ask := append(append([]pluginapi.ChatMessage(nil), messages...),
			pluginapi.ChatMessage{Role: "assistant", Content: answer},
			pluginapi.ChatMessage{Role: "user", Content: "Check your answer against the reference material. Problems:\n" + huginn.Describe(remaining) +
				"Rewrite the whole answer for the user. Use only figures from the reference material, or calculations done correctly from them. " +
				"If a figure is not in the reference material, say it is not there instead of guessing. Reply with the answer only."},
		)
		revised, _, err := generateText(ctx, env, role, ask)
		if err != nil {
			break
		}
		// A revision must still be an answer: one that fixes figures by
		// dropping most of the reply is not kept.
		text := tools.ParseModelOutput(revised).Text
		if !substantial(text, answer) {
			break
		}
		again := huginn.Check(text, evidence, prompt)
		if len(again) >= len(remaining) {
			break
		}
		answer, remaining = text, again
	}
	env.Emit(EventVerified, map[string]any{
		"issues":    len(issues),
		"fixed":     len(issues) - len(remaining),
		"remaining": huginn.Figures(remaining),
		// figures lets the app write the list in the App language.
		"figures": figureTexts(remaining),
	})
	return answer
}

func substantial(revised, original string) bool {
	n := utf8.RuneCountInString(strings.TrimSpace(revised))
	return n >= 20 && n*10 >= utf8.RuneCountInString(original)*4
}

// toolNameRe matches tool ids, and their capability aliases, written out in
// an answer's text.
var toolNameRe = regexp.MustCompile(`\b(internet\.(search|open)|filesystem\.(read|write|search)|git\.(status|diff|log|show|add|commit|push)|files?\.(create|read|write|search)|web\.(search|open|fetch)|shell\.(run|execute)|tool_call)\b`)

// looseCallRe finds a call written as prose, such as files.search {"query":"x"}.
var looseCallRe = regexp.MustCompile(`([a-z]+\.[a-z_]+)\s*(\{[^{}]*\})`)

// looseToolCall reads a short answer that is only a tool call written as
// prose, with the id and its JSON arguments, as the call it meant. It is
// accepted only for an offered tool, so it cannot reach anything else.
func looseToolCall(content string, profile contracts.AIProfile) (*tools.ModelCall, bool) {
	if utf8.RuneCountInString(content) > 600 {
		return nil, false
	}
	m := looseCallRe.FindStringSubmatch(content)
	if m == nil {
		return nil, false
	}
	id := tools.Canonical(m[1])
	if !toolEnabled(profile, id) {
		return nil, false
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(m[2]), &args); err != nil {
		return nil, false
	}
	return &tools.ModelCall{ID: id, Args: args}, true
}

// narratesTools reports an answer that describes tool calls, such as
// "use internet.search to find the price", instead of answering.
func narratesTools(answer string) bool {
	if toolNameRe.MatchString(answer) {
		return true
	}
	// Connected services' tools, such as homeassistant.states.
	for _, def := range tools.ConnectedDefinitions() {
		if strings.Contains(answer, def.ID) {
			return true
		}
	}
	return false
}

func figureTexts(issues []huginn.Issue) []string {
	out := make([]string, 0, len(issues))
	for _, is := range issues {
		out = append(out, is.Text)
	}
	return out
}
