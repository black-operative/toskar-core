package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/yeixio/toskar-core/internal/tools"
)

type toolView struct {
	tools.Descriptor
	Enabled bool `json:"enabled"`
	// Health is ok, off (turned off on the Tools page), or unavailable (no
	// provider is running it, such as an MCP source that is down).
	Health   string   `json:"health"`
	Profiles []string `json:"profiles"`
}

func (a *App) toolHealth(id string, disabled map[string]struct{}) string {
	if _, off := disabled[id]; off {
		return "off"
	}
	t, err := a.Tools.Get(id)
	if err != nil {
		return "unavailable"
	}
	// A tool that needs something this computer lacks, such as a sandbox.
	if av, ok := t.(interface{ Available() (bool, string) }); ok {
		if ready, _ := av.Available(); !ready {
			return "unavailable"
		}
	}
	return "ok"
}

// describeTool returns one tool's descriptor with its state (Gungnir §7–8).
func (a *App) describeTool(ctx context.Context, id string) (any, error) {
	def, ok := tools.Lookup(id)
	if !ok {
		return nil, fmt.Errorf("tool %q not found", id)
	}
	list, err := a.listToolViews(ctx)
	if err != nil {
		return nil, err
	}
	for _, v := range list.([]toolView) {
		if v.ID == def.ID {
			return v, nil
		}
	}
	return nil, fmt.Errorf("tool %q not found", id)
}

func (a *App) listToolViews(ctx context.Context) (any, error) {
	disabled := a.Tools.Disabled()
	var profiles []struct {
		name  string
		tools map[string]string
	}
	if a.Profiles != nil {
		list, err := a.Profiles.List(ctx)
		if err != nil {
			return nil, err
		}
		for _, profile := range list {
			policies := map[string]string{}
			for _, tool := range profile.Tools {
				policies[tool.ToolID] = tool.Policy
			}
			profiles = append(profiles, struct {
				name  string
				tools map[string]string
			}{name: profile.Name, tools: policies})
		}
	}
	out := make([]toolView, 0)
	for _, def := range tools.Catalog() {
		_, off := disabled[def.ID]
		view := toolView{Descriptor: tools.Describe(def), Enabled: !off, Health: a.toolHealth(def.ID, disabled)}
		for _, profile := range profiles {
			policy := profile.tools[def.ID]
			if policy != "" && !strings.EqualFold(policy, tools.PolicyDeny) {
				view.Profiles = append(view.Profiles, profile.name)
			}
		}
		if view.Profiles == nil {
			view.Profiles = []string{}
		}
		out = append(out, view)
	}
	return out, nil
}

func (a *App) setToolEnabled(ctx context.Context, id string, enabled bool) error {
	if err := a.Tools.SetEnabled(id, enabled); err != nil {
		return err
	}
	a.invalidateCapabilities()
	ids := make([]string, 0)
	for disabled := range a.Tools.Disabled() {
		ids = append(ids, disabled)
	}
	return a.Settings.Set(ctx, "disabled_builtin_tools", strings.Join(ids, ","))
}

func (a *App) testTool(ctx context.Context, id string, args map[string]any) (map[string]any, error) {
	def, ok := tools.Lookup(id)
	if !ok {
		return nil, errString("unknown tool")
	}
	if def.Risk != "read" {
		return nil, errString("this tool changes data, so test it from a chat approval instead")
	}
	return a.Tools.Execute(ctx, id, args, tools.PolicyAllow, "tool test", map[string]any{"test": true})
}

func (a *App) loadDisabledTools(ctx context.Context) {
	raw, err := a.Settings.GetString(ctx, "disabled_builtin_tools", "")
	if err != nil || raw == "" {
		return
	}
	for _, id := range strings.Split(raw, ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		_ = a.Tools.SetEnabled(id, false)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
