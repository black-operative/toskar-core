package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/tools"
)

func TestParseWhatReadmesSay(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []Spec
	}{
		{
			name: "claude desktop config",
			text: `{
  "mcpServers": {
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/Users/me/Desktop"]
    },
    "notion": { "url": "https://mcp.notion.com/mcp" }
  }
}`,
			want: []Spec{
				{Name: "Filesystem", Preset: "folders", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-filesystem", "/Users/me/Desktop"}},
				{Name: "Notion", Preset: "notion", URL: "https://mcp.notion.com/mcp"},
			},
		},
		{
			name: "vs code with comments and trailing commas",
			text: `{
  // My servers
  "servers": {
    "github": {
      "type": "http",
      "url": "https://api.githubcopilot.com/mcp/",
      "headers": { "Authorization": "Bearer ${input:github_token}" }, /* asked for */
    },
  },
}`,
			want: []Spec{{Name: "Github", Preset: "github-mcp", URL: "https://api.githubcopilot.com/mcp/", Headers: map[string]string{"Authorization": "Bearer ${input:github_token}"}}},
		},
		{
			name: "fragment copied out of a bigger file",
			text: `"brave-search": {"command": "npx", "args": ["-y", "@brave/brave-search-mcp-server"], "env": {"BRAVE_API_KEY": "YOUR_API_KEY_HERE"}}`,
			want: []Spec{{Name: "Brave Search", Preset: "brave-search", Command: "npx", Args: []string{"-y", "@brave/brave-search-mcp-server"}, Env: map[string]string{"BRAVE_API_KEY": "YOUR_API_KEY_HERE"}}},
		},
		{
			name: "one server object",
			text: `{"command": "uvx", "args": ["mcp-server-time"]}`,
			want: []Spec{{Name: "Time", Preset: "time", Command: "uvx", Args: []string{"mcp-server-time"}}},
		},
		{
			name: "windsurf serverUrl",
			text: `{"mcpServers": {"deepwiki": {"serverUrl": "https://mcp.deepwiki.com/sse"}}}`,
			want: []Spec{{Name: "Deepwiki", URL: "https://mcp.deepwiki.com/sse"}},
		},
		{
			name: "web address",
			text: "https://mcp.linear.app/mcp",
			want: []Spec{{Name: "Linear", Preset: "linear", URL: "https://mcp.linear.app/mcp"}},
		},
		{
			name: "command line with variables",
			text: `DATABASE_URI="postgresql://me:pw@localhost/db" uvx postgres-mcp --access-mode=restricted`,
			want: []Spec{{Name: "Postgres", Preset: "postgres", Command: "uvx", Args: []string{"postgres-mcp", "--access-mode=restricted"}, Env: map[string]string{"DATABASE_URI": "postgresql://me:pw@localhost/db"}}},
		},
		{
			name: "claude mcp add, local",
			text: `claude mcp add playwright -e DEBUG=1 -- npx @playwright/mcp@latest`,
			want: []Spec{{Name: "Playwright", Preset: "browser", Command: "npx", Args: []string{"@playwright/mcp@latest"}, Env: map[string]string{"DEBUG": "1"}}},
		},
		{
			name: "claude mcp add, remote",
			text: `claude mcp add --transport http sentry https://mcp.sentry.dev/mcp`,
			want: []Spec{{Name: "Sentry", Preset: "sentry", URL: "https://mcp.sentry.dev/mcp"}},
		},
		{
			name: "mcp-remote becomes a direct connection",
			text: `{"mcpServers": {"linear": {"command": "npx", "args": ["-y", "mcp-remote", "https://mcp.linear.app/mcp"]},
				"internal": {"command": "npx", "args": ["mcp-remote@latest", "https://tools.example.com/mcp", "--header", "Authorization: Bearer ${AUTH_TOKEN}", "--transport", "http-only"],
					"env": {"AUTH_TOKEN": "abc123token"}}}}`,
			want: []Spec{
				{Name: "Internal", URL: "https://tools.example.com/mcp", Headers: map[string]string{"Authorization": "Bearer abc123token"}},
				{Name: "Linear", Preset: "linear", URL: "https://mcp.linear.app/mcp"},
			},
		},
		{
			name: "zed context servers",
			text: `{"context_servers": {"my-tool": {"command": {"path": "/usr/local/bin/my-tool", "args": ["serve"]}}}}`,
			want: []Spec{{Name: "My Tool", Command: "/usr/local/bin/my-tool", Args: []string{"serve"}}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.text)
			if err != nil {
				t.Fatal(err)
			}
			for i := range got {
				got[i] = normalize(got[i])
			}
			for i := range tc.want {
				tc.want[i] = normalize(tc.want[i])
			}
			if !reflect.DeepEqual(got, tc.want) {
				a, _ := json.Marshal(got)
				b, _ := json.Marshal(tc.want)
				t.Fatalf("got  %s\nwant %s", a, b)
			}
		})
	}
}

func normalize(s Spec) Spec {
	if len(s.Env) == 0 {
		s.Env = nil
	}
	if len(s.Headers) == 0 {
		s.Headers = nil
	}
	if len(s.Args) == 0 {
		s.Args = nil
	}
	return s
}

func TestParseRejectsNonsense(t *testing.T) {
	for _, text := range []string{"", "   ", `{"hello": 1}`, "{broken"} {
		if _, err := Parse(text); err == nil {
			t.Errorf("Parse(%q) accepted", text)
		}
	}
}

func TestNeedsAndFill(t *testing.T) {
	specs, err := Parse(`{"mcpServers": {"x": {"command": "npx", "args": ["-y", "pkg", "<path-to-db>"],
		"env": {"API_KEY": "<your-api-key>", "LOG_LEVEL": "info", "TOKEN": ""},
		"headers": {}}}}`)
	if err != nil {
		t.Fatal(err)
	}
	needs := specs[0].Needs()
	keys := []string{}
	for _, n := range needs {
		keys = append(keys, n.Key)
	}
	if strings.Join(keys, ",") != "env:API_KEY,env:TOKEN,arg:2" {
		t.Fatalf("needs = %v", keys)
	}
	if !needs[0].Secret || !needs[1].Secret {
		t.Fatalf("key and token should be secret: %+v", needs)
	}
	filled := specs[0].Fill(map[string]string{"env:API_KEY": "sk-1234567890abcd", "env:TOKEN": "t0ken-value", "arg:2": "/data/app.db"})
	if len(filled.Needs()) != 0 || filled.Args[2] != "/data/app.db" || specs[0].Env["API_KEY"] != "<your-api-key>" {
		t.Fatalf("fill = %+v (original changed: %v)", filled, specs[0].Env)
	}
}

func TestSplitKeepsSecretsApart(t *testing.T) {
	s := Spec{Name: "x", Command: "run",
		Args:    []string{"--db", "postgresql://u:hunter2pass@h/db", "--plain"},
		Env:     map[string]string{"GITHUB_TOKEN": "ghp_abcdefghijklmnop", "REGION": "eu", "OTHER": "sk-abcdefghijklmnopq"},
		Headers: map[string]string{"Authorization": "Bearer abcdefghij"}}
	stored, sec := split(s)
	if stored.Env["GITHUB_TOKEN"] != "" || stored.Env["OTHER"] != "" || stored.Env["REGION"] != "eu" {
		t.Fatalf("stored env = %v", stored.Env)
	}
	if stored.Args[1] != "" || stored.Args[2] != "--plain" || stored.Headers["Authorization"] != "" {
		t.Fatalf("stored = %+v", stored)
	}
	if !reflect.DeepEqual(join(stored, sec), s) {
		t.Fatal("join did not restore the spec")
	}
	vals := strings.Join(sec.values(), " ")
	for _, want := range []string{"ghp_abcdefghijklmnop", "hunter2pass", "abcdefghij"} {
		if !strings.Contains(vals, want) {
			t.Errorf("values missing %q", want)
		}
	}
}

func TestDiscoverReadsOtherApps(t *testing.T) {
	dir := t.TempDir()
	claude := filepath.Join(dir, "claude.json")
	_ = os.WriteFile(claude, []byte(`{"mcpServers": {
		"time": {"command": "uvx", "args": ["mcp-server-time"]},
		"yggdrasil": {"command": "/opt/homebrew/bin/yggctl", "args": ["mcp"]},
		"notion": {"command": "npx", "args": ["-y", "@notionhq/notion-mcp-server"], "env": {"NOTION_TOKEN": "ntn_1234567890abcdef"}}
	}}`), 0o600)
	vscode := filepath.Join(dir, "settings.json")
	_ = os.WriteFile(vscode, []byte(`{
		"editor.fontSize": 13,
		// MCP
		"mcp": {"servers": {"context7": {"type": "http", "url": "https://mcp.context7.com/mcp"}}},
	}`), 0o600)
	found := discoverIn([]App{
		{ID: "claude-desktop", Name: "Claude Desktop", Path: claude},
		{ID: "vscode-settings", Name: "VS Code", Path: vscode},
		{ID: "missing", Name: "Missing", Path: filepath.Join(dir, "nope.json")},
	})
	if len(found) != 3 {
		t.Fatalf("found = %+v", found)
	}
	names := []string{}
	for _, f := range found {
		names = append(names, f.AppName+"/"+f.Spec.Name)
		if f.Spec.Name == "Notion" {
			if f.View.Env["NOTION_TOKEN"] != "" || f.Spec.Env["NOTION_TOKEN"] == "" {
				t.Fatalf("view shows the token or spec lost it: %+v", f)
			}
		}
	}
	if strings.Join(names, ",") != "Claude Desktop/Notion,Claude Desktop/Time,VS Code/Context7" {
		t.Fatalf("names = %v", names)
	}
}

func TestGalleryEntriesBuildSpecs(t *testing.T) {
	p, _ := presetByID("folders")
	s, err := p.Spec(map[string]string{"folders": "~/Documents\n~/Projects, /tmp"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"-y", "@modelcontextprotocol/server-filesystem", "~/Documents", "~/Projects", "/tmp"}; !reflect.DeepEqual(s.Args, want) {
		t.Fatalf("args = %v", s.Args)
	}
	p, _ = presetByID("github-mcp")
	if _, err := p.Spec(nil); err == nil {
		t.Fatal("a required token was not asked for")
	}
	s, _ = p.Spec(map[string]string{"token": "github_pat_abc"})
	if s.Headers["Authorization"] != "Bearer github_pat_abc" {
		t.Fatalf("headers = %v", s.Headers)
	}
	p, _ = presetByID("context7")
	if s, err = p.Spec(nil); err != nil || len(s.Headers) != 0 {
		t.Fatalf("optional key: %+v, %v", s, err)
	}
	ids := map[string]bool{}
	for _, g := range Gallery() {
		if ids[g.ID] || reservedIDs[g.ID] {
			t.Errorf("gallery id %q is taken", g.ID)
		}
		ids[g.ID] = true
		if (g.command == "") == (g.url == "") {
			t.Errorf("%s needs a command or an address", g.ID)
		}
		if g.Remote != (g.url != "") {
			t.Errorf("%s Remote does not match", g.ID)
		}
	}
}

func TestToolShapes(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		tool RemoteTool
		risk string
	}{
		{RemoteTool{Name: "list_issues"}, tools.RiskRead},
		{RemoteTool{Name: "getPage"}, tools.RiskRead},
		{RemoteTool{Name: "browser_navigate", Annotations: &ToolAnnotations{ReadOnlyHint: &no}}, tools.RiskWrite},
		{RemoteTool{Name: "update_issue"}, tools.RiskWrite},
		{RemoteTool{Name: "get_and_delete", Annotations: &ToolAnnotations{DestructiveHint: &yes}}, tools.RiskWrite},
		{RemoteTool{Name: "whatever", Annotations: &ToolAnnotations{ReadOnlyHint: &yes}}, tools.RiskRead},
	}
	for _, c := range cases {
		if got := riskOf(c.tool); got != c.risk {
			t.Errorf("riskOf(%s) = %s, want %s", c.tool.Name, got, c.risk)
		}
	}
	if got := displayName(RemoteTool{Name: "searchIssues_byLabel"}); got != "Search issues by label" {
		t.Errorf("displayName = %q", got)
	}
	if got := toolID("linear", "issues/list.v2"); got != "linear.issues_list_v2" {
		t.Errorf("toolID = %q", got)
	}
	schema := json.RawMessage(`{"type":"object","properties":{
		"tags":{"type":"array","items":{"type":"string"}},
		"limit":{"type":["integer","null"]},
		"query":{"type":"string"},
		"filter":{"anyOf":[{"type":"null"},{"type":"object"}]}
	},"required":["query"]}`)
	if got := compactSchema(schema); got != `{"query":"string","filter?":"object","limit?":"integer","tags?":"string[]"}` {
		t.Errorf("compactSchema = %s", got)
	}
	if got := truncate(strings.Repeat("é", 10), 5); !strings.HasPrefix(got, "éé\n") {
		t.Errorf("truncate split a character: %q", got)
	}
}

func TestServersGetOnlySafeEnvironment(t *testing.T) {
	env := mergeEnv([]string{"HOME=/h", "PATH=/bin", "OPENAI_API_KEY=sk-leak", "XDG_CONFIG_HOME=/c", "LANG=en"}, "/bin:/opt", map[string]string{"TOKEN": "t", "PATH": "/x"})
	got := strings.Join(env, " ")
	for _, want := range []string{"HOME=/h", "XDG_CONFIG_HOME=/c", "LANG=en", "TOKEN=t", "PATH=/x:/bin:/opt"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
	if strings.Contains(got, "OPENAI_API_KEY") || strings.Count(got, "PATH=") != 1 {
		t.Errorf("env = %s", got)
	}
}
