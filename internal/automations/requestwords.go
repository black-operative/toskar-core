package automations

import (
	"encoding/json"
	"io/fs"
	"math"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/dlclark/regexp2"
	toskar "github.com/yeixio/toskar-core"
	"golang.org/x/text/unicode/norm"
)

// The words an automation request is read with, one file per language in
// the shared catalog, i18n/requests/<language>.json, which the web app reads
// too (#204). The file format is described in
// web/src/features/automations/requestWords/types.ts. This is a port of
// requestWords/match.ts: phrases become regular expressions with the same
// marks and the same whole-word rules.

// requestWordList is one language's words.
type requestWordList struct {
	Spaced   bool               `json:"spaced"`
	Numbers  map[string]float64 `json:"numbers"`
	Tens     string             `json:"tens"`
	Counters []string           `json:"counters"`
	Interval struct {
		Before []string `json:"before"`
		After  []string `json:"after"`
		Units  struct {
			Second []string `json:"second"`
			Minute []string `json:"minute"`
			Hour   []string `json:"hour"`
			Day    []string `json:"day"`
		} `json:"units"`
		Hourly   []string `json:"hourly"`
		HalfHour []string `json:"halfHour"`
	} `json:"interval"`
	Daily    map[string][]string `json:"daily"`
	Dayparts map[string][]string `json:"dayparts"`
	Weekly   struct {
		Before []string   `json:"before"`
		After  []string   `json:"after"`
		Days   [][]string `json:"days"`
		Alone  [][]string `json:"alone"`
	} `json:"weekly"`
	Once struct {
		Once        []string `json:"once"`
		Today       []string `json:"today"`
		Tomorrow    []string `json:"tomorrow"`
		NotTomorrow []string `json:"notTomorrow"`
	} `json:"once"`
	Clock struct {
		At            []string `json:"at"`
		Hour          []string `json:"hour"`
		Minute        []string `json:"minute"`
		Half          []string `json:"half"`
		AM            []string `json:"am"`
		PM            []string `json:"pm"`
		MeridiemFirst bool     `json:"meridiemFirst"`
	} `json:"clock"`
	Notify struct {
		None        []string `json:"none"`
		Change      []string `json:"change"`
		Significant []string `json:"significant"`
		Available   []string `json:"available"`
		Below       []string `json:"below"`
		Above       []string `json:"above"`
		BelowAfter  []string `json:"belowAfter"`
		AboveAfter  []string `json:"aboveAfter"`
	} `json:"notify"`
	Currencies  map[string]string  `json:"currencies"`
	Multipliers map[string]float64 `json:"multipliers"`
	Currency    string             `json:"currency"`
	Names       struct {
		Stock    []string `json:"stock"`
		Release  []string `json:"release"`
		Research []string `json:"research"`
	} `json:"names"`
	Task struct {
		DropAtEnd   []string `json:"dropAtEnd"`
		Price       []string `json:"price"`
		ReportPrice string   `json:"reportPrice"`
		FullStop    string   `json:"fullStop"`
	} `json:"task"`

	code     string
	mu       sync.Mutex
	compiled map[string]*regexp2.Regexp
}

var (
	wordsOnce  sync.Once
	wordsByTag map[string]*requestWordList
	wordsOrder []*requestWordList
)

// requestLanguagesAll is every language's words, by catalog code.
func requestLanguagesAll() (map[string]*requestWordList, []*requestWordList) {
	wordsOnce.Do(func() {
		wordsByTag = map[string]*requestWordList{}
		files, _ := fs.Glob(toskar.Catalog, "i18n/requests/*.json")
		sort.Strings(files)
		for _, file := range files {
			raw, err := fs.ReadFile(toskar.Catalog, file)
			if err != nil {
				continue
			}
			var w requestWordList
			if json.Unmarshal(raw, &w) != nil {
				continue
			}
			w.code = strings.TrimSuffix(path.Base(file), ".json")
			wordsByTag[w.code] = &w
			wordsOrder = append(wordsOrder, &w)
		}
	})
	return wordsByTag, wordsOrder
}

// requestWordsFor is a language tag's words: an exact match, then one with
// the same base language (pt → pt-BR).
func requestWordsFor(tag string) *requestWordList {
	byTag, order := requestLanguagesAll()
	lower := strings.ToLower(tag)
	for code, w := range byTag {
		if strings.ToLower(code) == lower {
			return w
		}
	}
	base := strings.SplitN(lower, "-", 2)[0]
	for _, w := range order {
		if strings.SplitN(strings.ToLower(w.code), "-", 2)[0] == base {
			return w
		}
	}
	return nil
}

// requestLanguages is the words to read a request with, in order: the
// language's own, then English, which everyone can write in.
func requestLanguages(tag string) []*requestWordList {
	en := requestWordsFor("en")
	own := requestWordsFor(tag)
	if own != nil && own != en {
		return []*requestWordList{own, en}
	}
	return []*requestWordList{en}
}

// pattern compiles a regular expression built from the words once.
func (w *requestWordList) pattern(name string, source func() string) *regexp2.Regexp {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.compiled == nil {
		w.compiled = map[string]*regexp2.Regexp{}
	}
	if re, ok := w.compiled[name]; ok {
		return re
	}
	text := source()
	var re *regexp2.Regexp
	if text != "" {
		re = regexp2.MustCompile(text, regexp2.Unicode)
	}
	w.compiled[name] = re
	return re
}

func (w *requestWordList) gap() string {
	if w.Spaced {
		return `\s+`
	}
	return `\s*`
}

// normalizeRequest is a request as phrases are matched against it: NFKC,
// lowercase, plain apostrophes, single spaces.
func normalizeRequest(text string) string {
	text = strings.ToLower(norm.NFKC.String(text))
	text = strings.NewReplacer("’", "'", "‘", "'").Replace(text)
	return strings.Join(strings.Fields(text), " ")
}

// withDigits reads numerals written in characters before a counter, so 八点
// becomes 8点.
func withDigits(text string, w *requestWordList) string {
	if w.Tens == "" || len(w.Counters) == 0 {
		return text
	}
	var chars []string
	for key := range w.Numbers {
		if len([]rune(key)) == 1 {
			chars = append(chars, escapeClass(key))
		}
	}
	sort.Strings(chars)
	re := w.pattern("digits", func() string {
		return "[" + strings.Join(chars, "") + "]+(?=\\s*(?:" + alternatives(w.Counters) + "))"
	})
	out, err := re.ReplaceFunc(text, func(m regexp2.Match) string {
		if v, ok := readNumeral(m.String(), w.Numbers, w.Tens); ok {
			return strconv.Itoa(int(v))
		}
		return m.String()
	}, -1, -1)
	if err != nil {
		return text
	}
	return out
}

func readNumeral(numeral string, numbers map[string]float64, tens string) (float64, bool) {
	if v, ok := numbers[numeral]; ok && numeral != tens {
		return v, true
	}
	at := strings.Index(numeral, tens)
	if at < 0 {
		return 0, false
	}
	before, after := numeral[:at], numeral[at+len(tens):]
	ten, one := 1.0, 0.0
	if before != "" {
		v, ok := numbers[before]
		if !ok {
			return 0, false
		}
		ten = v
	}
	if after != "" {
		v, ok := numbers[after]
		if !ok {
			return 0, false
		}
		one = v
	}
	return ten*10 + one, true
}

var phraseCache sync.Map // string → *regexp2.Regexp

// hasPhrase reports whether any of the phrases is in the text.
func hasPhrase(text string, list []string, spaced bool) bool {
	if len(list) == 0 {
		return false
	}
	key := strconv.FormatBool(spaced) + "\x00" + strings.Join(list, "\x00")
	re, ok := phraseCache.Load(key)
	if !ok {
		re, _ = phraseCache.LoadOrStore(key, regexp2.MustCompile(phraseSource(list, spaced, true, true), regexp2.Unicode))
	}
	found, _ := re.(*regexp2.Regexp).MatchString(text)
	return found
}

// phraseSource is the phrases as one non-capturing group, longest first, or
// a group that never matches. start and end ask for whole-word edges.
func phraseSource(list []string, spaced, start, end bool) string {
	if len(list) == 0 {
		return "(?!)"
	}
	sorted := append([]string(nil), list...)
	sort.SliceStable(sorted, func(i, j int) bool { return len(sorted[i]) > len(sorted[j]) })
	parts := make([]string, len(sorted))
	for i, phrase := range sorted {
		parts[i] = compilePhrase(phrase, spaced, start, end)
	}
	return "(?:" + strings.Join(parts, "|") + ")"
}

var (
	startsWithWord = regexp.MustCompile(`^\(*[\p{L}\p{N}]`)
	endsWithWord   = regexp.MustCompile(`[\p{L}\p{N}).?]$`)
)

func compilePhrase(phrase string, spaced, start, end bool) string {
	var b strings.Builder
	for _, ch := range phrase {
		switch ch {
		case '(':
			b.WriteString("(?:")
		case ')', '|', '?':
			b.WriteRune(ch)
		case '…':
			b.WriteString(".{0,40}?")
		case '#':
			b.WriteString(amountSource())
		case ' ':
			if spaced {
				b.WriteString(`\s+`)
			} else {
				b.WriteString(`\s*`)
			}
		default:
			b.WriteString(escapeRegexp(string(ch)))
		}
	}
	out := b.String()
	if !spaced {
		return out
	}
	// Whole words only: "unter" is not the start of "unterhalb".
	prefix, suffix := "", ""
	if start && startsWithWord.MatchString(phrase) {
		prefix = `(?<![\p{L}\p{N}])`
	}
	if end && endsWithWord.MatchString(phrase) {
		suffix = `(?![\p{L}\p{N}])`
	}
	return prefix + "(?:" + out + ")" + suffix
}

// alternatives is the words as plain alternatives, longest first.
func alternatives(list []string) string {
	sorted := append([]string(nil), list...)
	sort.SliceStable(sorted, func(i, j int) bool { return len(sorted[i]) > len(sorted[j]) })
	for i, s := range sorted {
		sorted[i] = escapeRegexp(s)
	}
	return strings.Join(sorted, "|")
}

func escapeRegexp(text string) string {
	var b strings.Builder
	for _, ch := range text {
		if strings.ContainsRune(`.*+?^${}()|[]\/`, ch) {
			b.WriteByte('\\')
		}
		b.WriteRune(ch)
	}
	return b.String()
}

func escapeClass(ch string) string {
	if strings.ContainsAny(ch, `]\^-`) {
		return `\` + ch
	}
	return ch
}

// Amounts: $500, 500 €, R$ 2.500, 1 299,99 €, 5万円, 50만 원.

const numberSource = `\d{1,3}(?:[.,\u00a0\u202f' ]\d{3})+(?:[.,]\d{1,2})?|\d+(?:[.,]\d+)?`

var (
	amountOnce    sync.Once
	amountPattern string
	amountReader  *regexp2.Regexp
)

func buildAmount() {
	amountOnce.Do(func() {
		_, order := requestLanguagesAll()
		symbols, multipliers := map[string]bool{}, map[string]bool{}
		for _, w := range order {
			for s := range w.Currencies {
				symbols[s] = true
			}
			for m := range w.Multipliers {
				multipliers[m] = true
			}
		}
		symbol, multiplier := tokens(keys(symbols)), tokens(keys(multipliers))
		amountPattern = `(?:` + symbol + `\s*)?(?:` + numberSource + `)(?:\s*` + multiplier + `)?(?:\s*` + symbol + `)?`
		amountReader = regexp2.MustCompile(`^(?:(`+symbol+`)\s*)?(`+numberSource+`)(?:\s*(`+multiplier+`))?(?:\s*(`+symbol+`))?`, regexp2.Unicode)
	})
}

// amountSource is an amount with an optional currency before or after it,
// without capturing groups.
func amountSource() string {
	buildAmount()
	return amountPattern
}

func keys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// tokens is the list as alternatives; a Latin-letter token such as "eur" or
// "k" must not run into a word ("europe", "km").
func tokens(list []string) string {
	if len(list) == 0 {
		return "(?!)"
	}
	sorted := append([]string(nil), list...)
	sort.SliceStable(sorted, func(i, j int) bool { return len(sorted[i]) > len(sorted[j]) })
	parts := make([]string, len(sorted))
	for i, token := range sorted {
		runes := []rune(token)
		prefix, suffix := "", ""
		if unicode.Is(unicode.Latin, runes[0]) {
			prefix = `(?<!` + latinLetter + `)`
		}
		if unicode.Is(unicode.Latin, runes[len(runes)-1]) {
			suffix = `(?!` + latinLetter + `)`
		}
		parts[i] = prefix + escapeRegexp(token) + suffix
	}
	return "(?:" + strings.Join(parts, "|") + ")"
}

// latinLetter is a Latin-script letter (the .NET syntax regexp2 follows has
// no script classes): ASCII letters and the Latin-1 and Extended-A/B ranges.
const latinLetter = `[A-Za-z\u00C0-\u00D6\u00D8-\u00F6\u00F8-\u024F]`

// amount is a number with the currency it names, or "" when it names none.
type amount struct {
	Value    float64
	Currency string
}

// readAmount reads an amount amountSource matched. Symbols resolve in the
// given languages first (¥ is yuan in Chinese).
func readAmount(text string, order []*requestWordList) (amount, bool) {
	buildAmount()
	m, err := amountReader.FindStringMatch(strings.TrimSpace(text))
	if err != nil || m == nil {
		return amount{}, false
	}
	g := m.Groups()
	value, ok := readNumber(g[2].String())
	if !ok {
		return amount{}, false
	}
	multiplier := 1.0
	if g[3].Length > 0 {
		if v, ok := lookupMultiplier(g[3].String(), order); ok {
			multiplier = v
		}
	}
	symbol := g[1].String()
	if symbol == "" {
		symbol = g[4].String()
	}
	out := amount{Value: math.Round(value*multiplier*100) / 100}
	if symbol != "" {
		out.Currency = lookupCurrency(symbol, order)
	}
	return out, true
}

func lookupMultiplier(key string, order []*requestWordList) (float64, bool) {
	_, all := requestLanguagesAll()
	for _, w := range append(append([]*requestWordList(nil), order...), all...) {
		if v, ok := w.Multipliers[key]; ok {
			return v, true
		}
	}
	return 0, false
}

func lookupCurrency(key string, order []*requestWordList) string {
	_, all := requestLanguagesAll()
	for _, w := range append(append([]*requestWordList(nil), order...), all...) {
		if v, ok := w.Currencies[key]; ok {
			return v
		}
	}
	return ""
}

var (
	numberShape = regexp.MustCompile(`^\d[\d.,\x{00a0}\x{202f}' ]*$`)
	nonDigit    = regexp.MustCompile(`[^\d]`)
)

// readNumber reads a number as people write it: 1,299.99 and 1.299,99 are
// the same, and a single separator before exactly three digits groups
// thousands (2.500 is 2500), while one before one or two digits is the
// decimal point (4.99).
func readNumber(text string) (float64, bool) {
	if !numberShape.MatchString(text) {
		return 0, false
	}
	seps := nonDigit.FindAllStringIndex(text, -1)
	if len(seps) == 0 {
		v, err := strconv.ParseFloat(text, 64)
		return v, err == nil
	}
	last := seps[len(seps)-1]
	lastSep := text[last[0]:last[1]]
	digitsAfter := len(text) - last[1]
	kinds := map[string]bool{}
	for _, s := range seps {
		kinds[text[s[0]:s[1]]] = true
	}
	first := text[seps[0][0]:seps[0][1]]
	decimal := (lastSep == "." || lastSep == ",") && (digitsAfter != 3 || (len(kinds) > 1 && len(seps) > 1 && lastSep != first))
	whole := text
	fraction := ""
	if decimal {
		whole, fraction = text[:last[0]], text[last[1]:]
	}
	whole = nonDigit.ReplaceAllString(whole, "")
	number := whole
	if fraction != "" {
		number += "." + fraction
	}
	v, err := strconv.ParseFloat(number, 64)
	return v, err == nil && !math.IsInf(v, 0)
}
