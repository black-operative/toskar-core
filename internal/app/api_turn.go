package app

import (
	"slices"

	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/internal/turnopts"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// narrowTools limits a profile's tools to what an API key and request
// allow (§62). It only removes tools; a policy is never loosened.
func narrowTools(profile profiles.Profile, opts *turnopts.Options) profiles.Profile {
	out := profile
	out.Tools = make([]contracts.ToolPolicy, 0, len(profile.Tools))
	for _, t := range profile.Tools {
		id := tools.Canonical(t.ToolID)
		if opts.Tools != nil && !slices.Contains(opts.Tools, id) {
			continue
		}
		if opts.ReadOnlyTools {
			if def, ok := tools.Lookup(id); !ok || def.Risk != tools.RiskRead {
				continue
			}
		}
		out.Tools = append(out.Tools, t)
	}
	return out
}

// progress sends a turn's progress to an API caller that asked for it.
func (e *chatExecEnv) progress(eventType string, payload map[string]any) {
	if e.opts == nil || e.opts.Progress == nil {
		return
	}
	e.opts.Progress(eventType, payload)
}
