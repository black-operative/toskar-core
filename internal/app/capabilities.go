package app

import (
	"context"
	"strings"
	"time"

	"github.com/yeixio/yggdrasil-core/internal/inventory"
	"github.com/yeixio/yggdrasil-core/internal/tools"
	"github.com/yeixio/yggdrasil-core/pkg/contracts"
	"github.com/yeixio/yggdrasil-core/pkg/pluginapi"
)

// Capabilities is the capability inventory right now (§37): what is
// installed, online, connected, and healthy, and what that adds up to.
func (a *App) Capabilities(ctx context.Context) inventory.Snapshot {
	if snap, ok := a.capCache.Get("snapshot"); ok {
		return snap
	}
	snap := a.buildCapabilities(ctx)
	a.capCache.Put("snapshot", snap)
	return snap
}

// buildCapabilities gathers the inventory from every part of the app.
func (a *App) buildCapabilities(ctx context.Context) inventory.Snapshot {
	s := inventory.Snapshot{At: time.Now().UTC(), Models: []inventory.Model{}, Nodes: []inventory.Node{},
		Tools: []inventory.Tool{}, Connectors: []inventory.Connector{}, Providers: []inventory.Provider{}}

	running := map[string]bool{}
	if views, err := a.localRunningViews(ctx); err == nil {
		for _, v := range views {
			running[v.ModelID] = true
		}
	}
	local := ""
	if a.Config != nil {
		local = a.Config.Get().NodeName
	}
	for _, m := range a.installedModels(ctx) {
		if !m.Installed && len(m.InstalledOn) == 0 {
			continue
		}
		im := inventory.Model{ID: m.ID, Name: firstNonEmpty(m.DisplayName, m.ID), Running: running[m.ID],
			ToolCalling: toolCallSupport(m.Capabilities) != "unsupported",
			Vision:      m.Capabilities.Vision, Coding: m.Capabilities.Coding, SupportRole: contracts.SupportRoleOf(m),
			MemoryNeeded: m.MemoryNeeded, On: []string{}}
		if m.Installed && local != "" {
			im.On = append(im.On, local)
		}
		for _, on := range m.InstalledOn {
			if on.NodeName != "" && on.NodeName != local {
				im.On = append(im.On, on.NodeName)
			}
		}
		s.Models = append(s.Models, im)
	}

	if nodes, err := a.listNodesWithHardware(ctx); err == nil {
		for _, n := range nodes {
			in := inventory.Node{ID: n.ID, Name: firstNonEmpty(n.Name, n.ID), Local: n.IsLocal,
				Online: n.IsLocal || n.Status == contracts.NodeStatusOnline}
			if n.Hardware != nil {
				in.MemoryBytes = n.Hardware.Memory.TotalBytes
				if a.Training != nil {
					in.Trainer = a.Training.TrainerName(*n.Hardware)
				}
			}
			s.Nodes = append(s.Nodes, in)
		}
	}

	disabled := map[string]struct{}{}
	if a.Tools != nil {
		disabled = a.Tools.Disabled()
	}
	for _, def := range tools.Catalog() {
		_, off := disabled[def.ID]
		tool := inventory.Tool{ID: def.ID, Name: def.Name, Description: def.Description,
			Source: def.Source, Risk: def.Risk, Enabled: !off}
		if a.Tools != nil {
			if t, err := a.Tools.Get(def.ID); err == nil {
				if av, ok := t.(interface{ Available() (bool, string) }); ok {
					if ready, why := av.Available(); !ready {
						tool.Unavailable = why
					}
				}
			}
		}
		s.Tools = append(s.Tools, tool)
	}

	if a.Connectors != nil {
		if list, err := a.Connectors.List(ctx); err == nil {
			for _, c := range list {
				s.Connectors = append(s.Connectors, inventory.Connector{ID: c.ID, Name: c.Name,
					Connected: c.Connected && c.Status != "error", Status: c.Status})
			}
		}
	}

	if a.Runtimes != nil {
		if list, err := a.Runtimes.List(ctx); err == nil {
			for _, r := range list {
				s.Providers = append(s.Providers, inventory.Provider{ID: r.ID, Name: r.DisplayName, Kind: "runtime",
					Status: r.Status, Healthy: r.Detection.Installed})
			}
		}
	}
	if a.MCP != nil {
		for _, v := range a.MCP.List(ctx) {
			s.Providers = append(s.Providers, inventory.Provider{ID: "mcp:" + v.ID, Name: v.Name, Kind: "mcp",
				Status: v.Status, Healthy: v.Enabled && v.Status == "ready"})
		}
	}

	if a.DB != nil {
		_ = a.DB.SQL.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(size_bytes), 0) FROM artifacts`).
			Scan(&s.Artifacts.Count, &s.Artifacts.Bytes)
	}

	s.Setups = a.setupOptions(s)
	s.Abilities = inventory.Abilities(s)
	return s
}

// capabilityFacts answers a question about what Yggdrasil can do from the
// inventory, so the model states facts instead of guessing (§37).
func (a *App) capabilityFacts(ctx context.Context, message string) string {
	if !inventory.IsQuestion(message) && !strings.Contains(strings.ToLower(message), "which computer") {
		return ""
	}
	return inventory.Facts(a.Capabilities(ctx), message)
}

// answerCapabilityQuestion replies to a short question about what
// Yggdrasil can do straight from the inventory (§37, §64).
func (a *App) answerCapabilityQuestion(ctx context.Context, conversationID, message string) (<-chan pluginapi.ChatChunk, bool) {
	if !inventory.IsQuestion(message) {
		return nil, false
	}
	reply, ok := inventory.Direct(a.Capabilities(ctx), message)
	if !ok {
		return nil, false
	}
	meta := &contracts.MessageMeta{Steps: []contracts.ActivityStep{{Kind: "share", Text: "Checked what Toskar can do right now"}}}
	// "Can you generate images?" on a computer that could: offer the setup.
	if asked := inventory.Ask(a.Capabilities(ctx), message); len(asked) == 1 && asked[0].Setup != nil {
		meta.Setup = setupOffer(asked[0], "")
	}
	return a.replyDirectly(ctx, conversationID, message, reply, meta), true
}
