package huginn

import (
	"regexp"
	"slices"
	"strings"

	"github.com/yeixio/toskar-core/internal/tools"
)

// Tool groups, by capability. A request is offered whole groups.
var toolGroups = map[string][]string{
	"web":     {"internet.search", "internet.open"},
	"read":    {"filesystem.search", "filesystem.read"},
	"write":   {"filesystem.write"},
	"create":  {"files.create"},
	"sheet":   {"spreadsheet.analyze"},
	"code":    {"code.execute"},
	"listen":  {"speech.transcribe"},
	"speak":   {"speech.synthesize"},
	"draw":    {"image.generate"},
	"film":    {"video.generate"},
	"browse":  {"browser.open", "browser.click", "browser.type", "browser.extract", "browser.download", "browser.screenshot", "browser.close"},
	"places":  {"places.search", "places.details", "maps.route", "maps.distance"},
	"retouch": {"image.edit"},
	"shell":   {"terminal"},
	"gitr":    {"git.status", "git.diff", "git.log", "git.show"},
	"gitw":    {"git.add", "git.commit", "git.push"},
}

// Groups each kind of request gets before cues in the message add more.
var kindGroups = map[Kind][]string{
	// A plain question is answered directly; small models offered tools
	// for one reach for them (found by the quality set, §64). Cues in the
	// message still add what it asks for.
	Chat:     {},
	Current:  {"web", "create"},
	Research: {"web", "create", "read"},
	Coding:   {"web", "create", "read", "write", "shell", "gitr"},
	Local:    {"read", "write", "create", "shell", "gitr", "gitw"},
}

var (
	cueRead    = regexp.MustCompile(`(?i)(\b(files?|folders?|director(y|ies)|documents?|workspace|repo|repository|project|readme|log file)\b|[~./][\w./-]*/[\w.-]+|\b\w+\.(go|py|js|ts|tsx|md|txt|json|ya?ml|toml|csv|log|sh)\b)`)
	cueWrite   = regexp.MustCompile(`(?i)\b(save|write|edit|update|change|fix|rename|append|create)\b.{0,40}\b(file|files|folder|config|readme|script)\b`)
	cueShell   = regexp.MustCompile(`(?i)\b(run|execute|install|build|compile|terminal|command|shell|script|npm|pnpm|pip|brew|make|go test|go build)\b`)
	cueGit     = regexp.MustCompile(`(?i)\b(git|commit|branch|diff|merge|rebase|staged|push|pull request)\b`)
	cueGitW    = regexp.MustCompile(`(?i)\b(commit|stage|push)\b`)
	cueMake    = regexp.MustCompile(`(?i)\b(make|create|write|generate|export|save|build)\b.{0,40}\b(files?|spreadsheets?|documents?|docs?|csv|xlsx|pdf|tables?|reports?|lists?)\b`)
	cueCode    = regexp.MustCompile(`(?i)\b(calculat\w*|comput\w*|analy[sz]\w*|plot\w*|charts?|graphs?|python|statistic\w*|regression|correlat\w*|simulat\w*|forecast\w*|average|median|percentiles?|run (this|the|my) code)\b`)
	cueListen  = regexp.MustCompile(`(?i)\b(transcri\w*|recordings?|voice (notes?|memos?|messages?)|audio|podcasts?|what (does|did) (it|she|he|they) say)\b`)
	cueSpeak   = regexp.MustCompile(`(?i)(\b(read (it|this|that|them)?\s*(aloud|out loud)|out loud|aloud|text to speech|narrat\w*|voice ?over|audio version|say it)\b)`)
	cueDraw    = regexp.MustCompile(`(?i)(\b(draw|paint|sketch|illustrat\w*|render)\b|\b(make|create|generate|design|produce|give me)\b.{0,40}\b(images?|pictures?|photos?|illustrations?|drawings?|paintings?|logos?|icons?|wallpapers?|artwork|portraits?|posters?)\b)`)
	cueRetouch = regexp.MustCompile(`(?i)\b(edit|change|retouch|recolou?r|remove|replace|turn|make|add)\b.{0,40}\b(images?|pictures?|photos?|backgrounds?|\w+\.(png|jpe?g))\b`)
	cuePlaces  = regexp.MustCompile(`(?i)(\b(near (me|here|by)|nearby|nearest|closest|directions?|route (from|between)|how (far|long does it take)|distance (from|to|between)|drive (from|to)|walk (from|to)|get (from|to)|address (of|for)|open now|opening hours|restaurants?|caf[eé]s?|pharmac(y|ies)|gas stations?)\b)`)
	cueFilm    = regexp.MustCompile(`(?i)\b(videos?|clips?|animat\w*|movies?|footage|gifs?|bring (it|this|that|the|my)( \w+)? to life|make (it|this) move)\b`)
	cueBrowse  = regexp.MustCompile(`(?i)(\b(browser|click|fill (in|out)|sign up|add to (my )?cart|on (the|that|their) (site|page|website)|log ?in to|screenshot|navigate to)\b|\bgo to \S+\.\w{2,}\b)`)
	cueSheet   = regexp.MustCompile(`(?i)\b(spreadsheets?|xlsx|csv|excel|workbooks?|sheets?)\b`)
	cueWeb     = regexp.MustCompile(`(?i)(\b(search|web|online|internet|look up|website|url|links?|news|latest)\b|https?://)`)
)

// ToolsFor picks the tools worth offering for a request (spec §16): the
// groups its kind needs, plus any the message asks for, limited to the
// tools the profile has. Offering fewer tools keeps small models from
// reaching for the wrong one, and keeps the prompt short.
func ToolsFor(k Kind, message string, available []string) []string {
	want := map[string]bool{}
	for _, g := range kindGroups[k] {
		want[g] = true
	}
	if cueRead.MatchString(message) {
		want["read"] = true
	}
	if cueWrite.MatchString(message) {
		want["read"], want["write"] = true, true
	}
	if cueShell.MatchString(message) {
		want["shell"] = true
	}
	if cueGit.MatchString(message) {
		want["gitr"] = true
		if cueGitW.MatchString(message) {
			want["gitw"] = true
		}
	}
	if cueWeb.MatchString(message) || tools.MessageAsksToFind(message) {
		want["web"] = true
	}
	if cueMake.MatchString(message) {
		want["create"] = true
	}
	if cueSheet.MatchString(message) {
		want["sheet"] = true
	}
	if cueCode.MatchString(message) {
		want["code"] = true
	}
	if cueListen.MatchString(message) {
		want["listen"] = true
	}
	if cueSpeak.MatchString(message) {
		want["speak"] = true
	}
	if cuePlaces.MatchString(message) {
		want["places"] = true
	}
	if cueBrowse.MatchString(message) {
		want["browse"] = true
	}
	if cueFilm.MatchString(message) {
		want["film"] = true
	}
	if cueDraw.MatchString(message) {
		want["draw"] = true
	}
	if cueRetouch.MatchString(message) {
		want["retouch"] = true
	}
	var out []string
	for _, id := range available {
		for g, ids := range toolGroups {
			if want[g] && slices.Contains(ids, id) {
				out = append(out, id)
				break
			}
		}
		// Tools outside the built-in groups, such as connected services
		// and MCP tool sources, are offered when the message names the
		// service or its subject, or always when the person said so.
		// One that changes something also needs the message to ask for a
		// change, so a question is never offered a way to act.
		if grouped(id) {
			continue
		}
		def, known := tools.Lookup(id)
		if (known && def.Always) || aboutService(strings.SplitN(id, ".", 2)[0], message) {
			if known && def.Risk == tools.RiskWrite && !cueAction.MatchString(message) {
				continue
			}
			out = append(out, id)
		}
	}
	return out
}

// serviceCues are what a message says when it is about a connected service
// without naming it (§32).
var serviceCues = map[string]*regexp.Regexp{
	"github":        regexp.MustCompile(`(?i)\b(git ?hub|issues?|pull requests?|PRs?)\b`),
	"email":         regexp.MustCompile(`(?i)\b(e-?mails?|inbox|mail|messages? from|unread|newsletters?|replies|reply to)\b`),
	"calendar":      regexp.MustCompile(`(?i)\b(calendar|schedule|meetings?|appointments?|events?|agenda|free (time|on|at|this|next|tomorrow)|busy|available|availability|book (a|an|time)|reschedule|tomorrow|this week|next week)\b`),
	"homeassistant": regexp.MustCompile(`(?i)(\bhome ?assistant\b|\b(lights?|lamps?|thermostat|heating|switch(es)?|sensors?|garage door|front door|locks?|fans?|blinds)\b)`),
}

// cueAction is a message asking to change something rather than to know.
var cueAction = regexp.MustCompile(`(?i)\b(turn|switch|set|dim|brighten|open|close|lock|unlock|start|stop|toggle|activate|run|comment|reply|respond|post|write|add|reopen|label|assign|tell them|send|draft|archive|schedule|book|reschedule|move|cancel|create|put)\b`)

func aboutService(service, message string) bool {
	lower := strings.ToLower(message)
	if strings.Contains(lower, service) {
		return true
	}
	if re, ok := serviceCues[service]; ok && re.MatchString(message) {
		return true
	}
	// Words the service itself taught, such as its device names.
	for _, def := range tools.ConnectedDefinitions() {
		if !strings.HasPrefix(def.ID, service+".") {
			continue
		}
		for _, cue := range def.Cues {
			if containsWord(lower, strings.ToLower(cue)) {
				return true
			}
		}
	}
	return false
}

func containsWord(text, word string) bool {
	if len(word) < 3 {
		return false
	}
	for i := 0; ; {
		k := strings.Index(text[i:], word)
		if k < 0 {
			return false
		}
		start, end := i+k, i+k+len(word)
		before := start == 0 || !isWordByte(text[start-1])
		after := end == len(text) || !isWordByte(text[end])
		if before && after {
			return true
		}
		i = start + 1
	}
}

func isWordByte(b byte) bool {
	return b == '_' || b >= 'a' && b <= 'z' || b >= '0' && b <= '9'
}

func grouped(id string) bool {
	for _, ids := range toolGroups {
		if slices.Contains(ids, id) {
			return true
		}
	}
	return false
}
