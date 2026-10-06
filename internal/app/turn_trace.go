package app

import (
	"encoding/json"
	"net/url"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/huginn"
	"github.com/yeixio/toskar-core/internal/locale"
	"github.com/yeixio/toskar-core/internal/mimir"
	"github.com/yeixio/toskar-core/internal/muninn"
	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Limits keep an answer's source list readable.
const (
	maxTurnSources = 8
	maxSnippetRune = 240
)

// turnTrace records what one chat turn drew on and did, so the answer can
// show its sources and a plain-language summary of the work. It also notes
// when the turn has taken in untrusted content (§58).
type turnTrace struct {
	mu        sync.Mutex
	sources   []contracts.Citation
	steps     []contracts.ActivityStep
	files     []contracts.FileRef
	notice    string
	untrusted bool
	// ownFiles is set when the answer read files attached to or made in
	// this chat.
	ownFiles bool
	// runID links the answer to its run trace (§35).
	runID string
	// lang is the App language notices are written in (multilingual spec
	// §16); "" is English.
	lang string
}

// noticeText is a chat:notices key's text in the App language.
func (t *turnTrace) noticeText(key string, params map[string]any) string {
	return locale.T(t.lang, "chat:notices."+key, params)
}

func (t *turnTrace) addSource(c contracts.Citation) {
	key := c.Kind + "|" + c.URL + "|" + c.Source + "|" + c.Title
	for _, s := range t.sources {
		if s.Kind+"|"+s.URL+"|"+s.Source+"|"+s.Title == key {
			return
		}
	}
	if len(t.sources) < maxTurnSources {
		t.sources = append(t.sources, c)
	}
}

func (t *turnTrace) addStep(kind, text string) {
	t.steps = append(t.steps, contracts.ActivityStep{Kind: kind, Text: text})
}

// step adds a chat:steps key's text in the App language.
func (t *turnTrace) step(kind, key string, params map[string]any) {
	t.addStep(kind, locale.T(t.lang, "chat:steps."+key, params))
}

// knowledge records passages retrieved from Mimir.
func (t *turnTrace) knowledge(hits []mimir.Hit) {
	if len(hits) == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.untrusted = true
	names := []string{}
	seen := map[string]bool{}
	for _, h := range hits {
		t.addSource(contracts.Citation{Kind: "knowledge", Title: h.Title, Source: h.SourceName, Snippet: snippet(h.Body)})
		if !seen[h.SourceName] {
			seen[h.SourceName] = true
			names = append(names, h.SourceName)
		}
	}
	params := map[string]any{"count": len(hits), "source": locale.T(t.lang, "chat:status.yourKnowledge", nil)}
	switch len(names) {
	case 0:
		t.step("knowledge", "passages", params)
	case 1, 2:
		params["source"] = listIn(t.lang, names)
		t.step("knowledge", "passages", params)
	default:
		params["source"], params["more"] = names[0], len(names)-1
		t.step("knowledge", "passagesMore", params)
	}
}

// attachment records a file the user attached that the model read.
func (t *turnTrace) attachment(a artifacts.Artifact, picked, total int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.untrusted = true
	if picked < total {
		t.step("file", "readParts", map[string]any{"count": picked, "name": a.Name})
	} else {
		t.step("file", "read", map[string]any{"name": a.Name})
	}
	t.ownFiles = true
	source := "attachedFile"
	if a.Producer == artifacts.ProducerAssistant {
		source = "madeInChat"
	}
	t.addSource(contracts.Citation{Kind: "file", Title: a.Name, Source: locale.T(t.lang, "chat:answer."+source, nil), ArtifactID: a.ID})
}

// dataKind reports whether the answer drew on the user's own data: "file"
// for attached files, "knowledge" for connected knowledge, or "".
func (t *turnTrace) dataKind() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ownFiles {
		return "file"
	}
	for _, s := range t.sources {
		if s.Kind == "knowledge" {
			return "knowledge"
		}
	}
	return ""
}

// noticeIfNone sets the answer's notice unless one is already there, such
// as a note that a fallback model answered.
func (t *turnTrace) noticeIfNone(notice string) {
	if notice == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.notice == "" {
		t.notice = notice
	}
}

// planned records that a request was worked through in parts.
func (t *turnTrace) planned(parts int, parallel bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch {
	case parts == 1:
		t.step("plan", "plannedOne", nil)
	case parallel:
		t.step("plan", "plannedParallel", map[string]any{"count": parts})
	default:
		t.step("plan", "planned", map[string]any{"count": parts})
	}
}

// verified records an answer check (spec §24). Figures that could not be
// confirmed become the answer's notice.
func (t *turnTrace) verified(issues, fixed int, remaining []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch {
	case issues == 0:
		t.step("verify", "checkedFigures", nil)
	case fixed > 0 && len(remaining) == 0:
		t.step("verify", "correctedFigures", map[string]any{"count": fixed})
	default:
		t.step("verify", "figuresUnconfirmed", nil)
	}
	if len(remaining) > 0 && t.notice == "" {
		t.notice = t.noticeText("unconfirmedFigures", map[string]any{"figures": listIn(t.lang, remaining)})
	}
}

// codeChecked records the answer's code blocks parsed (#111): all fine,
// errors fixed, or errors left, which the notice lists.
func (t *turnTrace) codeChecked(issues, fixed int, remaining []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch {
	case issues == 0:
		t.step("verify", "checkedCode", nil)
	case len(remaining) == 0:
		t.step("verify", "correctedCode", map[string]any{"count": fixed})
	default:
		t.step("verify", "codeUnconfirmed", nil)
		if t.notice == "" {
			t.notice = t.noticeText("codeErrors", map[string]any{"errors": listIn(t.lang, remaining)})
		}
	}
}

// linksChecked records the answer's links checked against the sources:
// all from a source, guessed ones rewritten, or some left, which the
// notice lists so the person knows they may not work.
func (t *turnTrace) linksChecked(issues, fixed int, remaining []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch {
	case issues == 0:
		t.step("verify", "checkedLinks", nil)
	case len(remaining) == 0:
		t.step("verify", "correctedLinks", map[string]any{"count": fixed})
	default:
		t.step("verify", "linksUnconfirmed", nil)
		if t.notice == "" {
			t.notice = t.noticeText("unsourcedLinks", map[string]any{"links": listIn(t.lang, remaining)})
		}
	}
}

// consistencyChecked records the answer checked for contradictions.
func (t *turnTrace) consistencyChecked(found, fixed int, remaining []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch {
	case found == 0:
		t.step("verify", "checkedConsistency", nil)
	case len(remaining) == 0:
		t.step("verify", "resolvedContradictions", map[string]any{"count": fixed})
	default:
		t.step("verify", "contradictionsRemain", nil)
		if t.notice == "" {
			t.notice = t.noticeText("contradictions", map[string]any{"count": len(remaining)})
		}
	}
}

// unconfirmedAction records an answer that says it changed something when
// nothing that changes things ran. The person is told plainly.
func (t *turnTrace) unconfirmedAction() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.step("verify", "checkedAction", nil)
	t.notice = t.noticeText("nothingChanged", nil)
}

// stopped records that the user stopped the turn. kept says whether part
// of the answer was written and saved.
func (t *turnTrace) stopped(kept, timedOut bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if timedOut {
		// The profile's time limit ran out (§40).
		t.step("stop", "stoppedTimeLimit", nil)
		t.notice = t.noticeText("stoppedTimeLimit", nil)
		return
	}
	t.step("stop", "stoppedByYou", nil)
	if kept {
		t.notice = t.noticeText("stopped", nil)
	}
}

// effort records an effort the user chose. Auto's own choice is not listed.
func (t *turnTrace) effort(e huginn.Effort) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.step("effort", "effort", map[string]any{"effort": e.Describe(t.lang)})
}

// sharing records that other work, such as training, is using this computer.
func (t *turnTrace) sharing(text string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.addStep("share", text)
}

// routed records which model Auto chose and why.
func (t *turnTrace) routed(reason string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.addStep("route", reason)
}

// recovered records that another model answered after one failed. notice is
// shown with the answer when the change may affect it.
func (t *turnTrace) recovered(step, notice string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.addStep("recover", step)
	if notice != "" {
		t.notice = notice
	}
}

// hasSideEffects reports whether the turn changed something, such as a file
// or a commit, so it must not be run again.
func (t *turnTrace) hasSideEffects() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, s := range t.steps {
		switch s.Kind {
		case "write", "create", "command", "git", "stop":
			return true
		}
	}
	return false
}

// memories records persistent memories given to the model.
func (t *turnTrace) memories(list []muninn.Memory) {
	if len(list) == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, m := range list {
		t.addSource(contracts.Citation{Kind: "memory", Title: m.Content, Source: locale.T(t.lang, "chat:answer.memory", nil)})
	}
	t.step("memory", "memories", map[string]any{"count": len(list)})
}

// tool records a tool call that succeeded.
func (t *turnTrace) tool(toolID string, args, result map[string]any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	str := func(m map[string]any, k string) string {
		v, _ := m[k].(string)
		return strings.TrimSpace(v)
	}
	switch toolID {
	case "internet.search":
		t.untrusted = true
		t.step("search", "searchedWeb", map[string]any{"query": str(args, "query")})
		var rows []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Snippet string `json:"snippet"`
		}
		if raw, err := json.Marshal(result["results"]); err == nil {
			_ = json.Unmarshal(raw, &rows)
		}
		for i, r := range rows {
			if i == 3 || r.URL == "" {
				break
			}
			t.addSource(contracts.Citation{Kind: "web", Title: firstNonEmpty(r.Title, hostOf(r.URL)), URL: r.URL, Snippet: snippet(r.Snippet)})
		}
	case "internet.open":
		t.untrusted = true
		u, title := str(result, "url"), str(result, "title")
		if u == "" {
			u = str(args, "url")
		}
		if strings.EqualFold(title, "Untitled page") {
			title = ""
		}
		title = firstNonEmpty(title, hostOf(u))
		t.step("read", "readPage", map[string]any{"title": title})
		// A page that was read outranks the same page as a search hit.
		for i := range t.sources {
			if t.sources[i].Kind == "web" && t.sources[i].URL == u {
				t.sources[i].Title = title
				return
			}
		}
		t.addSource(contracts.Citation{Kind: "web", Title: title, URL: u, Snippet: snippet(str(result, "content"))})
	case "filesystem.read":
		t.untrusted = true
		p := str(args, "path")
		t.step("file", "read", map[string]any{"name": p})
		t.addSource(contracts.Citation{Kind: "file", Title: p, Source: p})
	case "filesystem.search":
		t.step("file", "searchedFiles", map[string]any{"query": str(args, "query")})
	case "filesystem.write":
		t.step("write", "saved", map[string]any{"path": str(args, "path")})
	case "files.create":
		name := str(result, "name")
		t.step("create", "created", map[string]any{"name": name})
		size, _ := result["size_bytes"].(int64)
		t.files = append(t.files, contracts.FileRef{
			ID: str(result, "id"), Name: name, MimeType: str(result, "mime_type"), Kind: str(result, "kind"),
			Size: size, Producer: "assistant",
		})
	case "terminal":
		t.untrusted = true
		t.step("command", "ranCommand", nil)
	case "browser.open", "browser.click", "browser.type", "browser.extract", "browser.download", "browser.screenshot":
		// Pages are written by other people, so they are data, not
		// instructions (§58).
		t.untrusted = true
		page := firstNonEmpty(str(result, "title"), hostOf(str(result, "url")), hostOf(str(args, "url")))
		switch toolID {
		case "browser.open":
			t.step("browser", "browserOpened", map[string]any{"page": page})
		case "browser.click":
			t.step("browser", "browserClicked", map[string]any{"page": page})
		case "browser.type":
			t.step("browser", "browserTyped", map[string]any{"page": page})
		case "browser.download":
			t.step("browser", "browserDownloaded", map[string]any{"name": str(result, "name")})
		case "browser.screenshot":
			t.step("browser", "browserScreenshot", nil)
		default:
			t.step("browser", "read", map[string]any{"name": page})
		}
	case "places.search", "places.details", "maps.route", "maps.distance":
		// Place names and details are written by map contributors, so they
		// are data, not instructions (§58).
		t.untrusted = true
		switch {
		case toolID == "places.search" && str(args, "near") != "":
			t.step("places", "placesNear", map[string]any{"query": str(args, "query"), "near": str(args, "near")})
		case toolID == "places.search":
			t.step("places", "placesSearch", map[string]any{"query": str(args, "query")})
		case toolID == "places.details":
			t.step("places", "placeDetails", nil)
		default:
			t.step("places", "route", map[string]any{"from": str(args, "from"), "to": str(args, "to")})
		}
	default:
		if strings.HasPrefix(toolID, "git.") {
			t.untrusted = true
			t.step("git", "git", map[string]any{"action": strings.TrimPrefix(toolID, "git.")})
			return
		}
		if def, ok := tools.Lookup(toolID); ok && (strings.HasPrefix(def.Source, "connector:") || strings.HasPrefix(def.Source, "mcp:")) {
			// What a connected service or tool source returns was written by
			// other people, so it is data, not instructions (§58).
			t.untrusted = true
			t.addStep("service", def.Name)
			t.serviceSources(result)
		}
	}
}

// serviceSources cites the pages a connected service's result links to,
// such as GitHub issues.
func (t *turnTrace) serviceSources(result map[string]any) {
	add := func(m map[string]any) {
		u, _ := m["url"].(string)
		if !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") {
			return
		}
		title, _ := m["title"].(string)
		t.addSource(contracts.Citation{Kind: "web", Title: firstNonEmpty(title, hostOf(u)), URL: u})
	}
	add(result)
	if list, ok := result["results"].([]any); ok {
		for i, item := range list {
			if m, ok := item.(map[string]any); ok && i < 5 {
				add(m)
			}
		}
	}
}

// effectivePolicy applies §58: after a turn has read untrusted content, a
// tool that changes something asks first even when the profile allows it.
// Read-only tools and Deny are unchanged.
func effectivePolicy(policy, toolID string, untrusted bool) string {
	if policy != tools.PolicyAllow || !untrusted {
		return policy
	}
	if def, ok := tools.Lookup(toolID); !ok || !tools.Contained(def.Risk) {
		return tools.PolicyAsk
	}
	return policy
}

func (t *turnTrace) sawUntrusted() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.untrusted
}

// meta returns what to store with the answer, or nil when nothing was used.
func (t *turnTrace) meta() *contracts.MessageMeta {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.sources) == 0 && len(t.steps) == 0 && t.notice == "" && len(t.files) == 0 && t.runID == "" {
		return nil
	}
	return &contracts.MessageMeta{
		Sources:  append([]contracts.Citation(nil), t.sources...),
		Steps:    append([]contracts.ActivityStep(nil), t.steps...),
		Notice:   t.notice,
		Files:    append([]contracts.FileRef(nil), t.files...),
		RunID:    t.runID,
		Contract: contracts.ContractVersion,
	}
}

func snippet(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= maxSnippetRune {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:maxSnippetRune])) + "…"
}

func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return strings.TrimPrefix(u.Host, "www.")
	}
	return raw
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// listIn joins items in lang, such as "20, 30 and $9".
func listIn(lang string, items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}
	first := strings.Join(items[:len(items)-1], locale.T(lang, "chat:notices.listSeparator", nil))
	return locale.T(lang, "chat:notices.listAnd", map[string]any{"first": first, "last": items[len(items)-1]})
}
