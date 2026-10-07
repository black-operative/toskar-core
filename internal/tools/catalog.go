package tools

import (
	"sort"
	"strings"
	"sync"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Capability IDs are the user-facing groups. Individual tools stay the model API.
const (
	CapInternet = "internet"
	CapFiles    = "files"
	CapShell    = "shell"
	CapGit      = "git"
	// CapCode runs code in a sandbox (Gungnir §20).
	CapCode = "code"
	// CapSpeech transcribes audio and reads text aloud (Gungnir §18–19).
	CapSpeech = "speech"
	// CapImage makes and edits images (Gungnir §17).
	CapImage = "image"
	// CapVideo makes short clips (Gungnir §27).
	CapVideo = "video"
	// CapBrowser uses web pages in an isolated browser (Gungnir §26).
	CapBrowser = "browser"
	// CapAutomations sets up tasks that run on a schedule (#204).
	CapAutomations = "automations"
)

// Definition is one registered tool: built in, from a connected service,
// or from an MCP tool source.
type Definition struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Capability    string `json:"capability"`
	Source        string `json:"source"`
	Schema        string `json:"schema"`
	DefaultPolicy string `json:"default_policy"`
	Risk          string `json:"risk"` // read | create | write
	// Prefetch marks a connected service's read tool that Yggdrasil calls,
	// with no arguments, before the model answers a message about that
	// service, as it looks up the web first for current questions.
	Prefetch bool `json:"prefetch,omitempty"`
	// Cues are words that show a message is about this tool's service,
	// such as a connected home's device names.
	Cues []string `json:"cues,omitempty"`
	// Always offers the tool with every request, not only ones about its
	// service: a tool source the person set to always be available.
	Always bool `json:"always,omitempty"`

	// The rest describe the tool for the registry (Gungnir §7). Each is
	// optional; Describe works out what a definition leaves unset.
	Version int `json:"-"`
	// InputSchema is a full JSON Schema of the arguments, when the short
	// Schema above is not enough.
	InputSchema string   `json:"-"`
	Outputs     []string `json:"-"`
	// Level overrides the permission level worked out from risk and source.
	Level int `json:"-"`
	// Execution is local, remote, or either.
	Execution string `json:"-"`
	// Runtime is a runtime the tool needs, such as python.
	Runtime     string `json:"-"`
	GPU         bool   `json:"-"`
	Credentials bool   `json:"-"`
	// Progress marks a tool that reports progress while it runs.
	Progress bool `json:"-"`
}

// Risk levels. A create tool only adds a file to Yggdrasil's own store, so it
// changes nothing else on the computer.
const (
	RiskRead   = "read"
	RiskCreate = "create"
	RiskWrite  = "write"
)

// Contained reports whether a tool cannot change anything outside
// Yggdrasil: it reads, or it only creates a file in Yggdrasil's store.
func Contained(risk string) bool { return risk == RiskRead || risk == RiskCreate }

// BuiltinCatalog is the single list of tools shipped with Yggdrasil.
func BuiltinCatalog() []Definition {
	return []Definition{
		{ID: "internet.search", Name: "Web Search", Description: "Search the public internet and return titles, links, and snippets.", Capability: CapInternet, Source: "builtin", Schema: `{"query":"string"}`, DefaultPolicy: PolicyAllow, Risk: "read"},
		{ID: "places.search", Name: "Find Places", Description: "Find places with OpenStreetMap: a kind of place near somewhere (\"query\": \"coffee\", \"near\": \"Juneau, AK\" or \"58.30,-134.42\"), or a place or address by name. Returns names, addresses, distances in km and miles, opening hours, and ids for places.details. The user's location is not known: use the place they name, or where their notes say they live.", Capability: CapInternet, Source: "builtin", Schema: `{"query":"string","near":"string","radius_m":"integer","limit":"integer"}`, DefaultPolicy: PolicyAllow, Risk: RiskRead},
		{ID: "places.details", Name: "Place Details", Description: "Look up one place from places.search by its id, such as node/3430732134: address, opening hours, phone, and website.", Capability: CapInternet, Source: "builtin", Schema: `{"id":"string"}`, DefaultPolicy: PolicyAllow, Risk: RiskRead},
		{ID: "maps.route", Name: "Directions", Description: "Turn-by-turn directions between two places (names, addresses, or lat,lon) with OpenStreetMap: distance in km and miles, minutes, and steps. \"mode\" is driving (default), walking, or cycling.", Capability: CapInternet, Source: "builtin", Schema: `{"from":"string","to":"string","mode":"string"}`, DefaultPolicy: PolicyAllow, Risk: RiskRead},
		{ID: "maps.distance", Name: "Distance", Description: "How far apart two places are, in a straight line and by road or path, and how long it takes. \"mode\" is driving (default), walking, or cycling.", Capability: CapInternet, Source: "builtin", Schema: `{"from":"string","to":"string","mode":"string"}`, DefaultPolicy: PolicyAllow, Risk: RiskRead},
		{ID: "browser.open", Name: "Open Page in Browser", Description: "Open a web page in an isolated browser for this chat (a fresh profile, never the user's own), when a page needs JavaScript or interaction; for simply reading a page, internet.open is faster. Returns the page's text and its links, buttons, and fields, each with a ref for browser.click and browser.type.", Capability: CapBrowser, Source: "builtin", Schema: `{"url":"string"}`, DefaultPolicy: PolicyAllow, Risk: RiskRead, Level: LevelReadExternal},
		{ID: "browser.click", Name: "Click in Browser", Description: "Click a link or button on the open page by its ref, and return the page after. The user approves clicks.", Capability: CapBrowser, Source: "builtin", Schema: `{"ref":"integer"}`, DefaultPolicy: PolicyAsk, Risk: RiskWrite},
		{ID: "browser.type", Name: "Type in Browser", Description: "Type text into a field on the open page by its ref; \"submit\" presses Enter after. Never passwords, payment details, or codes: those fields are refused, and the user enters them. The user approves typing.", Capability: CapBrowser, Source: "builtin", Schema: `{"ref":"integer","text":"string","submit":"boolean"}`, DefaultPolicy: PolicyAsk, Risk: RiskWrite},
		{ID: "browser.extract", Name: "Read Page", Description: "Read more of the open page: \"what\" is text (default, the whole page), links, or tables.", Capability: CapBrowser, Source: "builtin", Schema: `{"what":"string"}`, DefaultPolicy: PolicyAllow, Risk: RiskRead, Level: LevelReadExternal},
		{ID: "browser.download", Name: "Download from Browser", Description: "Download a file the open page links to (\"ref\" of the link, or its \"url\"), keeping the page's sign-in, into this chat, up to 25 MB. The user approves downloads.", Capability: CapBrowser, Source: "builtin", Schema: `{"ref":"integer","url":"string"}`, DefaultPolicy: PolicyAsk, Risk: RiskWrite, Outputs: []string{OutputFile}},
		{ID: "browser.screenshot", Name: "Screenshot Page", Description: "Attach a picture of what the open page shows, for the user to see.", Capability: CapBrowser, Source: "builtin", Schema: `{}`, DefaultPolicy: PolicyAllow, Risk: RiskRead, Level: LevelReadExternal, Outputs: []string{OutputImage}},
		{ID: "browser.close", Name: "Close Browser", Description: "Close this chat's browser and forget its pages and sign-ins.", Capability: CapBrowser, Source: "builtin", Schema: `{}`, DefaultPolicy: PolicyAllow, Risk: RiskRead},
		{ID: "internet.open", Name: "Open Web Page", Description: "Open a web page and return readable text.", Capability: CapInternet, Source: "builtin", Schema: `{"url":"string"}`, DefaultPolicy: PolicyAllow, Risk: "read"},
		{ID: "filesystem.search", Name: "Find Files", Description: "Search file names in the workspace.", Capability: CapFiles, Source: "builtin", Schema: `{"query":"string"}`, DefaultPolicy: PolicyAllow, Risk: "read"},
		{ID: "filesystem.read", Name: "Read File", Description: "Read a file in the workspace.", Capability: CapFiles, Source: "builtin", Schema: `{"path":"string"}`, DefaultPolicy: PolicyAllow, Risk: "read"},
		{ID: "filesystem.write", Name: "Write File", Description: "Create or replace a file in the workspace.", Capability: CapFiles, Source: "builtin", Schema: `{"path":"string","content":"string"}`, DefaultPolicy: PolicyAllow, Risk: "write"},
		{ID: "files.create", Name: "Create File", Description: "Create a file the user can download: a Word document (.docx) or PDF (.pdf) written in Markdown, a document (.md, .txt, .html), data (.json, .csv), a spreadsheet (.xlsx, given as CSV text; a line \"## Sheet: Name\" starts another sheet, and a cell starting with = is a formula), or code. Use it when the user asks for a file, a spreadsheet, a PDF, or a document.", Capability: CapFiles, Source: "builtin", Schema: `{"name":"string","content":"string"}`, DefaultPolicy: PolicyAllow, Risk: RiskCreate},
		{ID: "spreadsheet.analyze", Name: "Analyze Spreadsheet", Description: "Summarize a spreadsheet (.xlsx, .csv) attached to or made in this chat: each sheet's rows, and each column's type, count, minimum, maximum, average, and total, or its most common values, with the first rows. Use it to answer questions about a spreadsheet's whole contents.", Capability: CapFiles, Source: "builtin", Schema: `{"file":"string","sheet":"string"}`, DefaultPolicy: PolicyAllow, Risk: RiskRead},
		{ID: "speech.transcribe", Name: "Transcribe Audio", Description: "Transcribe an audio file (.wav, .mp3, .m4a, .ogg, .flac, .webm) attached to this chat, on this computer: what it says, its language, and timed segments. \"quality\" is fast (default) or accurate; \"language\" is a code such as de, or empty to detect it.", Capability: CapSpeech, Source: "builtin", Schema: `{"file":"string","quality":"string","language":"string"}`, DefaultPolicy: PolicyAllow, Risk: RiskRead,
			Runtime: "python", Outputs: []string{OutputText}},
		{ID: "speech.synthesize", Name: "Read Aloud", Description: "Read text aloud, on this computer, as an audio file attached to the answer that can be played. Use it when the user asks to hear something, or for narration. Without \"voice\", the voice is the one for the text's language, or for \"language\" (a code such as de) when given. \"voice\" is a Piper voice such as en_US-lessac-medium or de_DE-thorsten-medium.", Capability: CapSpeech, Source: "builtin", Schema: `{"text":"string","voice":"string","language":"string","name":"string"}`, DefaultPolicy: PolicyAllow, Risk: RiskCreate,
			Runtime: "python", Outputs: []string{OutputAudio}},
		{ID: "image.generate", Name: "Generate Image", Description: "Make an image from a description, on this computer, attached to the answer as a PNG. Describe the subject, setting, style, and lighting in \"prompt\". \"width\" and \"height\" are pixels (default 1024 each, up to 1536); \"seed\" repeats an earlier image.", Capability: CapImage, Source: "builtin", Schema: `{"prompt":"string","width":"integer","height":"integer","seed":"integer","name":"string"}`, DefaultPolicy: PolicyAllow, Risk: RiskCreate,
			Runtime: "sdcpp", Outputs: []string{OutputImage}},
		{ID: "video.generate", Name: "Generate Video", Description: "Make a short clip, on this computer, attached to the answer as a video that plays: from a description in \"prompt\" (subject, motion, camera, style), or from an image in this chat (\"image\" is its name) brought to life. \"seconds\" is 1 to 5 (default 2); it takes several minutes, so say so. \"width\" and \"height\" default to 832×480 landscape, or the image's shape.", Capability: CapVideo, Source: "builtin", Schema: `{"prompt":"string","image":"string","seconds":"number","width":"integer","height":"integer","seed":"integer","name":"string"}`, DefaultPolicy: PolicyAllow, Risk: RiskCreate,
			Runtime: "sdcpp", Outputs: []string{OutputVideo}},
		{ID: "image.edit", Name: "Edit Image", Description: "Change a PNG or JPEG image in this chat from an instruction, on this computer, such as \"make it night\" or \"remove the car\". \"file\" is the image's name; the changed image is attached to the answer as a new file.", Capability: CapImage, Source: "builtin", Schema: `{"file":"string","prompt":"string","seed":"integer","name":"string"}`, DefaultPolicy: PolicyAllow, Risk: RiskCreate,
			Runtime: "sdcpp", Outputs: []string{OutputImage}},
		{ID: "code.execute", Name: "Run Code", Description: "Run Python in a sandbox for calculations, data analysis, and charts, with numpy, pandas, and matplotlib. It has no network and sees only files you list from this chat (\"files\": [\"sales.xlsx\"]), read from its working folder. Print results; files it saves there (.png, .csv, .xlsx, .pdf, and so on) are attached to the answer. Use matplotlib's savefig for charts.", Capability: CapCode, Source: "builtin", Schema: `{"code":"string","files":"array"}`, DefaultPolicy: PolicyAsk, Risk: RiskWrite,
			Level: LevelHighImpact, Runtime: "python", Outputs: []string{OutputText, OutputFile}},
		{ID: "automations.schedule", Name: "Schedule Task", Description: "Set up a task that runs on a schedule, such as \"every morning at 8, summarize the news\" or \"check this price every day and tell me if it drops below $500\". \"request\" is what to do and when, in the user's words and language. It shows the user a card to confirm; nothing is scheduled until they press Create, so tell them to check it.", Capability: CapAutomations, Source: "builtin", Schema: `{"request":"string"}`, DefaultPolicy: PolicyAllow, Risk: RiskRead},
		{ID: "terminal", Name: "Terminal", Description: "Run a shell command on this computer.", Capability: CapShell, Source: "builtin", Schema: `{"command":"string"}`, DefaultPolicy: PolicyAllow, Risk: "write"},
		{ID: "git.status", Name: "Git Status", Description: "Show changed files in the workspace.", Capability: CapGit, Source: "builtin", Schema: `{}`, DefaultPolicy: PolicyAllow, Risk: "read"},
		{ID: "git.diff", Name: "Git Diff", Description: "Show the current git diff.", Capability: CapGit, Source: "builtin", Schema: `{"path":"string"}`, DefaultPolicy: PolicyAllow, Risk: "read"},
		{ID: "git.log", Name: "Git Log", Description: "Show recent commits.", Capability: CapGit, Source: "builtin", Schema: `{"path":"string"}`, DefaultPolicy: PolicyAllow, Risk: "read"},
		{ID: "git.show", Name: "Git Show", Description: "Show one commit.", Capability: CapGit, Source: "builtin", Schema: `{"revision":"string"}`, DefaultPolicy: PolicyAllow, Risk: "read"},
		{ID: "git.add", Name: "Git Add", Description: "Stage files for commit.", Capability: CapGit, Source: "builtin", Schema: `{"path":"string"}`, DefaultPolicy: PolicyAllow, Risk: "write"},
		{ID: "git.commit", Name: "Git Commit", Description: "Create a git commit.", Capability: CapGit, Source: "builtin", Schema: `{"message":"string"}`, DefaultPolicy: PolicyAllow, Risk: "write"},
		{ID: "git.push", Name: "Git Push", Description: "Push commits to the remote.", Capability: CapGit, Source: "builtin", Schema: `{"remote":"string","branch":"string"}`, DefaultPolicy: PolicyAllow, Risk: "write"},
	}
}

func Lookup(id string) (Definition, bool) {
	id = Canonical(id)
	for _, def := range Catalog() {
		if def.ID == id {
			return def, true
		}
	}
	return Definition{}, false
}

// PolicyForProfile returns the stored policy, or deny when the profile does not list the tool.
func PolicyForProfile(profile contracts.AIProfile, toolID string) string {
	toolID = Canonical(toolID)
	for _, tool := range profile.Tools {
		if tool.ToolID == toolID {
			if strings.TrimSpace(tool.Policy) == "" {
				return PolicyDeny
			}
			return tool.Policy
		}
	}
	return PolicyDeny
}

// Connected tools come from services the user connected (spec §32) and
// from MCP tool sources. They change while the daemon runs, so they live
// beside the built-in catalog.
var (
	connectedMu sync.RWMutex
	connected   = map[string][]Definition{}
)

// SetConnected replaces the tools a connected service provides; nil
// removes them.
func SetConnected(source string, defs []Definition) {
	connectedMu.Lock()
	defer connectedMu.Unlock()
	if len(defs) == 0 {
		delete(connected, source)
		return
	}
	connected[source] = append([]Definition(nil), defs...)
}

// ConnectedDefinitions lists the tools of every connected service, sorted by id.
func ConnectedDefinitions() []Definition {
	connectedMu.RLock()
	defer connectedMu.RUnlock()
	var out []Definition
	for _, defs := range connected {
		out = append(out, defs...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Catalog is every tool: built in, then connected services.
func Catalog() []Definition {
	return append(BuiltinCatalog(), ConnectedDefinitions()...)
}

// WithConnected gives a profile the tools of connected services it does not
// list yet, at each tool's default policy: reading allowed, changes asked
// first. A policy the profile already sets for one, such as deny, stays.
func WithConnected(profile contracts.AIProfile) contracts.AIProfile {
	defs := ConnectedDefinitions()
	if len(defs) == 0 {
		return profile
	}
	add := make([]contracts.ToolPolicy, 0, len(defs))
	for _, def := range defs {
		add = append(add, contracts.ToolPolicy{ToolID: def.ID, Policy: def.DefaultPolicy})
	}
	profile.Tools = MergeMissingTools(profile.Tools, add)
	return profile
}

// Enabled reports tools this profile will actually expose to a model.
func Enabled(profile contracts.AIProfile, globallyDisabled map[string]struct{}) []Definition {
	var out []Definition
	for _, def := range Catalog() {
		if _, off := globallyDisabled[def.ID]; off {
			continue
		}
		if strings.EqualFold(PolicyForProfile(profile, def.ID), PolicyDeny) {
			continue
		}
		out = append(out, def)
	}
	return out
}

// PromptFor tells the model only about tools this profile actually has.
func PromptFor(profile contracts.AIProfile) string {
	enabled := Enabled(profile, nil)
	if len(enabled) == 0 {
		return ""
	}
	var lines []string
	internet, changes := false, false
	for _, def := range enabled {
		if def.Capability == CapInternet {
			internet = true
		}
		if def.Risk == RiskWrite {
			changes = true
		}
		lines = append(lines, "- "+def.ID+" "+def.Schema+" — "+def.Description)
	}
	prompt := "You have access to tools. Use them when they improve the accuracy or freshness of your answer.\n" +
		"When a tool is needed, call it directly. Do not explain that you are about to use a tool unless doing so is important to the user. Never expose function-call syntax, tool names, internal JSON, or execution protocol in your response.\n" +
		"To call a tool, reply with ONLY this JSON and nothing else:\n" +
		`{"tool_call":{"id":"<tool_id>","args":{...}}}` + "\n" +
		"Available tools:\n" + strings.Join(lines, "\n") + "\n" +
		"After tool results arrive, answer the user in plain text."
	if internet {
		prompt += "\nYou can search and open web pages. For current information, recent events, live data, websites, or facts that may have changed since training, use internet.search and internet.open before answering. Do not tell the user to search the web themselves."
	}
	if changes {
		// A small model told it may delete files often says it can't, then
		// hands the person the command to run (rm -rf) instead.
		prompt += "\nYou can change things on this computer with the tools above. Tools that change things ask the person to approve first, so when asked to make a change, call the tool and let them decide. Do not say you are unable to, and never give the person a command that deletes or overwrites files to run themselves."
	}
	return prompt
}

// MergeMissingTools appends tools that are absent. Existing policies stay as they are.
func MergeMissingTools(existing, additions []contracts.ToolPolicy) []contracts.ToolPolicy {
	seen := map[string]struct{}{}
	out := append([]contracts.ToolPolicy(nil), existing...)
	for _, tool := range existing {
		seen[tool.ToolID] = struct{}{}
	}
	for _, tool := range additions {
		if tool.ToolID == "" {
			continue
		}
		if _, ok := seen[tool.ToolID]; ok {
			continue
		}
		out = append(out, tool)
		seen[tool.ToolID] = struct{}{}
	}
	return out
}
