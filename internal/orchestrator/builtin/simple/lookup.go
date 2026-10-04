package simple

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// Limits for what a lookup hands the model.
const (
	lookupResults   = 5
	lookupPageRunes = 6000
	lookupQueryMax  = 200
)

// EventLookup tells the UI that Yggdrasil is looking something up before the
// model answers.
const EventLookup = "chat.lookup"

var askPrefixRe = regexp.MustCompile(`(?i)^\s*(hey|hi|ok|okay|so|please|can you|could you|would you|will you|search (the web|online) for|look up|find out about|find out|tell me about|tell me|i want to know|i'd like to know)[\s,:]+`)

// lookupQuery turns a question into a search query: polite openers removed,
// length capped.
func lookupQuery(prompt string) string {
	q := strings.TrimSpace(prompt)
	for i := 0; i < 4; i++ {
		next := askPrefixRe.ReplaceAllString(q, "")
		if next == q {
			break
		}
		q = next
	}
	q = strings.TrimRight(strings.TrimSpace(q), "?!. ")
	if utf8.RuneCountInString(q) > lookupQueryMax {
		q = string([]rune(q)[:lookupQueryMax])
	}
	if q == "" {
		return strings.TrimSpace(prompt)
	}
	return q
}

// lookUpFirst searches the web, and reads the best page, before the model
// answers a question that needs current information (spec §21). Small models
// often pick the wrong tool or none; Yggdrasil decides instead. It runs only
// when the profile allows web search without asking, and returns the material
// to give the model as data.
func lookUpFirst(ctx context.Context, env pluginapi.ExecutionEnvironment, profile contracts.AIProfile, prompt string, pages int) (string, bool) {
	if !tools.MessageNeedsLiveWeb(prompt) {
		return "", false
	}
	return lookUp(ctx, env, profile, lookupQuery(prompt), prompt, pages)
}

// webAllowed reports whether the profile lets Yggdrasil search without asking.
func webAllowed(profile contracts.AIProfile) bool {
	return strings.EqualFold(tools.PolicyForProfile(profile, "internet.search"), tools.PolicyAllow)
}

// lookUp searches the web for query and reads the best pages, up to pages
// of them (0 uses the search results alone). prompt is the question the
// material is for.
func lookUp(ctx context.Context, env pluginapi.ExecutionEnvironment, profile contracts.AIProfile, query, prompt string, pages int) (string, bool) {
	if !webAllowed(profile) {
		return "", false
	}
	env.Emit(EventLookup, map[string]any{"query": query})
	args := map[string]any{"query": query}
	result, err := env.ExecuteTool(ctx, "internet.search", args)
	if err != nil {
		return "", false
	}
	var rows []struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Snippet string `json:"snippet"`
	}
	if raw, err := json.Marshal(result["results"]); err == nil {
		_ = json.Unmarshal(raw, &rows)
	}
	if len(rows) == 0 {
		return "", false
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Web search results for “%s”:\n", query)
	for i, r := range rows {
		if i == lookupResults {
			break
		}
		fmt.Fprintf(&b, "- %s (%s): %s\n", strings.TrimSpace(r.Title), r.URL, strings.TrimSpace(r.Snippet))
	}
	for _, page := range readBestPages(ctx, env, profile, prompt, args, result, pages) {
		title, _ := page["title"].(string)
		link, _ := page["url"].(string)
		content, _ := page["content"].(string)
		if utf8.RuneCountInString(content) > lookupPageRunes {
			content = string([]rune(content)[:lookupPageRunes]) + "…"
		}
		fmt.Fprintf(&b, "\nPage read: %s (%s)\n%s\n", strings.TrimSpace(title), link, strings.TrimSpace(content))
	}
	return b.String(), true
}

// lookupGuidance tells the model how to use what was looked up.
const lookupGuidance = "Toskar already searched the web for this question; the results are in the reference material. Answer from them directly: state the facts and include the most relevant link. Do not mention the search, the results, or reference material. If they do not answer the question, say so."

// withoutWeb removes web tools for a turn whose lookup is already done, so a
// small model answers instead of searching again.
func withoutWeb(profile contracts.AIProfile) contracts.AIProfile {
	out := profile
	out.Tools = make([]contracts.ToolPolicy, 0, len(profile.Tools))
	for _, t := range profile.Tools {
		if t.ToolID == "internet.search" || t.ToolID == "internet.open" {
			continue
		}
		out.Tools = append(out.Tools, t)
	}
	return out
}

func joinReference(parts ...string) string {
	var keep []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			keep = append(keep, strings.TrimSpace(p))
		}
	}
	return strings.Join(keep, "\n\n")
}

// serviceGuidance tells the model how to use data fetched from a connected service.
const serviceGuidance = "Toskar already fetched current data from the user's connected service; it is in the reference material. Answer from it directly. Do not mention the reference material. To change something, use the service's tools."

// withoutFetched removes the tools serviceFirst already called, so a small
// model answers from their data instead of asking for it again.
func withoutFetched(profile contracts.AIProfile) contracts.AIProfile {
	out := profile
	out.Tools = make([]contracts.ToolPolicy, 0, len(profile.Tools))
	for _, t := range profile.Tools {
		if def, ok := tools.Lookup(t.ToolID); ok && def.Prefetch {
			continue
		}
		out.Tools = append(out.Tools, t)
	}
	return out
}

// serviceFirst fetches current data from a connected service before the
// model answers a message about it (spec §32), as lookUpFirst does for the
// web: small models otherwise search the web for "which lights are on". It
// uses only offered tools marked Prefetch that the profile allows without
// asking.
func serviceFirst(ctx context.Context, env pluginapi.ExecutionEnvironment, profile contracts.AIProfile) (string, bool) {
	var b strings.Builder
	for _, def := range tools.Enabled(profile, nil) {
		if !def.Prefetch || !strings.EqualFold(tools.PolicyForProfile(profile, def.ID), tools.PolicyAllow) {
			continue
		}
		result, err := env.ExecuteTool(ctx, def.ID, map[string]any{})
		if err != nil {
			continue
		}
		data := readable(result)
		if utf8.RuneCountInString(data) > lookupPageRunes {
			data = string([]rune(data)[:lookupPageRunes]) + "…"
		}
		fmt.Fprintf(&b, "Current data from %s:\n%s\n", def.Name, data)
	}
	if b.Len() == 0 {
		return "", false
	}
	return b.String(), true
}

// readable writes a service's result as plain lines, such as
// "- Porch light (light.porch): off", which small models read more reliably
// than JSON.
func readable(result map[string]any) string {
	keys := make([]string, 0, len(result))
	for k := range result {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		switch v := result[k].(type) {
		case []any:
			fmt.Fprintf(&b, "%s:\n", k)
			for _, item := range v {
				fmt.Fprintf(&b, "- %s\n", readableItem(item))
			}
		default:
			fmt.Fprintf(&b, "%s: %v\n", k, v)
		}
	}
	return b.String()
}

func readableItem(item any) string {
	m, ok := item.(map[string]any)
	if !ok {
		return fmt.Sprint(item)
	}
	name, _ := m["name"].(string)
	if name == "" {
		name, _ = m["title"].(string)
	}
	id, _ := m["entity_id"].(string)
	head := name
	switch {
	case name != "" && id != "":
		head = name + " (" + id + ")"
	case name == "":
		head = id
	}
	if state, ok := m["state"]; ok {
		unit, _ := m["unit"].(string)
		line := fmt.Sprintf("%s: %v", head, state)
		if unit != "" {
			line += " " + unit
		}
		return line
	}
	raw, _ := json.Marshal(m)
	return string(raw)
}
