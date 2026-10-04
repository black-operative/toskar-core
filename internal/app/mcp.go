package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/yeixio/yggdrasil-core/internal/auth"
	"github.com/yeixio/yggdrasil-core/internal/huginn"
	"github.com/yeixio/yggdrasil-core/internal/mcp"
	"github.com/yeixio/yggdrasil-core/internal/mimir"
	"github.com/yeixio/yggdrasil-core/internal/turnopts"
	"github.com/yeixio/yggdrasil-core/internal/version"
	"github.com/yeixio/yggdrasil-core/pkg/pluginapi"
)

// mcpBackend is what Yggdrasil's MCP server offers other apps: the local AI,
// the model list, and connected knowledge.
func (a *App) mcpBackend() mcp.Backend {
	return mcp.Backend{
		Ask:       a.mcpAsk,
		Models:    a.mcpModels,
		Search:    a.mcpSearch,
		Authorize: a.mcpAuthorize,
		Version:   version.Version,
	}
}

type mcpPermsKey struct{}

// mcpAuthorize checks an MCP request as /v1 checks one, and carries what
// its key allows into the calls.
func (a *App) mcpAuthorize(r *http.Request) (context.Context, error) {
	perms, err := a.openAIPermissions(r)
	if err != nil {
		return nil, err
	}
	return context.WithValue(r.Context(), mcpPermsKey{}, perms), nil
}

func mcpPerms(ctx context.Context) auth.APIKeyPermissions {
	if p, ok := ctx.Value(mcpPermsKey{}).(auth.APIKeyPermissions); ok {
		return p
	}
	return auth.DefaultAPIKeyPermissions()
}

// mcpAsk answers another app's question as an API request would, within
// its key's permissions. Memories are used only when the key always allows
// them, since an MCP call cannot ask for them, and tools only read.
func (a *App) mcpAsk(ctx context.Context, prompt, model string) (string, error) {
	if model == "" {
		model = huginn.AutoModelID
	}
	perms := mcpPerms(ctx)
	opts := &turnopts.Options{
		Memory:        perms.Memory == auth.UseAlways,
		Knowledge:     perms.Knowledge != auth.UseNever,
		ReadOnlyTools: true,
	}
	if perms.Tools == auth.ToolsNone {
		opts.Tools = []string{}
	}
	ctx = turnopts.With(ctx, opts)
	stream, err := a.RunChat(ctx, "", "", prompt, false, model, "")
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for chunk := range stream {
		if chunk.Error != "" {
			return "", errors.New(chunk.Error)
		}
		b.WriteString(chunk.Content)
	}
	answer := strings.TrimSpace(b.String())
	if answer == "" {
		return "", errors.New("the local AI did not answer")
	}
	return answer, nil
}

func (a *App) mcpModels(ctx context.Context) ([]mcp.ModelInfo, error) {
	out := []mcp.ModelInfo{{ID: huginn.AutoModelID, Name: "Auto", Description: "Toskar picks an installed model for each question."}}
	for _, m := range a.installedModels(ctx) {
		if !m.Installed || huginn.Supporting(m) {
			continue
		}
		out = append(out, mcp.ModelInfo{ID: m.ID, Name: m.DisplayName})
	}
	if a.Training != nil {
		if deployed, err := a.Training.DeployedModels(ctx); err == nil {
			for _, m := range deployed {
				out = append(out, mcp.ModelInfo{ID: m.ID, Name: m.DisplayName, Description: "Specialized AI"})
			}
		}
	}
	return out, nil
}

func (a *App) mcpSearch(ctx context.Context, query string, limit int) ([]mcp.Passage, error) {
	if a.Mimir == nil {
		return nil, errors.New("connected knowledge is not available")
	}
	if mcpPerms(ctx).Knowledge == auth.UseNever {
		return nil, errors.New("this API key may not use connected knowledge")
	}
	hits, err := a.Mimir.Search(ctx, mimir.SearchInput{Query: query, Limit: limit})
	if err != nil {
		return nil, err
	}
	out := make([]mcp.Passage, 0, len(hits))
	for _, h := range hits {
		out = append(out, mcp.Passage{Source: h.SourceName, Title: h.Title, Text: h.Body})
	}
	return out, nil
}

// mcpSample lets a tool source the person allowed ask the AI for a reply,
// with the model Auto picks for the request.
func (a *App) mcpSample(ctx context.Context, system string, msgs []mcp.SampleMessage, maxTokens int) (string, string, error) {
	last := msgs[len(msgs)-1].Text
	choice, err := a.chooseAuto(ctx, last, false, a.replyLanguage(ctx, "", last, "").Tag)
	if err != nil {
		return "", "", err
	}
	chat := make([]pluginapi.ChatMessage, 0, len(msgs)+1)
	if system != "" {
		chat = append(chat, pluginapi.ChatMessage{Role: "system", Content: system})
	}
	for _, m := range msgs {
		role := m.Role
		if role != "assistant" {
			role = "user"
		}
		chat = append(chat, pluginapi.ChatMessage{Role: role, Content: m.Text})
	}
	text, err := a.generateOnce(ctx, choice.Model.ID, "", a.localAdapters(ctx, choice.Model.ID), chat)
	return text, choice.Model.ID, err
}

// ctlPath is where toskarctl is, for the settings other apps use to run
// Toskar's MCP bridge. It is installed beside the daemon; an app bundle
// built before the rename has yggctl instead (#237).
func ctlPath() string {
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	for _, name := range []string{"toskarctl" + ext, "yggctl" + ext} {
		if exe, err := os.Executable(); err == nil {
			if p := filepath.Join(filepath.Dir(exe), name); fileExists(p) {
				return p
			}
		}
		for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin"} {
			if p := filepath.Join(dir, name); fileExists(p) {
				return p
			}
		}
	}
	return "toskarctl" + ext
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}
