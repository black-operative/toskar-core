package app

import (
	"context"
	"strings"
	"time"

	"github.com/yeixio/toskar-core/internal/api"
	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/internal/egress"
	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/internal/runtimes/external"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// externalKeySecret names the external server's API key in the secrets
// folder.
const externalKeySecret = "external-openai.key"

// externalListFor is how long the external server's model list is reused.
const externalListFor = time.Minute

func mustNormalizeExternal(raw string) string {
	u, err := external.NormalizeURL(raw)
	if err != nil {
		return ""
	}
	return u
}

// externalModels are the external server's models as Yggdrasil lists them
// (#111): ext: IDs, marked external. They are offered to choose, and Auto,
// fallback, and the default model never pick them.
func (a *App) externalModels(ctx context.Context) ([]contracts.Model, error) {
	if a.External == nil || a.External.Config().BaseURL == "" {
		return nil, nil
	}
	a.extMu.Lock()
	defer a.extMu.Unlock()
	if !a.extAt.IsZero() && time.Since(a.extAt) < externalListFor {
		return a.extList, a.extErr
	}
	lctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	ids, err := a.External.ListModels(lctx)
	var list []contracts.Model
	for _, id := range ids {
		list = append(list, contracts.Model{
			ID: external.ModelPrefix + id, DisplayName: id, Installed: true, Status: "external",
			Runtime: []string{"external-openai"}, Tags: []string{"general", "external"}, Purpose: []string{"general"},
			Capabilities: contracts.ModelCapabilities{ToolCalling: true, ToolCallSupport: "compatible"},
			Summary:      "On " + a.External.Host() + ". Chats with it leave this computer.",
		})
	}
	a.extList, a.extErr, a.extAt = list, err, time.Now()
	return list, err
}

// ExternalServer is the external server's settings and models.
func (a *App) ExternalServer(ctx context.Context) (api.ExternalServer, error) {
	cfg := a.External.Config()
	out := api.ExternalServer{BaseURL: cfg.BaseURL, HasKey: cfg.APIKey != "", Models: []string{}}
	if cfg.BaseURL == "" {
		return out, nil
	}
	models, err := a.externalModels(ctx)
	if err != nil {
		out.Error = err.Error()
	}
	for _, m := range models {
		out.Models = append(out.Models, m.DisplayName)
	}
	return out, nil
}

// SetExternalServer stores the base URL in the configuration and the API
// key in the secrets folder, then reads the model list.
func (a *App) SetExternalServer(ctx context.Context, in api.ExternalServerInput) (api.ExternalServer, error) {
	base, err := external.NormalizeURL(in.BaseURL)
	if err != nil {
		return api.ExternalServer{}, contracts.NewError("EXTERNAL_BAD_URL", nil, err)
	}
	if err := a.Config.Update(func(c *config.Config) { c.ExternalOpenAIURL = base }); err != nil {
		return api.ExternalServer{}, err
	}
	cfg := a.External.Config()
	cfg.BaseURL = base
	switch {
	case in.ClearKey || base == "":
		cfg.APIKey = ""
		_ = a.secrets.Delete(externalKeySecret)
	case strings.TrimSpace(in.APIKey) != "":
		cfg.APIKey = strings.TrimSpace(in.APIKey)
		if err := a.secrets.Write(externalKeySecret, cfg.APIKey); err != nil {
			return api.ExternalServer{}, err
		}
	}
	a.External.SetConfig(cfg)
	a.extMu.Lock()
	a.extAt = time.Time{}
	a.extMu.Unlock()
	return a.ExternalServer(ctx)
}

// profileOffline reports a profile that keeps work on this computer: one
// that turns web search off, as the offline presets do.
func profileOffline(p profiles.Profile) bool {
	for _, t := range p.Tools {
		if t.ToolID == "internet.search" {
			return t.Policy == "deny"
		}
	}
	return true
}

// externalAllowed says why a turn may not go to the external server, or
// nil: an offline profile, or memories or knowledge marked This computer
// only.
func externalAllowed(p profiles.Profile, keepsLocal bool) error {
	if profileOffline(p) {
		return contracts.Errorf("EXTERNAL_OFFLINE_PROFILE", nil, "this profile keeps work on this computer, so it doesn't use the external server; choose a model on this computer or another profile")
	}
	if keepsLocal {
		return contracts.Errorf("EXTERNAL_LOCAL_ONLY", nil, "this chat uses memories or knowledge marked This computer only, so it can't go to the external server; choose a model on this computer")
	}
	return nil
}

// generateExternal streams a turn from a model on the external server and
// records what left this computer.
func (a *App) generateExternal(ctx context.Context, modelID string, messages []pluginapi.ChatMessage) (<-chan pluginapi.ChatChunk, error) {
	model := external.ServerModel(modelID)
	a.Egress.Add(ctx, egress.ExternalServer, a.External.Host(), "prompt and conversation for "+model)
	ch, err := a.External.Chat(ctx, model, pluginapi.ChatRequest{Messages: messages, Stream: true})
	if err != nil {
		return nil, contracts.NewError("EXTERNAL_FAILED", nil, err)
	}
	return countWork(ctx, ch, a.beginWork(a.Config.Get().NodeID)), nil
}
