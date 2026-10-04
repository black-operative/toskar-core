package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// App is another app on this computer whose MCP servers can be imported.
type App struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

// Found is a server another app has, ready to add.
type Found struct {
	App     string `json:"app"`
	AppName string `json:"app_name"`
	Spec    Spec   `json:"-"`
	// View is the spec with secret values hidden, safe to show.
	View  Spec   `json:"spec"`
	Needs []Need `json:"needs,omitempty"`
}

// knownApps lists where apps keep their MCP settings on this system.
func knownApps() []App {
	home, _ := os.UserHomeDir()
	appData := os.Getenv("APPDATA")
	var support string
	switch runtime.GOOS {
	case "darwin":
		support = filepath.Join(home, "Library", "Application Support")
	case "windows":
		support = appData
	default:
		support = filepath.Join(home, ".config")
	}
	return []App{
		{ID: "claude-desktop", Name: "Claude Desktop", Path: filepath.Join(support, "Claude", "claude_desktop_config.json")},
		{ID: "claude-code", Name: "Claude Code", Path: filepath.Join(home, ".claude.json")},
		{ID: "cursor", Name: "Cursor", Path: filepath.Join(home, ".cursor", "mcp.json")},
		{ID: "vscode", Name: "VS Code", Path: filepath.Join(support, "Code", "User", "mcp.json")},
		{ID: "vscode-settings", Name: "VS Code", Path: filepath.Join(support, "Code", "User", "settings.json")},
		{ID: "windsurf", Name: "Windsurf", Path: filepath.Join(home, ".codeium", "windsurf", "mcp_config.json")},
		{ID: "gemini", Name: "Gemini CLI", Path: filepath.Join(home, ".gemini", "settings.json")},
		{ID: "lmstudio", Name: "LM Studio", Path: filepath.Join(home, ".lmstudio", "mcp.json")},
	}
}

// Discover lists the servers other apps on this computer have set up.
func Discover() []Found {
	return discoverIn(knownApps())
}

func discoverIn(apps []App) []Found {
	var out []Found
	for _, app := range apps {
		raw, err := os.ReadFile(app.Path)
		if err != nil || len(raw) > 8<<20 {
			continue
		}
		var root map[string]json.RawMessage
		if json.Unmarshal([]byte(cleanJSON(string(raw))), &root) != nil {
			continue
		}
		specs, err := specsFrom(root)
		if err != nil {
			continue
		}
		for _, s := range specs {
			if base := strings.TrimSuffix(filepath.Base(s.Command), ".exe"); base == "toskarctl" || base == "yggctl" {
				// Yggdrasil itself, shared with that app.
				continue
			}
			view, _ := split(s)
			out = append(out, Found{App: app.ID, AppName: app.Name, Spec: s, View: view, Needs: s.Needs()})
		}
	}
	return out
}
