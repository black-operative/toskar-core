package api

import (
	"context"
	"errors"
	"html/template"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/yeixio/yggdrasil-core/internal/config"
	"github.com/yeixio/yggdrasil-core/internal/mcp"
)

// BindMCP attaches the MCP tool source routes, Yggdrasil's own MCP server
// at /mcp, and the sign-in callback. ctl says where the toskarctl program
// is, for the settings other apps use.
func (s *Server) BindMCP(m *mcp.Manager, server *mcp.Server, ctl func() string) {
	s.mcp, s.mcpServer, s.ctl = m, server, ctl
}

func (s *Server) mcpRoutes(api *mux.Router) {
	api.HandleFunc("/mcp/servers", s.mcpHandler(s.handleListMCP)).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/mcp/servers", s.mcpHandler(s.handleAddMCP)).Methods(http.MethodPost)
	api.HandleFunc("/mcp/servers/{id}", s.mcpHandler(s.handleGetMCP)).Methods(http.MethodGet)
	api.HandleFunc("/mcp/servers/{id}", s.mcpHandler(s.handleUpdateMCP)).Methods(http.MethodPatch)
	api.HandleFunc("/mcp/servers/{id}", s.mcpHandler(s.handleReplaceMCP)).Methods(http.MethodPut)
	api.HandleFunc("/mcp/servers/{id}", s.mcpHandler(s.handleRemoveMCP)).Methods(http.MethodDelete)
	api.HandleFunc("/mcp/servers/{id}/check", s.mcpHandler(s.handleCheckMCP)).Methods(http.MethodPost)
	api.HandleFunc("/mcp/servers/{id}/sign-in", s.mcpHandler(s.handleSignInMCP)).Methods(http.MethodPost)
	api.HandleFunc("/mcp/servers/{id}/sign-out", s.mcpHandler(s.handleSignOutMCP)).Methods(http.MethodPost)
	api.HandleFunc("/mcp/servers/{id}/logs", s.mcpHandler(s.handleMCPLogs)).Methods(http.MethodGet)
	api.HandleFunc("/mcp/servers/{id}/prompts", s.mcpHandler(s.handleMCPPrompts)).Methods(http.MethodGet)
	api.HandleFunc("/mcp/servers/{id}/prompts/{name}", s.mcpHandler(s.handleMCPGetPrompt)).Methods(http.MethodPost)
	api.HandleFunc("/mcp/servers/{id}/resources", s.mcpHandler(s.handleMCPResources)).Methods(http.MethodGet)
	api.HandleFunc("/mcp/gallery", s.mcpHandler(s.handleMCPGallery)).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/mcp/import", s.mcpHandler(s.handleMCPImport)).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/mcp/parse", s.mcpHandler(s.handleMCPParse)).Methods(http.MethodPost)
	api.HandleFunc("/mcp/share", s.mcpHandler(s.handleMCPShare)).Methods(http.MethodGet, http.MethodOptions)
}

// mcpRootRoutes are outside /api/v1: the MCP endpoint other apps call,
// which checks keys as /v1 does, and the sign-in callback the browser
// returns to from a service, which its one-time state value protects.
func (s *Server) mcpRootRoutes(r *mux.Router) {
	r.HandleFunc(mcp.CallbackPath, s.handleMCPCallback).Methods(http.MethodGet)
	r.HandleFunc("/mcp", func(w http.ResponseWriter, req *http.Request) {
		if s.mcpServer == nil {
			http.Error(w, "MCP is not available", http.StatusNotImplemented)
			return
		}
		s.mcpServer.ServeHTTP(w, req)
	}).Methods(http.MethodPost, http.MethodGet, http.MethodDelete, http.MethodOptions)
}

func (s *Server) mcpHandler(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.mcp == nil {
			writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Tool sources are not available.", nil)
			return
		}
		h(w, r)
	}
}

func writeMCPErr(w http.ResponseWriter, err error) {
	if errors.Is(err, mcp.ErrUnknown) {
		writeErrFrom(w, http.StatusNotFound, "MCP_NOT_FOUND", err)
		return
	}
	writeErrFrom(w, http.StatusBadRequest, "MCP_FAILED", err)
}

func (s *Server) handleListMCP(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.mcp.List(r.Context()))
}

func (s *Server) handleGetMCP(w http.ResponseWriter, r *http.Request) {
	v, ok := s.mcp.Get(mux.Vars(r)["id"])
	if !ok {
		writeErr(w, http.StatusNotFound, "MCP_NOT_FOUND", "No such tool source.", nil)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// handleAddMCP adds a source after checking it connects. Starting one for
// the first time can download it, so this can take a minute.
func (s *Server) handleAddMCP(w http.ResponseWriter, r *http.Request) {
	var req mcp.AddRequest
	if !decodeBody(w, r, &req) {
		return
	}
	added, err := s.mcp.Add(r.Context(), req)
	if err != nil {
		writeMCPErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, added)
}

func (s *Server) handleUpdateMCP(w http.ResponseWriter, r *http.Request) {
	var u mcp.Update
	if !decodeBody(w, r, &u) {
		return
	}
	v, err := s.mcp.Update(r.Context(), mux.Vars(r)["id"], u)
	if err != nil {
		writeMCPErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleReplaceMCP(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Spec   mcp.Spec          `json:"spec"`
		Values map[string]string `json:"values"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	v, err := s.mcp.Replace(r.Context(), mux.Vars(r)["id"], body.Spec, body.Values)
	if err != nil {
		writeMCPErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleRemoveMCP(w http.ResponseWriter, r *http.Request) {
	if err := s.mcp.Remove(r.Context(), mux.Vars(r)["id"]); err != nil {
		writeMCPErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCheckMCP(w http.ResponseWriter, r *http.Request) {
	v, err := s.mcp.Check(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeMCPErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleSignInMCP(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RedirectBase string `json:"redirect_base"`
	}
	if r.ContentLength > 0 && !decodeBody(w, r, &body) {
		return
	}
	link, err := s.mcp.SignIn(r.Context(), mux.Vars(r)["id"], body.RedirectBase)
	if err != nil {
		writeMCPErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": link})
}

func (s *Server) handleSignOutMCP(w http.ResponseWriter, r *http.Request) {
	v, err := s.mcp.SignOut(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeMCPErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleMCPLogs(w http.ResponseWriter, r *http.Request) {
	lines, err := s.mcp.Logs(mux.Vars(r)["id"])
	if err != nil {
		writeMCPErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, lines)
}

func (s *Server) handleMCPPrompts(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	list, err := s.mcp.Prompts(ctx, mux.Vars(r)["id"])
	if err != nil {
		writeMCPErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleMCPGetPrompt(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Arguments map[string]string `json:"arguments"`
	}
	if r.ContentLength > 0 && !decodeBody(w, r, &body) {
		return
	}
	vars := mux.Vars(r)
	text, err := s.mcp.GetPrompt(r.Context(), vars["id"], vars["name"], body.Arguments)
	if err != nil {
		writeMCPErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"text": text})
}

func (s *Server) handleMCPResources(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	list, err := s.mcp.Resources(ctx, mux.Vars(r)["id"])
	if err != nil {
		writeMCPErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleMCPGallery(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.mcp.GalleryView())
}

// handleMCPImport lists servers set up in other apps on this computer,
// with secret values hidden. Adding one reads it from the app again, so
// its secrets never pass through the browser.
func (s *Server) handleMCPImport(w http.ResponseWriter, r *http.Request) {
	list := s.mcp.ImportCandidates()
	if list == nil {
		list = []mcp.ImportCandidate{}
	}
	writeJSON(w, http.StatusOK, list)
}

// handleMCPParse reads pasted text into servers, and what each still needs.
func (s *Server) handleMCPParse(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text string `json:"text"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	specs, err := mcp.Parse(body.Text)
	if err != nil {
		writeMCPErr(w, err)
		return
	}
	type parsed struct {
		Spec    mcp.Spec   `json:"spec"`
		Needs   []mcp.Need `json:"needs"`
		Missing string     `json:"missing,omitempty"`
	}
	out := make([]parsed, 0, len(specs))
	for _, sp := range specs {
		p := parsed{Spec: sp, Needs: sp.Needs()}
		if p.Needs == nil {
			p.Needs = []mcp.Need{}
		}
		if !sp.Remote() {
			p.Missing = mcp.MissingRuntime(sp.Command)
		}
		out = append(out, p)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleMCPShare says how other apps reach Yggdrasil's MCP server.
func (s *Server) handleMCPShare(w http.ResponseWriter, r *http.Request) {
	ctl := "toskarctl"
	if s.ctl != nil {
		ctl = s.ctl()
	}
	host, port := "127.0.0.1", 7331
	needsKey := false
	if s.deps.Config != nil {
		cfg := s.deps.Config.Get()
		port = cfg.APIPort
		needsKey = config.ListensBeyondLoopback(cfg.APIHost)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"url":       "http://" + host + ":" + strconv.Itoa(port) + "/mcp",
		"command":   ctl,
		"args":      []string{"mcp"},
		"needs_key": needsKey,
	})
}

var callbackPage = template.Must(template.New("callback").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Toskar sign-in</title>
<style>
:root{color-scheme:light dark;--bg:#f7f6f2;--ink:#1d1d1b;--muted:#6b6b66;--ok:#2f7d4f;--bad:#b3362b}
@media (prefers-color-scheme:dark){:root{--bg:#141413;--ink:#ecebe6;--muted:#9a9993;--ok:#6cc08d;--bad:#ef7b6f}}
body{margin:0;min-height:100vh;display:grid;place-items:center;background:var(--bg);color:var(--ink);font:16px/1.5 system-ui,sans-serif;padding:16px}
main{max-width:28rem;text-align:center}h1{font-size:1.25rem;margin:0 0 .5rem}p{color:var(--muted);margin:0}
.ok{color:var(--ok)}.bad{color:var(--bad)}
</style></head><body><main>
{{if .OK}}<h1 class="ok">Signed in to {{.Name}}</h1><p>Its tools are ready in Toskar. You can close this window.</p>
{{else}}<h1 class="bad">Sign-in did not finish</h1><p>{{.Error}}</p>{{end}}
</main>
<script>
try { if (window.opener) {
  // Both names: a page from before the Toskar rename listens for the old one.
  window.opener.postMessage({ type: 'toskar-mcp-sign-in', ok: {{.OK}} }, '*')
  window.opener.postMessage({ type: 'yggdrasil-mcp-sign-in', ok: {{.OK}} }, '*')
} } catch (e) {}
{{if .OK}}setTimeout(function () { window.close() }, 1500){{end}}
</script></body></html>`))

// handleMCPCallback finishes a sign-in when the service sends the browser
// back, and shows a page that says how it went.
func (s *Server) handleMCPCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	data := struct {
		OK          bool
		Name, Error string
	}{}
	if s.mcp == nil {
		data.Error = "Tool sources are not available."
	} else {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
		defer cancel()
		name, err := s.mcp.FinishSignIn(ctx, q.Get("state"), q.Get("code"), q.Get("error"), q.Get("error_description"))
		data.Name = name
		if err != nil {
			data.Error = err.Error()
		} else {
			data.OK = true
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	_ = callbackPage.Execute(w, data)
}
