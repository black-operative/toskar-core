package automations

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/dlclark/regexp2"
	"github.com/yeixio/toskar-core/internal/locale"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Reading an automation request, "every morning at 8, tell me if the price
// is below $500", into a schedule, a notification, a name, and the task to
// run (#204). The words come from i18n/requests (requestwords.go); this is a
// port of web/src/features/automations/parseRequest.ts, so the form,
// toskarctl, and chat read a request the same way. Text the person sees,
// such as the name and the notes, is in the language asked for.

// ParsedRequest is an automation read from a request.
type ParsedRequest struct {
	Name         string       `json:"name"`
	Prompt       string       `json:"prompt"`
	Schedule     Schedule     `json:"schedule"`
	Notification Notification `json:"notification"`
	// Notes say what was assumed, such as a time of day when none was given.
	Notes []string `json:"notes"`
}

// Errors reading a request, with codes the apps show in their language.
var (
	ErrRequestEmpty    = contracts.NewError("REQUEST_EMPTY", nil, errors.New("describe the automation"))
	ErrRequestTimeZone = contracts.NewError("REQUEST_TIME_ZONE", nil, errors.New("a time zone is required"))
	ErrRequestWhen     = contracts.NewError("REQUEST_NO_SCHEDULE", nil, errors.New("describe when it should run, for example \"every morning at 8:00 AM\""))
	ErrRequestBadTime  = contracts.NewError("REQUEST_BAD_TIME", nil, errors.New("that time is not valid"))
)

// ParseRequest reads a request written in lang (the App language) or in
// English. now and timeZone place a one-time run; lang is also the
// language of the name and the notes.
func ParseRequest(text string, now time.Time, timeZone, lang string) (ParsedRequest, error) {
	original := strings.TrimSpace(text)
	if original == "" {
		return ParsedRequest{}, ErrRequestEmpty
	}
	if timeZone == "" {
		return ParsedRequest{}, ErrRequestTimeZone
	}
	loc, err := time.LoadLocation(timeZone)
	if err != nil {
		return ParsedRequest{}, ErrRequestTimeZone
	}
	normalized := normalizeRequest(original)
	// The language's words first, then English's. The first that finds a
	// schedule is the language the request is written in.
	languages := requestLanguages(lang)
	for _, words := range languages {
		found, err := parseSchedule(withDigits(normalized, words), words, now, loc, timeZone, lang)
		if err != nil {
			return ParsedRequest{}, err
		}
		if found == nil {
			continue
		}
		order := []*requestWordList{words}
		for _, other := range languages {
			if other != words {
				order = append(order, other)
			}
		}
		// An amount without a currency is in the language's.
		notification := parseNotification(normalized, order, languages[0].Currency)
		return ParsedRequest{
			Name:         automationName(normalized, original, notification, order, lang),
			Prompt:       readableTask(original, notification, words),
			Schedule:     found.schedule,
			Notification: notification,
			Notes:        found.notes,
		}, nil
	}
	return ParsedRequest{}, ErrRequestWhen
}

type scheduleFound struct {
	schedule Schedule
	notes    []string
}

func parseSchedule(text string, w *requestWordList, now time.Time, loc *time.Location, zone, lang string) (*scheduleFound, error) {
	notes := []string{}
	if seconds := parseInterval(text, w); seconds > 0 {
		return &scheduleFound{schedule: Schedule{Kind: KindInterval, TimeZone: zone, EverySeconds: seconds}, notes: notes}, nil
	}
	if weekday, ok := parseWeekday(text, w); ok {
		clock, err := parseClock(text, w)
		if err != nil {
			return nil, err
		}
		hour, minute := 8, 0
		if clock != nil {
			hour, minute = clock.hour, clock.minute
		} else {
			notes = append(notes, locale.T(lang, "automations:parse.noTime8", nil))
		}
		return &scheduleFound{schedule: Schedule{Kind: KindWeekly, TimeZone: zone, Hour: hour, Minute: minute, Weekday: &weekday}, notes: notes}, nil
	}
	if daily := dailyPart(text, w); daily != "" {
		clock, err := parseClock(text, w)
		if err != nil {
			return nil, err
		}
		var hour, minute int
		if clock != nil {
			hour, minute = clock.hour, clock.minute
		} else {
			part := daily
			if part == "day" {
				part = daypartIn(text, w)
				if part == "" {
					part = "morning"
				}
			}
			var note string
			hour, minute, note = namedDaypart(part, lang)
			notes = append(notes, note)
		}
		return &scheduleFound{schedule: Schedule{Kind: KindDaily, TimeZone: zone, Hour: hour, Minute: minute}, notes: notes}, nil
	}
	if day := onceDay(text, w); day != "" {
		clock, err := parseClock(text, w)
		if err != nil {
			return nil, err
		}
		hour, minute := 9, 0
		if clock != nil {
			hour, minute = clock.hour, clock.minute
		} else {
			notes = append(notes, locale.T(lang, "automations:parse.noTime9", nil))
		}
		today := now.In(loc)
		date := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, loc)
		if day == "tomorrow" {
			date = date.AddDate(0, 0, 1)
		}
		at := time.Date(date.Year(), date.Month(), date.Day(), hour, minute, 0, 0, loc).UTC()
		return &scheduleFound{schedule: Schedule{Kind: KindOnce, TimeZone: zone, At: &at}, notes: notes}, nil
	}
	return nil, nil
}

// group is a named group's text, and whether it took part in the match.
func group(m *regexp2.Match, name string) (string, bool) {
	g := m.GroupByName(name)
	if g == nil || len(g.Captures) == 0 {
		return "", false
	}
	return g.String(), true
}

func parseInterval(text string, w *requestWordList) int {
	if hasPhrase(text, w.Interval.HalfHour, w.Spaced) {
		return 30 * 60
	}
	if hasPhrase(text, w.Interval.Hourly, w.Spaced) {
		return 60 * 60
	}
	numberWords := make([]string, 0, len(w.Numbers))
	for k := range w.Numbers {
		numberWords = append(numberWords, k)
	}
	amount := `(?<q>\d+|` + phraseSource(numberWords, w.Spaced, true, true) + `)`
	u := w.Interval.Units
	unit := `(?:(?<second>` + phraseSource(u.Second, w.Spaced, true, true) + `)|(?<minute>` + phraseSource(u.Minute, w.Spaced, true, true) + `)|(?<hour>` + phraseSource(u.Hour, w.Spaced, true, true) + `)|(?<day>` + phraseSource(u.Day, w.Spaced, true, true) + `))`
	forms := []*regexp2.Regexp{
		w.pattern("intervalBefore", func() string {
			if len(w.Interval.Before) == 0 {
				return ""
			}
			return phraseSource(w.Interval.Before, w.Spaced, true, true) + `(?:` + w.gap() + amount + `)?` + w.gap() + unit
		}),
		w.pattern("intervalAfter", func() string {
			if len(w.Interval.After) == 0 {
				return ""
			}
			return `(?:(?<![\d])` + amount + w.gap() + `)?` + unit + `\s*` + phraseSource(w.Interval.After, w.Spaced, false, true)
		}),
	}
	for _, form := range forms {
		if form == nil {
			continue
		}
		m, _ := form.FindStringMatch(text)
		for ; m != nil; m, _ = form.FindNextMatch(m) {
			count := 1
			if q, ok := group(m, "q"); ok && q != "" {
				n, ok := quantity(q, w)
				if !ok {
					continue
				}
				count = n
			}
			if count <= 0 {
				continue
			}
			seconds := 86400
			if _, ok := group(m, "second"); ok {
				seconds = 1
			} else if _, ok := group(m, "minute"); ok {
				seconds = 60
			} else if _, ok := group(m, "hour"); ok {
				seconds = 3600
			}
			// "Every day" is a daily schedule, at a time of day.
			if seconds == 86400 && count == 1 {
				continue
			}
			return count * seconds
		}
	}
	return 0
}

var allDigits = regexp.MustCompile(`^\d+$`)

func quantity(token string, w *requestWordList) (int, bool) {
	if allDigits.MatchString(token) {
		n, err := strconv.Atoi(token)
		return n, err == nil
	}
	if v, ok := w.Numbers[token]; ok {
		return int(v), true
	}
	joined := strings.NewReplacer(" ", "", "-", "").Replace(token)
	if v, ok := w.Numbers[joined]; ok {
		return int(v), true
	}
	return 0, false
}

func parseWeekday(text string, w *requestWordList) (int, bool) {
	names := func(list [][]string) string {
		parts := make([]string, len(list))
		for i, day := range list {
			parts[i] = `(?<d` + strconv.Itoa(i) + `>` + phraseSource(day, w.Spaced, true, true) + `)`
		}
		return `(?:` + strings.Join(parts, "|") + `)`
	}
	wk := w.Weekly
	forms := []*regexp2.Regexp{
		w.pattern("weekBefore", func() string {
			if len(wk.Before) == 0 || len(wk.Days) == 0 {
				return ""
			}
			return phraseSource(wk.Before, w.Spaced, true, true) + w.gap() + names(wk.Days)
		}),
		w.pattern("weekAfter", func() string {
			if len(wk.After) == 0 || len(wk.Days) == 0 {
				return ""
			}
			return names(wk.Days) + `\s*` + phraseSource(wk.After, w.Spaced, false, true)
		}),
		w.pattern("weekAlone", func() string {
			if len(wk.Alone) == 0 {
				return ""
			}
			return names(wk.Alone)
		}),
	}
	for _, form := range forms {
		if form == nil {
			continue
		}
		m, _ := form.FindStringMatch(text)
		if m == nil {
			continue
		}
		for i := 0; i < 7; i++ {
			if _, ok := group(m, "d"+strconv.Itoa(i)); ok {
				return i, true
			}
		}
	}
	return 0, false
}

var dayparts = []string{"morning", "afternoon", "evening", "night"}

// dailyPart is the time of day a daily phrase names, "day" when it names
// none, or "" when the request isn't daily.
func dailyPart(text string, w *requestWordList) string {
	for _, part := range dayparts {
		if hasPhrase(text, w.Daily[part], w.Spaced) {
			return part
		}
	}
	if hasPhrase(text, w.Daily["day"], w.Spaced) {
		return "day"
	}
	return ""
}

// daypartIn is a time of day the request mentions, checked from evening to
// morning.
func daypartIn(text string, w *requestWordList) string {
	for _, part := range []string{"evening", "night", "afternoon", "morning"} {
		if hasPhrase(text, w.Dayparts[part], w.Spaced) {
			return part
		}
	}
	return ""
}

func namedDaypart(part, lang string) (int, int, string) {
	switch part {
	case "evening":
		return 18, 0, locale.T(lang, "automations:parse.evening", nil)
	case "night":
		return 21, 0, locale.T(lang, "automations:parse.night", nil)
	case "afternoon":
		return 15, 0, locale.T(lang, "automations:parse.afternoon", nil)
	}
	return 8, 0, locale.T(lang, "automations:parse.morning", nil)
}

func onceDay(text string, w *requestWordList) string {
	o := w.Once
	masked := text
	if len(o.NotTomorrow) > 0 {
		re := w.pattern("notTomorrow", func() string { return phraseSource(o.NotTomorrow, w.Spaced, true, true) })
		if out, err := re.Replace(text, " ", -1, -1); err == nil {
			masked = out
		}
	}
	today := hasPhrase(text, o.Today, w.Spaced)
	tomorrow := hasPhrase(masked, o.Tomorrow, w.Spaced)
	if !today && !tomorrow && !hasPhrase(text, o.Once, w.Spaced) {
		return ""
	}
	if today && !tomorrow {
		return "today"
	}
	return "tomorrow"
}

// suffix is a suffix that follows a number: "18h30", "8時", "6시에".
func suffix(list []string, w *requestWordList) string {
	source := phraseSource(list, w.Spaced, false, false)
	if w.Spaced {
		return source + `(?!\p{L})`
	}
	return source + `(?![間间간])`
}

// timeSource matches 18:30, 18 h 30, 18h, 8 Uhr, 8時30分, 6시 반, 6点半.
func timeSource(w *requestWordList) string {
	hour := suffix(w.Clock.Hour, w)
	minute := suffix(w.Clock.Minute, w)
	half := suffix(w.Clock.Half, w)
	return `(?<![\d.,:])(?<h>\d{1,2})(?:(?::(?<m>\d{2}))(?:\s*` + hour + `)?|\s*` + hour + `(?:\s*(?<m2>\d{1,2})\s*` + minute + `|\s*(?<m3>\d{2})(?!\d)|\s*(?<half>` + half + `))?)`
}

type clockTime struct{ hour, minute int }

func parseClock(text string, w *requestWordList) (*clockTime, error) {
	c := w.Clock
	twelveHour := len(c.AM) > 0 && len(c.PM) > 0
	meridiem := `(?:(?<am>` + phraseSource(c.AM, w.Spaced, true, true) + `)|(?<pm>` + phraseSource(c.PM, w.Spaced, true, true) + `))`
	forms := []*regexp2.Regexp{
		w.pattern("clockMeridiemFirst", func() string {
			if !twelveHour || !c.MeridiemFirst {
				return ""
			}
			return meridiem + `\s*` + timeSource(w)
		}),
		w.pattern("clockMeridiemLast", func() string {
			if !twelveHour || c.MeridiemFirst {
				return ""
			}
			return `(?<![\d.,:])(?<h>\d{1,2})(?::(?<m>\d{2}))?\s*(?:` + suffix(c.Hour, w) + `\s*)?` + meridiem
		}),
		w.pattern("clockTime", func() string { return timeSource(w) }),
		w.pattern("clockAt", func() string {
			if len(c.At) == 0 {
				return ""
			}
			return phraseSource(c.At, w.Spaced, true, true) + `\s*(?<h>\d{1,2})(?:\s*(?<half>` + suffix(c.Half, w) + `))?(?!\d|:|[.,]\d)`
		}),
	}
	for _, form := range forms {
		if form == nil {
			continue
		}
		m, _ := form.FindStringMatch(text)
		if m == nil {
			continue
		}
		hourText, _ := group(m, "h")
		hour, _ := strconv.Atoi(hourText)
		minute := 0
		if v, ok := group(m, "m"); ok {
			minute, _ = strconv.Atoi(v)
		} else if v, ok := group(m, "m2"); ok {
			minute, _ = strconv.Atoi(v)
		} else if v, ok := group(m, "m3"); ok {
			minute, _ = strconv.Atoi(v)
		} else if _, ok := group(m, "half"); ok {
			minute = 30
		}
		_, am := group(m, "am")
		_, pm := group(m, "pm")
		if am || pm {
			return clockFrom(hour, minute, pm)
		}
		if hour > 23 || minute > 59 {
			return nil, nil
		}
		return &clockTime{hour: afterDaypart(hour, text, w), minute: minute}, nil
	}
	return nil, nil
}

// afterDaypart moves a morning hour to the afternoon when a time of day
// says so: "8 in the evening" is 20:00.
func afterDaypart(hour int, text string, w *requestWordList) int {
	if hour < 1 || hour > 11 {
		return hour
	}
	switch daypartIn(text, w) {
	case "afternoon", "evening":
		return hour + 12
	case "night":
		if hour >= 6 {
			return hour + 12
		}
	}
	return hour
}

func clockFrom(hour, minute int, pm bool) (*clockTime, error) {
	if minute > 59 || hour > 12 || hour < 1 {
		return nil, ErrRequestBadTime
	}
	next := hour % 12
	if pm {
		next += 12
	}
	return &clockTime{hour: next, minute: minute}, nil
}

func parseNotification(text string, order []*requestWordList, currency string) Notification {
	says := func(list func(*requestWordList) []string) bool {
		for _, w := range order {
			if hasPhrase(text, list(w), w.Spaced) {
				return true
			}
		}
		return false
	}
	if says(func(w *requestWordList) []string { return w.Notify.None }) {
		return Notification{Mode: NotifyNone}
	}
	if says(func(w *requestWordList) []string { return w.Notify.Change }) {
		return Notification{Mode: NotifyOnChange}
	}
	if says(func(w *requestWordList) []string { return w.Notify.Significant }) {
		return Notification{Mode: NotifyOnCondition, Condition: &Condition{Kind: ConditionSignificant}}
	}
	for _, w := range order {
		op, text, ok := findThreshold(text, w)
		if !ok {
			continue
		}
		amt, ok := readAmount(text, order)
		if !ok {
			continue
		}
		if amt.Currency == "" {
			amt.Currency = currency
		}
		return Notification{Mode: NotifyOnCondition, Condition: &Condition{Kind: ConditionThreshold, Op: op, Value: amt.Value, Currency: amt.Currency}}
	}
	if says(func(w *requestWordList) []string { return w.Notify.Available }) {
		return Notification{Mode: NotifyOnCondition, Condition: &Condition{Kind: ConditionAvailable}}
	}
	return Notification{Mode: NotifyAlways}
}

func findThreshold(text string, w *requestWordList) (op, amountText string, ok bool) {
	n := w.Notify
	amountGroup := `(?<amount>` + amountSource() + `)`
	forms := []*regexp2.Regexp{
		w.pattern("priceBefore", func() string {
			if len(n.Below) == 0 && len(n.Above) == 0 {
				return ""
			}
			return `(?:(?<below>` + phraseSource(n.Below, w.Spaced, true, true) + `)|(?<above>` + phraseSource(n.Above, w.Spaced, true, true) + `))\s*` + amountGroup
		}),
		w.pattern("priceAfter", func() string {
			if len(n.BelowAfter) == 0 && len(n.AboveAfter) == 0 {
				return ""
			}
			return `(?<![\d.,])` + amountGroup + `\s*(?:(?<below>` + phraseSource(n.BelowAfter, w.Spaced, false, true) + `)|(?<above>` + phraseSource(n.AboveAfter, w.Spaced, false, true) + `))`
		}),
	}
	for _, form := range forms {
		if form == nil {
			continue
		}
		m, _ := form.FindStringMatch(text)
		if m == nil {
			continue
		}
		if a, found := group(m, "amount"); found && a != "" {
			if _, above := group(m, "above"); above {
				return OpAbove, a, true
			}
			return OpBelow, a, true
		}
	}
	return "", "", false
}

var (
	trailingStops   = regexp.MustCompile(`[.。]+$`)
	trailingJoiners = regexp.MustCompile(`[\s,，、:：]+$`)
	sentenceEnd     = regexp.MustCompile(`[.!?。！？]$`)
	clauseBreak     = regexp2.MustCompile(`[,，、：]|:(?=\s)`, regexp2.Unicode)
	clauseStop      = regexp.MustCompile(`[.!?。！？]`)
)

func readableTask(original string, notification Notification, w *requestWordList) string {
	text := trailingStops.ReplaceAllString(strings.TrimSpace(original), "")
	text = withoutScheduleClause(text, w)
	for _, ending := range w.Task.DropAtEnd {
		re := w.pattern("drop:"+ending, func() string {
			return `(?i)` + phraseSource([]string{ending}, w.Spaced, true, true) + `$`
		})
		if out, err := re.Replace(text, "", -1, -1); err == nil {
			text = out
		}
	}
	text = strings.Join(strings.Fields(trailingJoiners.ReplaceAllString(text, "")), " ")
	if text == "" {
		text = strings.TrimSpace(original)
	}
	stop := w.Task.FullStop
	if notification.Condition != nil && notification.Condition.Kind == ConditionThreshold && !hasPhrase(normalizeRequest(text), w.Task.Price, w.Spaced) {
		text += stop + w.Task.ReportPrice
	}
	sentence := upperFirst(text)
	if sentenceEnd.MatchString(sentence) {
		return sentence
	}
	return sentence + strings.TrimSpace(stop)
}

// withoutScheduleClause drops a leading clause that only says when:
// "Every morning at 8:00 AM, check this product" runs "Check this product".
func withoutScheduleClause(text string, w *requestWordList) string {
	// A colon ends a clause only before a space, so 8:00 stays a time.
	m, _ := clauseBreak.FindStringMatch(text)
	if m == nil {
		return text
	}
	// Positions are in runes, as in JavaScript (near enough: UTF-16 units).
	runes := []rune(text)
	comma := m.Index
	if comma <= 0 || comma > 80 {
		return text
	}
	clause := string(runes[:comma])
	rest := strings.TrimSpace(string(runes[comma+m.Length:]))
	if rest == "" || clauseStop.MatchString(clause) {
		return text
	}
	normalized := withDigits(normalizeRequest(clause), w)
	found, err := parseSchedule(normalized, w, time.Unix(0, 0), time.UTC, "UTC", locale.Source)
	if err != nil || found == nil {
		return text
	}
	if onlySchedule(normalized, w) {
		return rest
	}
	return text
}

var wordSplit = regexp.MustCompile(`[\s\p{P}]+`)

// onlySchedule reports a clause that says nothing but when: "Täglich um 7:30
// Uhr prüfen" also says what to do, so it stays. Short words such as "de
// la", に, and 에 may remain.
func onlySchedule(clause string, w *requestWordList) bool {
	re := w.pattern("scheduleWords", func() string {
		var lists []string
		add := func(l ...[]string) {
			for _, x := range l {
				lists = append(lists, x...)
			}
		}
		add(w.Interval.Before, w.Interval.After, w.Interval.Units.Second, w.Interval.Units.Minute, w.Interval.Units.Hour, w.Interval.Units.Day, w.Interval.Hourly, w.Interval.HalfHour)
		for k := range w.Numbers {
			lists = append(lists, k)
		}
		for _, v := range w.Daily {
			add(v)
		}
		for _, v := range w.Dayparts {
			add(v)
		}
		add(w.Weekly.Before, w.Weekly.After)
		add(w.Weekly.Days...)
		add(w.Weekly.Alone...)
		add(w.Once.Once, w.Once.Today, w.Once.Tomorrow, w.Clock.At, w.Clock.Hour, w.Clock.Minute, w.Clock.Half, w.Clock.AM, w.Clock.PM)
		return `\d+(?:[:.]\d+)?|` + phraseSource(lists, w.Spaced, true, true)
	})
	rest := clause
	if out, err := re.Replace(clause, " ", -1, -1); err == nil {
		rest = out
	}
	for _, word := range wordSplit.Split(rest, -1) {
		if word != "" && utf8.RuneCountInString(word) > 3 {
			return false
		}
	}
	return true
}

func automationName(text, original string, n Notification, order []*requestWordList, lang string) string {
	says := func(list func(*requestWordList) []string) bool {
		for _, w := range order {
			if hasPhrase(text, list(w), w.Spaced) {
				return true
			}
		}
		return false
	}
	c := n.Condition
	if n.Mode == NotifyOnCondition && c != nil && c.Kind == ConditionThreshold {
		key := "automations:names.priceBelow"
		if c.Op == OpAbove {
			key = "automations:names.priceAbove"
		}
		return locale.T(lang, key, map[string]any{"amount": formatPrice(c.Value, c.Currency, lang)})
	}
	if c != nil && c.Kind == ConditionAvailable {
		if says(func(w *requestWordList) []string { return w.Names.Stock }) {
			return locale.T(lang, "automations:names.stock", nil)
		}
		return locale.T(lang, "automations:names.availability", nil)
	}
	if c != nil && c.Kind == ConditionSignificant {
		return locale.T(lang, "automations:names.significance", nil)
	}
	if says(func(w *requestWordList) []string { return w.Names.Release }) {
		return locale.T(lang, "automations:names.release", nil)
	}
	if says(func(w *requestWordList) []string { return w.Names.Research }) {
		return locale.T(lang, "automations:names.research", nil)
	}
	cleaned := strings.Join(strings.Fields(original), " ")
	if cleaned == "" {
		return locale.T(lang, "automations:names.scheduled", nil)
	}
	if runes := []rune(cleaned); len(runes) > 48 {
		cleaned = strings.TrimSpace(string(runes[:48])) + "…"
	}
	return upperFirst(cleaned)
}

func upperFirst(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}
