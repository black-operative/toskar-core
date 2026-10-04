package contextusage

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

func TestMeasureScalesToRuntimeTokens(t *testing.T) {
	usage := Measure(nil, "instructions", "tools", []pluginapi.ChatMessage{
		{Role: "system", Content: "ignored because the pieces are passed separately"},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "call"},
		{Role: "user", Content: "Tool result for you, not for the user:\npage"},
	}, 100)
	sum := usage.Instructions + usage.Tools + usage.Conversation + usage.ToolResults
	if sum != 100 || usage.PromptTokens != 100 || usage.Estimated {
		t.Fatalf("usage=%+v sum=%d", usage, sum)
	}
	if usage.ToolResults == 0 || usage.Instructions == 0 || usage.Tools == 0 || usage.Conversation == 0 {
		t.Fatalf("missing section: %+v", usage)
	}
}

func TestMeasureEstimatesWithoutRuntimeTokens(t *testing.T) {
	usage := Measure(nil, "1234", "", []pluginapi.ChatMessage{{Role: "user", Content: "12345678"}}, 0)
	if !usage.Estimated || usage.Instructions != 1 || usage.Conversation != 2 || usage.PromptTokens != 3 {
		t.Fatalf("%+v", usage)
	}
}

func TestWithoutCurrentTurnKeepsEarlierCopy(t *testing.T) {
	stored := []contracts.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
		{Role: "user", Content: "hello"},
	}
	got := WithoutCurrentTurn(stored, "hello")
	if len(got) != 2 || got[0].Content != "hello" || got[1].Content != "hi" {
		t.Fatalf("%+v", got)
	}
}

func TestFitPriorDropsOldest(t *testing.T) {
	prior := []pluginapi.ChatMessage{
		{Role: "user", Content: "aaaa"},
		{Role: "assistant", Content: "bbbb"},
		{Role: "user", Content: "cccc"},
	}
	got := FitPrior(prior, 2, nil)
	if len(got) != 2 || got[0].Content != "bbbb" || got[1].Content != "cccc" {
		t.Fatalf("%+v", got)
	}
	if FitPrior(prior, 0, nil) != nil {
		t.Fatal("no room should keep nothing")
	}
}

// words counts one token per word, unlike the estimate.
func words(text string) (int, bool) { return len(strings.Fields(text)), true }

func TestMeasureCountsWithTheTokenizer(t *testing.T) {
	usage := Measure(words, "be brief", "", []pluginapi.ChatMessage{{Role: "user", Content: "what is the weather"}}, 0)
	if usage.Estimated || usage.Instructions != 2 || usage.Conversation != 4 || usage.PromptTokens != 6 {
		t.Fatalf("%+v", usage)
	}
}

func TestMeasureCountsTheTemplateAsInstructions(t *testing.T) {
	// 6 tokens counted; the runtime saw 20, the rest being the chat template.
	usage := Measure(words, "be brief", "", []pluginapi.ChatMessage{{Role: "user", Content: "what is the weather"}}, 20)
	if usage.Estimated || usage.Instructions != 16 || usage.Conversation != 4 || usage.PromptTokens != 20 {
		t.Fatalf("%+v", usage)
	}
}

func TestMeasureIsEstimatedWhenTheTokenizerFails(t *testing.T) {
	failed := func(text string) (int, bool) { return Estimate(text), false }
	usage := Measure(failed, "1234", "", []pluginapi.ChatMessage{{Role: "user", Content: "12345678"}}, 0)
	if !usage.Estimated || usage.PromptTokens != 3 {
		t.Fatalf("%+v", usage)
	}
}

func TestRoomAndFitCountTokens(t *testing.T) {
	// 600 tokens of window: 512 for the reply, 3 for the reserved text.
	if got := Room(600, words, "one two three"); got != 85 {
		t.Fatalf("room %d", got)
	}
	if got := Room(700, nil, strings.Repeat("x", 400)); got != 700-512-100 {
		t.Fatalf("estimated room %d", got)
	}
	if got := Room(600, nil, strings.Repeat("x", 400)); got != 0 {
		t.Fatalf("a full window has no room, got %d", got)
	}
	prior := []pluginapi.ChatMessage{
		{Role: "user", Content: strings.Repeat("long ", 50)},
		{Role: "assistant", Content: "a short answer"},
		{Role: "user", Content: "and another question"},
	}
	got := FitPrior(prior, 10, words)
	if len(got) != 2 || got[0].Content != "a short answer" {
		t.Fatalf("%+v", got)
	}
}

// The estimate is what the gauge shows when no tokenizer is reachable, so it
// should be near real tokenizers on the text people send: about a token per
// CJK character, more than four characters a token for code and JSON, and
// unchanged for plain English (#230).
func TestEstimateByScript(t *testing.T) {
	japanese := "今日はとても良い天気なので、公園まで散歩に行きました。" // 27 characters
	if n := Estimate(japanese); n < 24 || n > 30 {
		t.Errorf("Japanese: %d tokens for %d characters, want about one each", n, utf8.RuneCountInString(japanese))
	}
	korean := "오늘은 날씨가 좋아서 공원에 산책을 갔어요."
	if n := Estimate(korean); n < 15 {
		t.Errorf("Korean: %d tokens, want at least one per syllable", n)
	}
	json := `{"model":"gemma-4-e4b","messages":[{"role":"user","content":"hi"}],"stream":true}`
	if n, old := Estimate(json), len(json)/4; n <= old || n > len(json)/2 {
		t.Errorf("JSON: %d tokens for %d characters, want more than %d and at most %d", n, len(json), old, len(json)/2)
	}
	prose := strings.Repeat("The quick brown fox jumps over the lazy dog and runs into the forest ", 6)
	if n, old := Estimate(prose), utf8.RuneCountInString(prose)/4; n < old || n > old+old/10 {
		t.Errorf("English: %d tokens, want close to the old estimate %d", n, old)
	}
}
