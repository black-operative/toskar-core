package huginn

import (
	"regexp"
	"sort"
	"strings"

	"github.com/yeixio/toskar-core/internal/locale"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Specialist is a deployed specialized AI that Auto may route to (spec §61):
// a base model with a LoRA adapter, trained for one job, with tools off.
type Specialist struct {
	// ID is the model id, such as sai:tire-assistant.
	ID   string
	Name string
	Goal string
	// Questions are the user turns of its training examples.
	Questions []string
}

// Thresholds for routing to a specialist on what a message is about.
const (
	// minTopicWords is how many of a message's words must be ones the
	// specialist was trained on.
	minTopicWords = 2
	// minTopicShare is the share of the message's words that must be.
	minTopicShare = 0.4
	// questionDocs is how many training questions must use a word before
	// it counts as part of the specialist's topic.
	questionDocs = 2
)

var termRe = regexp.MustCompile(`[a-z][a-z0-9'-]*`)

// commonWords say nothing about a topic.
var commonWords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`a about above after again against all also am an and any are as ask at be
		because been before being below best between both but by can cannot could did do does doing down during
		each either else ever every few find for from further get gets give go good got had has have having he
		help her here hers him his how however i if in into is it its just know let like make many may me might
		more most much must my need needs no nor not now of off ok okay on once one only or other our out over own
		please right same say see she should show so some such tell than thank thanks that the their them then
		there these they thing things think this those through to too try under until up us use using very want
		was way we well were what when where whether which while who whom why will with would yes yet you your
		yours assistant ai bot question questions answer answers customer customers user users people person`) {
		commonWords[w] = true
	}
}

// stem folds simple plurals, so "tires" matches "tire".
func stem(w string) string {
	switch {
	case len(w) > 4 && strings.HasSuffix(w, "ies"):
		return w[:len(w)-3] + "y"
	case len(w) > 4 && strings.HasSuffix(w, "ses"):
		return w[:len(w)-2]
	case len(w) > 3 && strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss"):
		return w[:len(w)-1]
	}
	return w
}

// topicTerms returns a text's distinctive words, stemmed and deduplicated.
func topicTerms(s string) []string {
	seen := map[string]bool{}
	var out []string
	for _, w := range termRe.FindAllString(strings.ToLower(s), -1) {
		w = strings.Trim(w, "'-")
		if len(w) < 3 || commonWords[w] {
			continue
		}
		w = stem(w)
		if commonWords[w] || seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
	}
	return out
}

// topic is the words a specialist was trained on: its name and goal, and
// words that recur across its training questions.
func (s Specialist) topic() map[string]bool {
	out := map[string]bool{}
	for _, w := range topicTerms(s.Name + " " + s.Goal) {
		out[w] = true
	}
	docs := map[string]int{}
	for _, q := range s.Questions {
		for _, w := range topicTerms(q) {
			docs[w]++
		}
	}
	need := questionDocs
	if len(s.Questions) < questionDocs {
		need = 1
	}
	for w, n := range docs {
		if n >= need {
			out[w] = true
		}
	}
	return out
}

// named reports whether a message mentions the specialist by name.
func (s Specialist) named(message string) bool {
	name := strings.ToLower(strings.TrimSpace(s.Name))
	if len(name) < 3 {
		return false
	}
	re := regexp.MustCompile(`(^|[^a-z0-9])` + regexp.QuoteMeta(name) + `($|[^a-z0-9])`)
	return re.MatchString(strings.ToLower(message))
}

// ChooseSpecialist picks a specialized AI for a message, or none (spec
// §61). A message that names one goes to it. Otherwise the message must be
// mostly about what one was trained on. Requests that need tools, current
// information, or code never go to a specialist, because it answers from
// its training alone.
func ChooseSpecialist(message string, k Kind, list []Specialist) (Specialist, locale.Text, bool) {
	if k == Current || k == Local || k == Coding || len(list) == 0 {
		return Specialist{}, locale.Text{}, false
	}
	for _, s := range list {
		if s.named(message) {
			return s, locale.Key("chat:steps.specialistNamed", map[string]any{"name": s.Name}), true
		}
	}
	terms := topicTerms(message)
	if len(terms) == 0 {
		return Specialist{}, locale.Text{}, false
	}
	var best Specialist
	var bestHits []string
	for _, s := range list {
		topic := s.topic()
		var hits []string
		for _, w := range terms {
			if topic[w] {
				hits = append(hits, w)
			}
		}
		if len(hits) > len(bestHits) {
			best, bestHits = s, hits
		}
	}
	if len(bestHits) < minTopicWords || float64(len(bestHits)) < minTopicShare*float64(len(terms)) {
		return Specialist{}, locale.Text{}, false
	}
	sort.Strings(bestHits)
	if len(bestHits) > 3 {
		bestHits = bestHits[:3]
	}
	// The words are the message's own, so they stay as written.
	return best, locale.Key("chat:steps.specialistTopic", map[string]any{"name": best.Name, "words": strings.Join(bestHits, ", ")}), true
}

// Supporting reports a model that serves Yggdrasil instead of chatting: an
// embedding or reranker model for Mimir, or a classifier for Huginn (spec
// §61). Auto never picks one to answer, and neither does fallback.
func Supporting(m contracts.Model) bool { return contracts.SupportRoleOf(m) != "" }

// SupportRoleOf names a supporting model's job, or "" for a chat model.
func SupportRoleOf(m contracts.Model) string { return contracts.SupportRoleOf(m) }
