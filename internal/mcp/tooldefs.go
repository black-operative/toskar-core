package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/yeixio/toskar-core/internal/tools"
)

// maxResultText caps the text a tool result gives the model, so one large
// page or file does not crowd out the conversation.
const maxResultText = 24_000

// resultMap turns a tool result into what the model reads: the text, any
// structured data, and links it can cite.
func resultMap(res callToolResult) (map[string]any, error) {
	var texts []string
	var links []any
	omitted := 0
	for _, ct := range res.Content {
		switch ct.Type {
		case "text":
			texts = append(texts, ct.Text)
		case "resource":
			if ct.Resource != nil {
				if ct.Resource.Text != "" {
					texts = append(texts, ct.Resource.Text)
				} else {
					omitted++
				}
				if isWeb(ct.Resource.URI) {
					links = append(links, map[string]any{"url": ct.Resource.URI})
				}
			}
		case "resource_link":
			link := map[string]any{"url": ct.URI, "title": firstNonEmpty(ct.Title, ct.Name)}
			if ct.Description != "" {
				link["description"] = ct.Description
			}
			links = append(links, link)
		default:
			// Images and audio cannot be read by a text model.
			omitted++
		}
	}
	text := strings.TrimSpace(strings.Join(texts, "\n\n"))
	if res.IsError {
		if text == "" {
			text = "the tool reported an error"
		}
		return nil, errors.New(truncate(text, 600))
	}
	out := map[string]any{}
	if text != "" {
		out["text"] = truncate(text, maxResultText)
	}
	if len(res.StructuredContent) > 0 && string(res.StructuredContent) != "null" {
		var structured any
		if json.Unmarshal(res.StructuredContent, &structured) == nil {
			// Servers usually send the same data as text too; keep the
			// structured copy only when there is no text or it is small.
			if text == "" || len(res.StructuredContent) < 4000 {
				out["data"] = structured
			}
		}
	}
	if len(links) > 0 {
		// "results" with url and title is what sources are cited from.
		out["results"] = links
	}
	if omitted > 0 {
		out["note"] = fmt.Sprintf("%d image or file item(s) were left out because they cannot be read as text.", omitted)
	}
	if len(out) == 0 {
		out["text"] = "Done. The tool returned nothing to read."
	}
	return out, nil
}

func isWeb(u string) bool { return strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://") }

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := s[:n]
	// Do not end inside a UTF-8 sequence.
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut + "\n…(cut short)"
}

var readNameRe = regexp.MustCompile(`(?i)^(get|list|search|read|fetch|find|query|describe|show|lookup|look_up|view|check|count|resolve|browse|explain|inspect|retrieve|whoami|summari[sz]e|analy[sz]e|ask|current|convert)([_\-A-Z.]|$)`)

// riskOf decides whether a tool only reads. The server's own hints count
// first; without them, a name like get_issue or search_pages reads.
func riskOf(t RemoteTool) string {
	if a := t.Annotations; a != nil {
		if a.ReadOnlyHint != nil {
			if *a.ReadOnlyHint {
				return tools.RiskRead
			}
			return tools.RiskWrite
		}
		if a.DestructiveHint != nil && *a.DestructiveHint {
			return tools.RiskWrite
		}
	}
	if readNameRe.MatchString(t.Name) {
		return tools.RiskRead
	}
	return tools.RiskWrite
}

// defaultPolicy is reading without asking and asking before changes, as
// for connected services.
func defaultPolicy(risk string) string {
	if risk == tools.RiskRead {
		return tools.PolicyAllow
	}
	return tools.PolicyAsk
}

var toolNameRe = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

// toolID is the catalog id of a server's tool: "linear.list_issues".
func toolID(serverID, name string) string {
	return serverID + "." + strings.Trim(toolNameRe.ReplaceAllString(name, "_"), "_")
}

// displayName is a person-friendly name: list_issues is "List issues".
func displayName(t RemoteTool) string {
	if t.Annotations != nil && t.Annotations.Title != "" {
		return t.Annotations.Title
	}
	if t.Title != "" {
		return t.Title
	}
	n := strings.NewReplacer("_", " ", "-", " ", ".", " ").Replace(t.Name)
	n = camelRe.ReplaceAllString(n, "$1 $2")
	n = strings.ToLower(strings.TrimSpace(n))
	if n == "" {
		return t.Name
	}
	return strings.ToUpper(n[:1]) + n[1:]
}

var camelRe = regexp.MustCompile(`([a-z0-9])([A-Z])`)

// compactSchema writes a tool's input schema the way built-in tools
// describe theirs, {"query":"string","limit?":"integer"}, which small
// models follow more reliably than full JSON Schema.
func compactSchema(raw json.RawMessage) string {
	var s struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &s) != nil || len(s.Properties) == 0 {
		return "{}"
	}
	required := map[string]bool{}
	for _, r := range s.Required {
		required[r] = true
	}
	names := make([]string, 0, len(s.Properties))
	for n := range s.Properties {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		if required[names[i]] != required[names[j]] {
			return required[names[i]]
		}
		return names[i] < names[j]
	})
	var parts []string
	for _, n := range names {
		key := n
		if !required[n] {
			key += "?"
		}
		kb, _ := json.Marshal(key)
		vb, _ := json.Marshal(typeOf(s.Properties[n]))
		parts = append(parts, string(kb)+":"+string(vb))
	}
	out := "{" + strings.Join(parts, ",") + "}"
	if len(out) > 1200 {
		out = out[:1200] + "…}"
	}
	return out
}

func typeOf(raw json.RawMessage) string {
	var p struct {
		Type  any               `json:"type"`
		Enum  []any             `json:"enum"`
		Items json.RawMessage   `json:"items"`
		AnyOf []json.RawMessage `json:"anyOf"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return "any"
	}
	if len(p.Enum) > 0 && len(p.Enum) <= 8 {
		vals := make([]string, 0, len(p.Enum))
		for _, e := range p.Enum {
			vals = append(vals, fmt.Sprint(e))
		}
		return strings.Join(vals, "|")
	}
	t := ""
	switch x := p.Type.(type) {
	case string:
		t = x
	case []any:
		for _, v := range x {
			if s, ok := v.(string); ok && s != "null" {
				t = s
				break
			}
		}
	}
	if t == "" && len(p.AnyOf) > 0 {
		for _, a := range p.AnyOf {
			if at := typeOf(a); at != "null" && at != "any" {
				return at
			}
		}
	}
	if t == "array" {
		if len(p.Items) > 0 {
			return typeOf(p.Items) + "[]"
		}
		return "array"
	}
	if t == "" {
		return "any"
	}
	return t
}
