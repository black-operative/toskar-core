// Package inventory is Yggdrasil's capability inventory (spec §37): which
// models, computers, tools, providers, connected services, and files exist
// right now, and what they add up to. Huginn asks it questions such as
// "can I generate an image?" instead of hardcoding what is installed.
package inventory

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Model is an installed model.
type Model struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Running      bool     `json:"running"`
	ToolCalling  bool     `json:"tool_calling"`
	Vision       bool     `json:"vision"`
	Coding       bool     `json:"coding"`
	SupportRole  string   `json:"support_role,omitempty"`
	MemoryNeeded uint64   `json:"memory_needed_bytes,omitempty"`
	On           []string `json:"on"`
}

// Node is a computer Yggdrasil can use.
type Node struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Local       bool   `json:"local"`
	Online      bool   `json:"online"`
	MemoryBytes uint64 `json:"memory_bytes,omitempty"`
	// Trainer names how it can train specialized AIs, if it can.
	Trainer string `json:"trainer,omitempty"`
}

// Tool is a tool the assistant can call.
type Tool struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Source is builtin, connector:<id>, or mcp:<id>.
	Source  string `json:"source"`
	Risk    string `json:"risk,omitempty"`
	Enabled bool   `json:"enabled"`
	// Unavailable says why a tool that is on cannot run on this computer
	// yet, such as image generation before it is set up.
	Unavailable string `json:"unavailable,omitempty"`
}

// Connector is a connected-service integration.
type Connector struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Connected bool   `json:"connected"`
	Status    string `json:"status,omitempty"`
}

// Provider is something that serves models or tools: a runtime, or an MCP
// server.
type Provider struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Status  string `json:"status"`
	Healthy bool   `json:"healthy"`
}

// Artifacts summarizes stored files.
type Artifacts struct {
	Count int   `json:"count"`
	Bytes int64 `json:"bytes"`
}

// Ability is something Yggdrasil can or cannot do right now, and why.
type Ability struct {
	ID        string   `json:"id"`
	Label     string   `json:"label"`
	Available bool     `json:"available"`
	Via       []string `json:"via,omitempty"`
	// Note says how it works, or what would make it possible.
	Note string `json:"note,omitempty"`
	// Setup is what would add a missing ability, when Yggdrasil can install
	// it (Gungnir §29).
	Setup *Setup `json:"setup,omitempty"`
}

// Setup is an installable way to add an ability: a provider, how much it
// downloads, and the computer it would run on.
type Setup struct {
	Ability string `json:"ability"`
	// Option is what to install, such as an image model's id.
	Option    string `json:"option"`
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
	NodeID    string `json:"node_id,omitempty"`
	NodeName  string `json:"node_name,omitempty"`
	// Tools are the tools it makes ready; a profile that denies them is
	// not offered it.
	Tools []string `json:"tools"`
}

// Snapshot is the inventory at one moment.
type Snapshot struct {
	At         time.Time   `json:"at"`
	Models     []Model     `json:"models"`
	Nodes      []Node      `json:"nodes"`
	Tools      []Tool      `json:"tools"`
	Connectors []Connector `json:"connectors"`
	Providers  []Provider  `json:"providers"`
	Artifacts  Artifacts   `json:"artifacts"`
	Abilities  []Ability   `json:"abilities"`
	// Setups are installable ways to add missing abilities.
	Setups []Setup `json:"setups,omitempty"`
}

// rule decides one ability from a snapshot.
type rule struct {
	id, label string
	// cue matches a question about this ability.
	cue  *regexp.Regexp
	eval func(s Snapshot) (bool, []string, string)
}

func toolsWhere(s Snapshot, keep func(Tool) bool) []string {
	var out []string
	for _, t := range s.Tools {
		if t.Enabled && t.Unavailable == "" && keep(t) {
			out = append(out, t.Name)
		}
	}
	return out
}

func toolID(ids ...string) func(Tool) bool {
	return func(t Tool) bool {
		for _, id := range ids {
			if t.ID == id || strings.HasPrefix(t.ID, id) {
				return true
			}
		}
		return false
	}
}

// toolAbout matches tools from any source by what they say they do, so a
// new MCP server's tools count without code here.
func toolAbout(subject, action *regexp.Regexp) func(Tool) bool {
	return func(t Tool) bool {
		text := t.ID + " " + t.Name + " " + t.Description
		return subject.MatchString(text) && (action == nil || action.MatchString(text))
	}
}

func modelsWhere(s Snapshot, keep func(Model) bool) []string {
	var out []string
	for _, m := range s.Models {
		if keep(m) {
			out = append(out, m.Name)
		}
	}
	return out
}

func connected(s Snapshot, id string) (string, bool) {
	for _, c := range s.Connectors {
		if c.ID == id && c.Connected {
			return c.Name, true
		}
	}
	return "", false
}

var (
	imageRe    = regexp.MustCompile(`(?i)\b(image|picture|photo|drawing|illustration)s?\b`)
	makeRe     = regexp.MustCompile(`(?i)\b(generat|creat|draw|render|make)`)
	mailRe     = regexp.MustCompile(`(?i)\b(e-?mail|gmail|inbox|outlook|mail)\b`)
	calendarRe = regexp.MustCompile(`(?i)\b(calendar|event|meeting|appointment)s?\b`)
)

var rules = []rule{
	{"web", "Search the web and read pages", regexp.MustCompile(`(?i)\b(search|browse|look (it |things )?up|google)\b.*\b(web|internet|online)\b|\b(internet|web) access\b|\bbrowse the web\b`),
		func(s Snapshot) (bool, []string, string) {
			v := toolsWhere(s, toolID("internet."))
			return len(v) > 0, v, "Searches use DuckDuckGo; pages are read as text."
		}},
	{"read_files", "Read files on this computer", regexp.MustCompile(`(?i)\b(read|open|see|access|look at)\b.*\b(my )?(files?|documents?|folders?)\b`),
		func(s Snapshot) (bool, []string, string) {
			v := toolsWhere(s, toolID("filesystem.read", "filesystem.search"))
			return len(v) > 0, v, "Within the workspace folder."
		}},
	{"make_files", "Make files you can download", regexp.MustCompile(`(?i)\b(make|create|write|generate|save)\b.*\b(file|spreadsheet|document|csv|xlsx|pdf)s?\b`),
		func(s Snapshot) (bool, []string, string) {
			v := toolsWhere(s, toolID("files.create", "filesystem.write"))
			return len(v) > 0, v, ""
		}},
	{"run_code", "Run commands and code", regexp.MustCompile(`(?i)\b(run|execute)\b.*\b(python|code|scripts?|commands?|programs?|shell|terminal)\b`),
		func(s Snapshot) (bool, []string, string) {
			if v := toolsWhere(s, toolID("code.execute")); len(v) > 0 {
				return true, append(v, toolsWhere(s, toolID("terminal"))...), "Python runs in a sandbox with numpy, pandas, and matplotlib: no network, no access to your files beyond those in the chat, and a time limit. Charts and files it writes are attached. It asks first unless the profile allows it."
			}
			v := toolsWhere(s, toolID("terminal"))
			if len(v) == 0 {
				return false, nil, "The Run Code and Terminal tools are turned off."
			}
			return true, v, "Commands run in this computer's terminal, so Python works if it is installed. Running a command asks first unless the profile allows it."
		}},
	{"git", "Work with Git repositories", regexp.MustCompile(`(?i)\b(git|commit|branch|pull request)\b`),
		func(s Snapshot) (bool, []string, string) {
			v := toolsWhere(s, toolID("git."))
			return len(v) > 0, v, ""
		}},
	{"image_generation", "Generate images", regexp.MustCompile(`(?i)\b(generat|creat|draw|make|render|paint)\w*\b.*\b(images?|pictures?|photos?|drawings?|illustrations?|art)\b|\b(draw|paint|sketch|illustrate)\s+(me\s+)?(a|an|the|my|our|some)\b`),
		func(s Snapshot) (bool, []string, string) {
			v := toolsWhere(s, toolAbout(imageRe, makeRe))
			if len(v) > 0 {
				return true, v, ""
			}
			for _, t := range s.Tools {
				if t.Enabled && t.ID == "image.generate" && t.Unavailable != "" {
					return false, nil, strings.ToUpper(t.Unavailable[:1]) + t.Unavailable[1:] + "."
				}
			}
			return false, nil, "No image model or image tool is installed. Adding a tool source that generates images, on the Tools page, would add it."
		}},
	{"browser", "Use websites", regexp.MustCompile(`(?i)\b(browser|click|fill (in|out)|sign (up|in)|log ?in|websites?|web pages?)\b`),
		func(s Snapshot) (bool, []string, string) {
			v := toolsWhere(s, toolID("browser."))
			if len(v) > 0 {
				return true, v, "In an isolated browser with a fresh profile; clicking, typing, and downloading ask first, and passwords and payment details are never typed."
			}
			for _, t := range s.Tools {
				if t.Enabled && t.ID == "browser.open" && t.Unavailable != "" {
					return false, nil, strings.ToUpper(t.Unavailable[:1]) + t.Unavailable[1:] + "."
				}
			}
			return false, nil, "The browser is turned off for this profile."
		}},
	{"video_generation", "Make videos", regexp.MustCompile(`(?i)\b(generat|creat|make|render|animat|produc)\w*\b.*\b(videos?|clips?|animations?|movies?|footage)\b`),
		func(s Snapshot) (bool, []string, string) {
			v := toolsWhere(s, toolID("video."))
			if len(v) > 0 {
				return true, v, "A clip of a few seconds takes several minutes."
			}
			for _, t := range s.Tools {
				if t.Enabled && t.ID == "video.generate" && t.Unavailable != "" {
					return false, nil, strings.ToUpper(t.Unavailable[:1]) + t.Unavailable[1:] + "."
				}
			}
			return false, nil, "No video model or video tool is installed."
		}},
	{"vision", "Understand images", regexp.MustCompile(`(?i)\b(see|read|understand|describe|look at|analy[sz]e)\b.*\b(images?|pictures?|photos?|screenshots?)\b`),
		func(s Snapshot) (bool, []string, string) {
			v := modelsWhere(s, func(m Model) bool { return m.Vision })
			if len(v) > 0 {
				return true, v, ""
			}
			return false, nil, "No installed model can see images. Install a vision model on the Models page."
		}},
	{"email", "Read and send email", regexp.MustCompile(`(?i)\b(e-?mail|gmail|inbox|outlook)\b`),
		func(s Snapshot) (bool, []string, string) {
			v := toolsWhere(s, toolAbout(mailRe, nil))
			if len(v) > 0 {
				return true, v, "Sending asks first."
			}
			return false, nil, "No email is connected. Connecting Email in Settings → Connected services, with your mail server and an app password, would add it."
		}},
	{"calendar", "Use a calendar", regexp.MustCompile(`(?i)\bcalendar\b`),
		func(s Snapshot) (bool, []string, string) {
			v := toolsWhere(s, toolAbout(calendarRe, nil))
			if len(v) > 0 {
				return true, v, ""
			}
			return false, nil, "No calendar is connected. Connecting Calendar in Settings → Connected services, with your CalDAV address and an app password, would add it."
		}},
	{"github", "Use GitHub", regexp.MustCompile(`(?i)\bgit ?hub\b`),
		func(s Snapshot) (bool, []string, string) {
			if name, ok := connected(s, "github"); ok {
				return true, []string{name}, ""
			}
			return false, nil, "GitHub is not connected. Connect it in Settings › Connected services."
		}},
	{"home", "Control smart-home devices", regexp.MustCompile(`(?i)\b(home assistant|smart home|lights?|thermostat)\b`),
		func(s Snapshot) (bool, []string, string) {
			if name, ok := connected(s, "homeassistant"); ok {
				return true, []string{name}, "Changes ask first."
			}
			return false, nil, "Home Assistant is not connected. Connect it in Settings › Connected services."
		}},
	{"meaning_search", "Search knowledge by meaning", regexp.MustCompile(`(?i)\b(semantic|by meaning|embedding)\b`),
		func(s Snapshot) (bool, []string, string) {
			v := modelsWhere(s, func(m Model) bool { return m.SupportRole == "embedding" })
			if len(v) > 0 {
				return true, v, ""
			}
			return false, nil, "Knowledge search uses keywords. Install an embedding model to also search by meaning."
		}},
	{"training", "Train specialized AIs", regexp.MustCompile(`(?i)\b(train|fine-?tune)\b`),
		func(s Snapshot) (bool, []string, string) {
			var v []string
			for _, n := range s.Nodes {
				if n.Online && n.Trainer != "" {
					v = append(v, n.Name+" ("+n.Trainer+")")
				}
			}
			if len(v) > 0 {
				return true, v, ""
			}
			return false, nil, "No online computer can train. Training needs Apple Silicon with MLX or an NVIDIA GPU."
		}},
	{"other_computers", "Use paired computers", regexp.MustCompile(`(?i)\b(other|paired|another) (computers?|machines?|nodes?)\b`),
		func(s Snapshot) (bool, []string, string) {
			var v []string
			for _, n := range s.Nodes {
				if !n.Local && n.Online {
					v = append(v, n.Name)
				}
			}
			if len(v) > 0 {
				return true, v, ""
			}
			return false, nil, "No paired computer is online."
		}},
}

// Abilities works out every ability from a snapshot.
func Abilities(s Snapshot) []Ability {
	out := make([]Ability, 0, len(rules))
	for _, r := range rules {
		ok, via, note := r.eval(s)
		a := Ability{ID: r.id, Label: r.label, Available: ok, Via: via, Note: note}
		if !ok {
			for i := range s.Setups {
				if s.Setups[i].Ability == r.id {
					a.Setup = &s.Setups[i]
					break
				}
			}
		}
		out = append(out, a)
	}
	return out
}

// Needs returns the missing ability a request asks for, when it can be set
// up: "Make me an image of a Viking tree" on a computer without image
// generation (Gungnir §29). A question about abilities is not a request.
func Needs(s Snapshot, message string) (Ability, bool) {
	if IsQuestion(message) {
		return Ability{}, false
	}
	all := Abilities(s)
	for i, r := range rules {
		if r.cue.MatchString(message) && !all[i].Available && all[i].Setup != nil {
			return all[i], true
		}
	}
	return Ability{}, false
}

// askRe is a question about what the assistant can do, not a request to do it.
var askRe = regexp.MustCompile(`(?i)^\s*(can|could|are|do|does|is|will|would|which|what)\b.*\b(you|yggdrasil|i)\b.*\b(able to|access|can|do|have|run|support|use|generate|connect)|^\s*(can|could) (you|i|yggdrasil)\b|\bwhich (computers?|nodes?|machines?) can run\b`)

// makeThisRe is a question that names something to make: "are you able to
// make a picture of a dog for me?" asks for the picture, not about
// pictures, so it's a request.
var makeThisRe = regexp.MustCompile(`(?i)\b(draw|paint|sketch|illustrate|make|create|generate|render|produce|design|animate)\b[^?]{0,40}?\b(of|showing|that shows)\s+\w+|\b(draw|paint|sketch|illustrate|animate)\s+(me\s+)?(a|an|the|my|our|some)\s+\w+|\b(draw|paint|sketch|make|create|generate|render|design)\b[^?]{0,60}\bfor me\b`)

// IsQuestion reports a message asking what Yggdrasil can do.
func IsQuestion(message string) bool {
	return strings.Contains(message, "?") && askRe.MatchString(strings.TrimSpace(message)) && !makeThisRe.MatchString(message)
}

// Ask returns the abilities a capability question is about.
func Ask(s Snapshot, message string) []Ability {
	if !IsQuestion(message) {
		return nil
	}
	all := Abilities(s)
	var out []Ability
	for i, r := range rules {
		if r.cue.MatchString(message) {
			out = append(out, all[i])
		}
	}
	return out
}

// Placement is whether a computer can run a model.
type Placement struct {
	Node      string `json:"node"`
	Online    bool   `json:"online"`
	Installed bool   `json:"installed"`
	Fits      bool   `json:"fits"`
	Note      string `json:"note"`
}

// fitShare is the most of a computer's memory a model may need.
const fitShare = 0.75

// NodesFor says which computers can run a model.
func NodesFor(s Snapshot, modelID string) ([]Placement, bool) {
	var m *Model
	for i := range s.Models {
		if s.Models[i].ID == modelID {
			m = &s.Models[i]
		}
	}
	if m == nil {
		return nil, false
	}
	out := []Placement{}
	for _, n := range s.Nodes {
		p := Placement{Node: n.Name, Online: n.Online}
		for _, on := range m.On {
			if on == n.Name {
				p.Installed = true
			}
		}
		p.Fits = n.MemoryBytes == 0 || m.MemoryNeeded == 0 || float64(m.MemoryNeeded) <= float64(n.MemoryBytes)*fitShare
		switch {
		case !n.Online:
			p.Note = "offline"
		case !p.Fits:
			p.Note = fmt.Sprintf("needs about %.1f GB; it has %.1f GB", gb(m.MemoryNeeded), gb(n.MemoryBytes))
		case !p.Installed:
			p.Note = "can run it after downloading it"
		default:
			p.Note = "can run it now"
		}
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i]) > rank(out[j]) })
	return out, true
}

func rank(p Placement) int {
	r := 0
	if p.Online {
		r += 4
	}
	if p.Fits {
		r += 2
	}
	if p.Installed {
		r++
	}
	return r
}

func gb(b uint64) float64 { return float64(b) / (1 << 30) }

var runModelRe = regexp.MustCompile(`(?i)\bwhich (computers?|nodes?|machines?) can run\b`)

// ModelIn finds the installed model a question names, by id or name.
func ModelIn(s Snapshot, message string) (Model, bool) {
	if !runModelRe.MatchString(message) {
		return Model{}, false
	}
	lower := strings.ToLower(message)
	var best Model
	found := false
	for _, m := range s.Models {
		name := strings.ToLower(m.Name)
		if (name != "" && strings.Contains(lower, name)) || strings.Contains(lower, strings.ToLower(m.ID)) {
			if !found || len(m.Name) > len(best.Name) {
				best, found = m, true
			}
		}
	}
	return best, found
}

// Facts writes what a capability question needs to know, for the model to
// answer from (it states facts; it grants nothing).
func Facts(s Snapshot, message string) string {
	var lines []string
	for _, a := range Ask(s, message) {
		line := "- " + a.Label + ": "
		if a.Available {
			line += "yes"
			if len(a.Via) > 0 {
				line += ", via " + strings.Join(a.Via, ", ")
			}
		} else {
			line += "no"
		}
		if a.Note != "" {
			line += ". " + a.Note
		}
		lines = append(lines, line)
	}
	if m, ok := ModelIn(s, message); ok {
		places, _ := NodesFor(s, m.ID)
		for _, p := range places {
			lines = append(lines, fmt.Sprintf("- %s on %s: %s", m.Name, p.Node, p.Note))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "What Toskar can do right now, from its capability inventory. Answer the question from these facts; do not claim abilities that are not listed as yes:\n" +
		strings.Join(lines, "\n")
}

// directMaxWords is the longest question answered straight from the
// inventory; a longer one also asks for something else.
const directMaxWords = 14

// Direct answers a short question about one ability, or about which
// computer can run a model, straight from the inventory, without a model:
// small models ignore the facts and claim abilities they lack (found by the
// quality set, §64).
func Direct(s Snapshot, message string) (string, bool) {
	if !IsQuestion(message) || len(strings.Fields(message)) > directMaxWords {
		return "", false
	}
	if m, ok := ModelIn(s, message); ok {
		places, _ := NodesFor(s, m.ID)
		var lines []string
		for _, p := range places {
			lines = append(lines, fmt.Sprintf("- %s: %s", p.Node, p.Note))
		}
		return m.Name + ":\n" + strings.Join(lines, "\n"), true
	}
	asked := Ask(s, message)
	if len(asked) != 1 {
		return "", false
	}
	a := asked[0]
	what := strings.ToLower(a.Label[:1]) + a.Label[1:]
	var b strings.Builder
	if a.Available {
		fmt.Fprintf(&b, "Yes, I can %s", what)
		if len(a.Via) > 0 {
			fmt.Fprintf(&b, ", using %s", strings.Join(a.Via, ", "))
		}
		b.WriteString(".")
	} else {
		fmt.Fprintf(&b, "No, I can't %s right now.", what)
	}
	if a.Note != "" {
		b.WriteString(" " + a.Note)
	}
	return b.String(), true
}
