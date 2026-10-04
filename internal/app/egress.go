package app

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/yeixio/toskar-core/internal/egress"
	"github.com/yeixio/toskar-core/internal/mimir"
	"github.com/yeixio/toskar-core/internal/tools"
)

// recordToolEgress records a tool call that sends data off this computer:
// a web search, a page fetch, or a connected service (§63).
func (a *App) recordToolEgress(ctx context.Context, toolID string, args map[string]any) {
	if a.Egress == nil {
		return
	}
	str := func(k string) string { v, _ := args[k].(string); return strings.TrimSpace(v) }
	switch toolID {
	case "internet.search":
		a.Egress.Add(ctx, egress.WebSearch, "DuckDuckGo", str("query"))
		return
	case "internet.open":
		raw := str("url")
		host := raw
		if u, err := url.Parse(raw); err == nil && u.Hostname() != "" {
			host = u.Hostname()
		}
		a.Egress.Add(ctx, egress.WebPage, host, raw)
		return
	}
	def, ok := tools.Lookup(toolID)
	if ok && strings.HasPrefix(def.Source, "mcp:") && a.MCP != nil {
		// A tool source on the web is sent the call; one on this computer
		// is not, by Yggdrasil.
		if name, host, remote := a.MCP.Destination(strings.TrimPrefix(def.Source, "mcp:")); remote {
			a.Egress.Add(ctx, egress.Connector, name+" ("+host+")", connectorDetail(def.Name, args))
		}
		return
	}
	if !ok || !strings.HasPrefix(def.Source, "connector:") {
		return
	}
	service := strings.TrimPrefix(def.Source, "connector:")
	name := service
	if a.Connectors != nil {
		name = a.Connectors.ServiceName(service)
	}
	a.Egress.Add(ctx, egress.Connector, name, connectorDetail(def.Name, args))
}

// connectorDetail says what a connected service was asked, without long
// text such as a comment's body.
func connectorDetail(action string, args map[string]any) string {
	var parts []string
	for _, k := range []string{"query", "repo", "number", "entity_id", "filter", "domain", "service", "from", "to", "id", "uid", "title"} {
		if v, ok := args[k]; ok {
			if s := strings.TrimSpace(anyString(v)); s != "" {
				parts = append(parts, k+": "+s)
			}
		}
	}
	if len(parts) == 0 {
		return action
	}
	return action + " (" + strings.Join(parts, ", ") + ")"
}

func anyString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	}
	return ""
}

// markLocalOnly keeps the rest of the turn on this computer.
func (e *chatExecEnv) markLocalOnly() {
	e.mu.Lock()
	e.localOnly = true
	e.mu.Unlock()
}

// markLocalSources keeps the turn here when a passage comes from a
// knowledge source marked this computer only.
func (e *chatExecEnv) markLocalSources(ctx context.Context, hits []mimir.Hit) {
	if e.app == nil || e.app.Mimir == nil {
		return
	}
	seen := map[string]bool{}
	for _, h := range hits {
		if seen[h.SourceID] {
			continue
		}
		seen[h.SourceID] = true
		if src, err := e.app.Mimir.Get(ctx, h.SourceID); err == nil && src.LocalOnly {
			e.markLocalOnly()
			return
		}
	}
}

// keepLocalIfNeeded runs a turn here instead of on a paired computer when
// it uses data marked this computer only, and says so once (§63).
func (e *chatExecEnv) keepLocalIfNeeded(role, nodeID string) string {
	if e.app == nil || e.app.Config == nil {
		return nodeID
	}
	local := e.app.Config.Get().NodeID
	e.mu.Lock()
	keep := e.localOnly && nodeID != "" && nodeID != local
	first := keep && !e.keptLocally
	if keep {
		e.keptLocally = true
		if e.roleNodes != nil {
			e.roleNodes[role] = local
		}
	}
	e.mu.Unlock()
	if !keep {
		return nodeID
	}
	if first && e.trace != nil {
		e.trace.sharing(e.trace.noticeText("chat:steps.localOnly", nil))
	}
	return local
}
