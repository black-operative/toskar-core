package mcp

import (
	"fmt"
	"strings"
)

// Field is one value a gallery entry asks for.
type Field struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Help        string `json:"help,omitempty"`
	Secret      bool   `json:"secret,omitempty"`
	Optional    bool   `json:"optional,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	Default     string `json:"default,omitempty"`
	// Kind says what the field is for the form: text, folder, file, or
	// folders (one per line).
	Kind string `json:"kind,omitempty"`

	// Where the value goes: an environment variable, a header (Format
	// wraps it, such as "Bearer %s"), or the end of the arguments.
	env    string
	header string
	format string
	arg    bool
}

// Preset is a gallery entry: a well-known server, set up with a few plain
// questions instead of a config file.
type Preset struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Category    string  `json:"category"`
	Homepage    string  `json:"homepage,omitempty"`
	Fields      []Field `json:"fields"`
	// Setup is guidance shown before the fields, such as which token
	// scopes to grant.
	Setup string `json:"setup,omitempty"`
	// SignIn means the service asks you to sign in in the browser.
	SignIn bool `json:"sign_in,omitempty"`
	// Remote means the server runs on the service's computers.
	Remote bool `json:"remote"`

	command string
	args    []string
	env     map[string]string
	url     string
	cues    []string
}

// Categories, in gallery order.
const (
	CatFiles = "Files & data"
	CatWeb   = "Web & browser"
	CatWork  = "Work apps"
	CatDocs  = "Developer docs"
	CatUtil  = "Utilities"
)

var gallery = []Preset{
	{
		ID: "folders", Name: "Folders", Category: CatFiles,
		Description: "Let the AI read, search, and organize files in folders you choose.",
		Homepage:    "https://github.com/modelcontextprotocol/servers/tree/main/src/filesystem",
		command:     "npx", args: []string{"-y", "@modelcontextprotocol/server-filesystem"},
		Fields: []Field{{Key: "folders", Label: "Folders to share", Kind: "folders", Default: "~/Documents",
			Help: "One per line. The AI can only see inside these folders.", arg: true}},
		cues: []string{"folder", "folders", "documents", "downloads", "desktop", "my files"},
	},
	{
		ID: "sqlite", Name: "SQLite database", Category: CatFiles,
		Description: "Ask questions about a SQLite database file and run queries on it.",
		Homepage:    "https://pypi.org/project/mcp-server-sqlite/",
		command:     "uvx", args: []string{"mcp-server-sqlite", "--db-path"},
		Fields: []Field{{Key: "path", Label: "Database file", Kind: "file", Placeholder: "~/data/app.db", arg: true}},
		cues:   []string{"sqlite", "database", "table", "tables", "sql", "query", "rows"},
	},
	{
		ID: "postgres", Name: "PostgreSQL", Category: CatFiles,
		Description: "Look at a PostgreSQL database's tables, run read-only queries, and check its health.",
		Homepage:    "https://github.com/crystaldba/postgres-mcp",
		command:     "uvx", args: []string{"postgres-mcp", "--access-mode=restricted"},
		Setup: "Toskar connects in read-only mode. Use a database user that can only read, if you have one.",
		Fields: []Field{{Key: "uri", Label: "Connection address", Secret: true,
			Placeholder: "postgresql://user:password@localhost:5432/dbname", env: "DATABASE_URI"}},
		cues: []string{"postgres", "postgresql", "database", "table", "tables", "sql", "query", "schema"},
	},
	{
		ID: "browser", Name: "Browser", Category: CatWeb,
		Description: "Let the AI open websites in a real browser, click, fill in forms, and read what it sees.",
		Homepage:    "https://github.com/microsoft/playwright-mcp",
		command:     "npx", args: []string{"-y", "@playwright/mcp@latest"},
		Setup: "Uses Google Chrome on this computer. Pages the AI opens can see what you are signed in to, so it asks before clicking or typing.",
		cues:  []string{"browser", "website", "web page", "webpage", "click", "screenshot", "fill in", "log in to", "navigate"},
	},
	{
		ID: "brave-search", Name: "Brave Search", Category: CatWeb,
		Description: "Search the web, news, images, and places with Brave's independent index.",
		Homepage:    "https://brave.com/search/api/",
		command:     "npx", args: []string{"-y", "@brave/brave-search-mcp-server"},
		env:    map[string]string{"BRAVE_MCP_TRANSPORT": "stdio"},
		Setup:  "Get a free API key at brave.com/search/api.",
		Fields: []Field{{Key: "key", Label: "Brave Search API key", Secret: true, Placeholder: "BSA…", env: "BRAVE_API_KEY"}},
		cues:   []string{"brave", "search", "news", "nearby", "near me"},
	},
	{
		ID: "notion", Name: "Notion", Category: CatWork, Remote: true, SignIn: true,
		Description: "Search, read, and update your Notion pages and databases.",
		Homepage:    "https://developers.notion.com/docs/mcp",
		url:         "https://mcp.notion.com/mcp",
		cues:        []string{"notion", "page", "pages", "wiki", "notes", "database"},
	},
	{
		ID: "linear", Name: "Linear", Category: CatWork, Remote: true, SignIn: true,
		Description: "Find, create, and update Linear issues, projects, and comments.",
		Homepage:    "https://linear.app/docs/mcp",
		url:         "https://mcp.linear.app/mcp",
		cues:        []string{"linear", "issue", "issues", "ticket", "tickets", "project", "cycle", "backlog", "bug"},
	},
	{
		ID: "atlassian", Name: "Jira & Confluence", Category: CatWork, Remote: true, SignIn: true,
		Description: "Search and update Jira issues and Confluence pages.",
		Homepage:    "https://www.atlassian.com/platform/remote-mcp-server",
		url:         "https://mcp.atlassian.com/v1/sse",
		cues:        []string{"jira", "confluence", "atlassian", "ticket", "tickets", "epic", "sprint", "issue"},
	},
	{
		ID: "sentry", Name: "Sentry", Category: CatWork, Remote: true, SignIn: true,
		Description: "Look up errors, issues, and releases in Sentry, and ask why something broke.",
		Homepage:    "https://docs.sentry.io/product/sentry-mcp/",
		url:         "https://mcp.sentry.dev/mcp",
		cues:        []string{"sentry", "error", "errors", "crash", "exception", "stack trace", "release"},
	},
	{
		ID: "github-mcp", Name: "GitHub (all tools)", Category: CatWork, Remote: true,
		Description: "GitHub's own server: repositories, code, issues, pull requests, Actions, and more.",
		Homepage:    "https://github.com/github/github-mcp-server",
		url:         "https://api.githubcopilot.com/mcp/",
		Setup: "Create a fine-grained personal access token limited to the repositories you want. " +
			"Grant only what you want the AI to do; read-only access is enough to search and read. " +
			"For issues and pull requests only, the GitHub connection in Settings is simpler.",
		Fields: []Field{{Key: "token", Label: "Personal access token", Secret: true, Placeholder: "github_pat_…",
			header: "Authorization", format: "Bearer %s"}},
		cues: []string{"github", "repo", "repository", "pull request", "workflow", "actions", "commit"},
	},
	{
		ID: "context7", Name: "Context7 library docs", Category: CatDocs, Remote: true,
		Description: "Current documentation and code examples for thousands of libraries and frameworks.",
		Homepage:    "https://context7.com",
		url:         "https://mcp.context7.com/mcp",
		Fields: []Field{{Key: "key", Label: "API key", Secret: true, Optional: true,
			Help: "Optional. A free key from context7.com gives higher limits.", header: "CONTEXT7_API_KEY"}},
		cues: []string{"docs", "documentation", "library", "framework", "sdk", "api reference", "context7", "package"},
	},
	{
		ID: "deepwiki", Name: "DeepWiki", Category: CatDocs, Remote: true,
		Description: "Ask questions about any public GitHub repository and read its generated wiki.",
		Homepage:    "https://deepwiki.com",
		url:         "https://mcp.deepwiki.com/mcp",
		cues:        []string{"deepwiki", "codebase", "repository", "repo", "how does", "architecture"},
	},
	{
		ID: "microsoft-learn", Name: "Microsoft Learn", Category: CatDocs, Remote: true,
		Description: "Search Microsoft's official documentation: Azure, .NET, Windows, Microsoft 365, and more.",
		Homepage:    "https://learn.microsoft.com/training/support/mcp",
		url:         "https://learn.microsoft.com/api/mcp",
		cues:        []string{"microsoft", "azure", ".net", "dotnet", "c#", "powershell", "windows", "office 365", "microsoft 365"},
	},
	{
		ID: "time", Name: "Time zones", Category: CatUtil,
		Description: "Current time anywhere, and conversions between time zones.",
		Homepage:    "https://pypi.org/project/mcp-server-time/",
		command:     "uvx", args: []string{"mcp-server-time"},
		cues: []string{"time zone", "timezone", "what time", "time in", "o'clock", "utc", "convert time"},
	},
	{
		ID: "everything", Name: "MCP test server", Category: CatUtil,
		Description: "A server with one of every MCP feature, for trying things out and checking a setup.",
		Homepage:    "https://github.com/modelcontextprotocol/servers/tree/main/src/everything",
		command:     "npx", args: []string{"-y", "@modelcontextprotocol/server-everything"},
		cues: []string{"test server", "mcp test", "echo"},
	},
}

// Gallery lists the gallery entries.
func Gallery() []Preset { return append([]Preset(nil), gallery...) }

func presetByID(id string) (Preset, bool) {
	for _, p := range gallery {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}

// Runner is the program a local entry needs, such as npx.
func (p Preset) Runner() string { return p.command }

// Spec builds the server spec from the person's answers.
func (p Preset) Spec(values map[string]string) (Spec, error) {
	s := Spec{Name: p.Name, Preset: p.ID, Command: p.command, Args: append([]string(nil), p.args...),
		URL: p.url, Env: map[string]string{}, Headers: map[string]string{}}
	for k, v := range p.env {
		s.Env[k] = v
	}
	for _, f := range p.Fields {
		v := strings.TrimSpace(values[f.Key])
		if v == "" {
			v = f.Default
		}
		if v == "" {
			if f.Optional {
				continue
			}
			return Spec{}, fmt.Errorf("%s is needed", f.Label)
		}
		switch {
		case f.env != "":
			s.Env[f.env] = v
		case f.header != "":
			if f.format != "" {
				v = fmt.Sprintf(f.format, strings.TrimPrefix(v, "Bearer "))
			}
			s.Headers[f.header] = v
		case f.arg && f.Kind == "folders":
			for _, line := range strings.FieldsFunc(v, func(r rune) bool { return r == '\n' || r == ',' }) {
				if line = strings.TrimSpace(line); line != "" {
					s.Args = append(s.Args, line)
				}
			}
		case f.arg:
			s.Args = append(s.Args, v)
		}
	}
	return s, nil
}

// withPreset links a pasted or imported spec to its gallery entry, so it
// gets the entry's name and selection words.
func withPreset(s Spec) Spec {
	for _, p := range gallery {
		same := p.url != "" && strings.TrimRight(s.URL, "/") == strings.TrimRight(p.url, "/")
		if !same && p.command != "" && s.Command != "" && len(p.args) > 0 {
			pkg := packageOf(p.args)
			same = pkg != "" && packageOf(s.Args) == pkg
		}
		if same {
			s.Preset = p.ID
			if strings.EqualFold(s.Name, p.Name) {
				// "Deepwiki" from the address is DeepWiki.
				s.Name = p.Name
			}
			return s
		}
	}
	return s
}

// packageOf is the package a runner line names, without its version.
func packageOf(args []string) string {
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		if i := strings.LastIndex(a, "@"); i > 0 {
			a = a[:i]
		}
		return a
	}
	return ""
}
