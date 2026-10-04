package huginn

import (
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/yeixio/toskar-core/internal/locale"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// AutoModelID is the model a chat asks for when it wants Yggdrasil to choose.
const AutoModelID = "auto"

// Shares of this computer's memory a model may need. A quick question gets a
// model that loads and answers fast; harder requests may use more.
const (
	quickShare = 0.40
	fullShare  = 0.60
)

// Choice is the model picked for a request and what it was picked for.
// Reason says why in plain language.
type Choice struct {
	Model  contracts.Model
	Kind   Kind
	Effort Effort
	// Lang is the answer's language when the model was picked for writing
	// it well, or "".
	Lang string
	// LanguageWeak is set when the model is expected to write the answer's
	// language materially worse and nothing installed that fits does
	// better (§16), so the user can be told.
	LanguageWeak bool
	// Rated is set when community ratings chose a model other than the one
	// Auto would pick without them.
	Rated bool
	// AvoidedBusy is set when the best model was busy answering something
	// else, so a similar idle one was chosen; Busy is that model.
	AvoidedBusy bool
	Busy        contracts.Model
}

// Signals are what Auto weighs beyond the request itself (spec §13).
type Signals struct {
	// Rating is each model's community signal by model ID: its
	// confidence-weighted score less the average, above 0 rated better.
	Rating map[string]float64
	// Busy are the models loaded here that are already answering something.
	Busy map[string]bool
}

// ratingLead is how much better rated a model must be to be chosen over a
// larger one; closer than that, size decides.
const ratingLead = 0.5

// Reason says why the model was picked, in the App language app, such as
// "Auto chose Qwen 2.5 7B for a coding question at Thorough effort".
func (c Choice) Reason(app string) string {
	key := "chat:steps.autoChose"
	params := map[string]any{"model": Name(c.Model), "kind": c.Kind.Describe(app)}
	if c.Lang != "" {
		key += "In"
		params["language"] = locale.LanguageName(c.Lang, app)
	}
	if c.Effort == EffortFast || c.Effort == EffortThorough {
		key += "Effort"
		params["effort"] = c.Effort.Describe(app)
	}
	reason := locale.T(app, key, params)
	switch {
	case c.AvoidedBusy:
		reason = locale.T(app, "chat:steps.autoWhy", map[string]any{"choice": reason,
			"why": locale.T(app, "chat:steps.autoBusy", map[string]any{"model": Name(c.Busy)})})
	case c.Rated:
		reason = locale.T(app, "chat:steps.autoWhy", map[string]any{"choice": reason, "why": locale.T(app, "chat:steps.autoRated", nil)})
	}
	return reason
}

func has(list []string, v string) bool { return slices.Contains(list, v) }

func toolRank(c contracts.ModelCapabilities) int {
	switch c.ToolCallSupport {
	case "native":
		return 3
	case "compatible":
		return 2
	case "limited":
		return 1
	case "unsupported":
		return 0
	}
	if c.ToolCalling {
		return 2
	}
	return 0
}

// general reports a model meant for conversation. Vision models and models
// that think out loud before answering are kept for when nothing else fits.
func general(m contracts.Model) bool {
	if has(m.Tags, "vision") {
		return false
	}
	if has(m.Tags, "reasoning") && !has(m.Tags, "general") {
		return false
	}
	if len(m.Purpose) > 0 && !has(m.Purpose, "general") && !has(m.Purpose, "assistant") && !has(m.Purpose, "research") {
		return false
	}
	return true
}

// suits reports whether a model is a good match for a kind of request.
func suits(k Kind, m contracts.Model) bool {
	switch k {
	case Coding:
		return m.Capabilities.Coding || has(m.Tags, "coding")
	case Current, Local:
		return toolRank(m.Capabilities) >= 2 && (general(m) || m.Capabilities.Coding)
	case Research:
		return general(m) && (has(m.Purpose, "research") || has(m.Tags, "reasoning") || has(m.Tags, "large") || has(m.Tags, "general"))
	default:
		return general(m)
	}
}

func fits(m contracts.Model, memTotal uint64, share float64) bool {
	if memTotal == 0 || m.MemoryNeeded == 0 {
		return true
	}
	return float64(m.MemoryNeeded) <= float64(memTotal)*share
}

// bigger prefers the model that needs more memory, a stand-in for quality.
func bigger(a, b contracts.Model) bool { return a.MemoryNeeded > b.MemoryNeeded }

func best(models []contracts.Model, keep func(contracts.Model) bool, better func(a, b contracts.Model) bool) (contracts.Model, bool) {
	var out contracts.Model
	found := false
	for _, m := range models {
		if !keep(m) {
			continue
		}
		if !found || better(m, out) {
			out, found = m, true
		}
	}
	return out, found
}

func running(m contracts.Model) bool { return m.Status == "running" }

// Choose picks an installed model for a request (spec §13). memTotal is this
// computer's memory in bytes, or 0 when unknown. It returns false when no
// model is installed.
func Choose(k Kind, installed []contracts.Model, memTotal uint64) (Choice, bool) {
	return ChooseFor(k, EffortAuto, installed, memTotal)
}

// ChooseFor is Choose with an effort: Fast keeps any request on a quick,
// already-loaded model where one suits; Thorough takes the largest suitable
// model that fits, even for a quick question.
func ChooseFor(k Kind, e Effort, installed []contracts.Model, memTotal uint64) (Choice, bool) {
	return ChooseIn(k, e, "", installed, memTotal)
}

// ChooseIn is ChooseFor for an answer in lang, a BCP 47 tag such as "es"
// (§15–16): at each step, a model that writes the language better comes
// before a larger one, and a loaded model is kept for a quick request only
// when nothing that fits writes the language better. "" leaves language
// out of the choice.
func ChooseIn(k Kind, e Effort, lang string, installed []contracts.Model, memTotal uint64) (Choice, bool) {
	return ChooseWith(k, e, lang, installed, memTotal, Signals{})
}

// ChooseWith is ChooseIn weighing community ratings and load: a model rated
// clearly better (by ratingLead) comes before a larger one, and a model busy
// answering something else gives way to a similar idle one that is loaded.
func ChooseWith(k Kind, e Effort, lang string, installed []contracts.Model, memTotal uint64, sig Signals) (Choice, bool) {
	c, ok := choose(k, e, lang, installed, memTotal, sig)
	if ok && len(sig.Rating) > 0 {
		plain, _ := choose(k, e, lang, installed, memTotal, Signals{Busy: sig.Busy})
		c.Rated = plain.Model.ID != c.Model.ID
	}
	return c, ok
}

func choose(k Kind, e Effort, lang string, installed []contracts.Model, memTotal uint64, sig Signals) (Choice, bool) {
	var models []contracts.Model
	for _, m := range installed {
		if m.Installed && !Supporting(m) {
			models = append(models, m)
		}
	}
	if len(models) == 0 {
		return Choice{}, false
	}
	quick := (k == Chat && e != EffortThorough) || e == EffortFast
	share := fullShare
	if quick {
		share = quickShare
	}
	// A model that writes the answer's language better comes first; then
	// one the community rates clearly better; among equals, the larger one.
	better := func(a, b contracts.Model) bool {
		if ra, rb := languageRank(a, lang), languageRank(b, lang); ra != rb {
			return ra > rb
		}
		if ra, rb := sig.Rating[a.ID], sig.Rating[b.ID]; math.Abs(ra-rb) >= ratingLead {
			return ra > rb
		}
		return bigger(a, b)
	}
	idle := func(m contracts.Model) bool { return !sig.Busy[m.ID] }
	// The best language rank of a model that fits at all, to tell whether a
	// weak pick could have been better (§16).
	bestRank := 0
	for _, m := range models {
		if fits(m, memTotal, fullShare) {
			bestRank = max(bestRank, languageRank(m, lang))
		}
	}
	pick := func(m contracts.Model) (Choice, bool) {
		c := Choice{Model: m, Kind: k, Effort: e, LanguageWeak: WeakIn(m, lang) && languageRank(m, lang) >= bestRank}
		if lang != "" && !strings.HasPrefix(lang, "en") && languageRank(m, lang) >= rankGood {
			c.Lang = lang
		}
		return c, true
	}

	// A quick request goes to a suitable model that is already loaded, so it
	// is not slowed by loading another, unless one that fits writes the
	// answer's language better.
	if quick {
		loaded := func(m contracts.Model) bool { return running(m) && suits(k, m) && fits(m, memTotal, fullShare) }
		m, ok := best(models, func(m contracts.Model) bool { return loaded(m) && idle(m) }, better)
		if !ok {
			m, ok = best(models, loaded, better)
		}
		if ok {
			top, _ := best(models, func(m contracts.Model) bool { return suits(k, m) && fits(m, memTotal, share) }, better)
			if top.ID == "" || languageRank(m, lang) >= languageRank(top, lang) {
				return pick(m)
			}
		}
	}
	if m, ok := best(models, func(m contracts.Model) bool { return suits(k, m) && fits(m, memTotal, share) }, better); ok {
		// Busy answering something else: a loaded, idle model that writes
		// the language as well, is at least half the size, and isn't rated
		// clearly worse answers sooner.
		if sig.Busy[m.ID] {
			if alt, ok := best(models, func(x contracts.Model) bool {
				return running(x) && idle(x) && suits(k, x) && fits(x, memTotal, share) &&
					languageRank(x, lang) >= languageRank(m, lang) && x.MemoryNeeded*2 >= m.MemoryNeeded &&
					sig.Rating[x.ID] > sig.Rating[m.ID]-ratingLead
			}, better); ok {
				c, _ := pick(alt)
				c.AvoidedBusy, c.Busy = true, m
				return c, true
			}
		}
		return pick(m)
	}
	// Nothing ideal: any conversational model that fits, then anything that
	// fits, then the smallest installed model.
	if m, ok := best(models, func(m contracts.Model) bool { return general(m) && fits(m, memTotal, fullShare) }, better); ok {
		return pick(m)
	}
	if m, ok := best(models, func(m contracts.Model) bool { return fits(m, memTotal, fullShare) }, better); ok {
		return pick(m)
	}
	m, _ := best(models, func(contracts.Model) bool { return true }, func(a, b contracts.Model) bool { return a.MemoryNeeded < b.MemoryNeeded })
	return pick(m)
}

// Fallback picks the model to try after failed could not answer (spec §14,
// §26): a conversational model that needs no more memory than the one that
// failed, preferring the largest such model, else the smallest other model.
func Fallback(failed string, installed []contracts.Model, memTotal uint64) (contracts.Model, bool) {
	var failedModel contracts.Model
	var others []contracts.Model
	for _, m := range installed {
		if !m.Installed || Supporting(m) {
			continue
		}
		if m.ID == failed {
			failedModel = m
			continue
		}
		others = append(others, m)
	}
	if len(others) == 0 {
		return contracts.Model{}, false
	}
	limit := failedModel.MemoryNeeded
	noLarger := func(m contracts.Model) bool {
		return limit == 0 || m.MemoryNeeded == 0 || m.MemoryNeeded <= limit
	}
	if m, ok := best(others, func(m contracts.Model) bool { return general(m) && noLarger(m) && fits(m, memTotal, fullShare) }, bigger); ok {
		return m, true
	}
	return best(others, func(m contracts.Model) bool { return general(m) || m.Capabilities.Coding }, func(a, b contracts.Model) bool { return a.MemoryNeeded < b.MemoryNeeded })
}

// Smaller reports whether falling back from a to b is likely to give a less
// capable answer, so the user should be told.
func Smaller(a, b contracts.Model) bool {
	return a.MemoryNeeded > 0 && b.MemoryNeeded > 0 && b.MemoryNeeded < a.MemoryNeeded*3/4
}

// Name is how a model is shown to the user.
func Name(m contracts.Model) string {
	if m.DisplayName != "" {
		return m.DisplayName
	}
	return m.ID
}

// smallBelow is the size, in billions of parameters, under which a model is
// small: fast, but more likely to mix up facts and numbers.
const smallBelow = 4.0

// Billions reads a model's size from its Parameters label, such as "1B",
// "3.8B", or "500M". It returns false when the size is unknown.
func Billions(m contracts.Model) (float64, bool) {
	p := strings.ToUpper(strings.TrimSpace(m.Parameters))
	scale := 1.0
	switch {
	case strings.HasSuffix(p, "B"):
		p = strings.TrimSuffix(p, "B")
	case strings.HasSuffix(p, "M"):
		p, scale = strings.TrimSuffix(p, "M"), 0.001
	default:
		return 0, false
	}
	v, err := strconv.ParseFloat(p, 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	return v * scale, true
}

// Small reports whether a model is small enough that its answers about
// files and data deserve a second look. Unknown sizes are not small.
func Small(m contracts.Model) bool {
	b, ok := Billions(m)
	return ok && b < smallBelow
}

// Larger returns the best installed model that is not small and fits this
// computer, to suggest instead of a small one.
func Larger(installed []contracts.Model, memTotal uint64) (contracts.Model, bool) {
	return best(installed, func(m contracts.Model) bool {
		return m.Installed && !Supporting(m) && !Small(m) && general(m) && fits(m, memTotal, fullShare)
	}, bigger)
}
