package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/locale"
	"github.com/yeixio/toskar-core/internal/structured"
	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// "Notify on change" (#204): the run's executor fingerprints what its tools
// read, and when that and the values can't tell whether the result changed,
// asks the run's model, which is still loaded, to compare it with the last
// one.

// sourceRecorder fingerprints what an automation run's read-only tools
// returned, in the order they returned it.
type sourceRecorder struct {
	mu   sync.Mutex
	hash []byte
	seen bool
}

func (s *sourceRecorder) add(toolID string, args, result map[string]any) {
	def, ok := tools.Lookup(toolID)
	if !ok || def.Risk != tools.RiskRead {
		return
	}
	raw, err := json.Marshal(map[string]any{"tool": toolID, "args": args, "result": result})
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	h := sha256.New()
	h.Write(s.hash)
	h.Write(raw)
	s.hash, s.seen = h.Sum(nil), true
}

func (s *sourceRecorder) sum() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.seen {
		return ""
	}
	return hex.EncodeToString(s.hash)
}

var changeSchema = &structured.Schema{
	Type:     "object",
	Required: []string{"changed"},
	Properties: map[string]*structured.Schema{
		"changed": {Type: "boolean"},
		"what":    {Type: "string"},
	},
}

// judgeChange asks the run's model whether its result differs from the
// previous one in a way the person would care about, beyond rewording. It
// returns nil when the model gives no usable answer; the text decides then.
func (e automationExecutor) judgeChange(ctx context.Context, env *automationEnv, automation automations.Automation, previous, current string) *automations.Change {
	role := "assistant"
	if roles := env.base.profile.Roles; len(roles) > 0 && roles[0].Role != "" {
		role = roles[0].Role
	}
	lang := e.app.replyLanguage(ctx, "", automation.Prompt, automation.ResponseLanguage).Tag
	ask := []pluginapi.ChatMessage{
		{Role: "system", Content: "You compare two results of the same scheduled task, run at different times. Say whether anything the person would care about changed: a new or removed item, a different number, price, status, date, or answer. Different wording, order, or detail about the same facts is not a change."},
		{Role: "user", Content: "Earlier result:\n<<<\n" + previous + "\n>>>\n\nNew result:\n<<<\n" + current + "\n>>>\n\n" +
			`Reply with only this JSON: {"changed": true or false, "what": "if it changed, one short sentence saying what changed, in ` + locale.LanguageName(lang, locale.Source) + `"}`},
	}
	reply, err := collectText(ctx, env.base, role, ask)
	if err != nil {
		return nil
	}
	parsed := structured.Parse(reply, changeSchema)
	if !parsed.OK() {
		return nil
	}
	obj, _ := parsed.Value.(map[string]any)
	changed, ok := obj["changed"].(bool)
	if !ok {
		return nil
	}
	what, _ := obj["what"].(string)
	return &automations.Change{Changed: changed, What: strings.TrimSpace(what)}
}
