package connectors

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// event is a calendar event (RFC 5545 VEVENT).
type event struct {
	UID, Summary, Location, Description string
	Start, End                          time.Time
	AllDay, Transparent, Recurring      bool
	Calendar, Href, ETag                string
}

func (e event) view() map[string]any {
	layout := time.RFC3339
	start, end := e.Start.Local(), e.End.Local()
	if e.AllDay {
		layout = "2006-01-02"
		start, end = e.Start, e.End.AddDate(0, 0, -1)
	}
	out := map[string]any{"uid": e.UID, "title": e.Summary, "start": start.Format(layout), "end": end.Format(layout), "calendar": e.Calendar}
	if e.AllDay {
		out["all_day"] = true
	}
	if e.Location != "" {
		out["location"] = e.Location
	}
	if e.Description != "" {
		d := []rune(e.Description)
		if len(d) > 500 {
			d = append(d[:500], '…')
		}
		out["description"] = string(d)
	}
	if e.Recurring {
		out["repeats"] = true
	}
	if e.Transparent {
		out["free"] = true
	}
	return out
}

// unfold joins folded lines (RFC 5545 §3.1).
func unfold(data string) []string {
	data = strings.ReplaceAll(data, "\r\n", "\n")
	var out []string
	for _, line := range strings.Split(data, "\n") {
		if (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) && len(out) > 0 {
			out[len(out)-1] += line[1:]
			continue
		}
		out = append(out, line)
	}
	return out
}

// prop splits "DTSTART;TZID=Europe/Oslo:20261002T150000".
func prop(line string) (name string, params map[string]string, value string) {
	params = map[string]string{}
	head, value, ok := strings.Cut(line, ":")
	if !ok {
		return "", params, ""
	}
	parts := strings.Split(head, ";")
	name = strings.ToUpper(parts[0])
	for _, p := range parts[1:] {
		if k, v, ok := strings.Cut(p, "="); ok {
			params[strings.ToUpper(k)] = strings.Trim(v, `"`)
		}
	}
	return name, params, value
}

var icsUnescape = strings.NewReplacer(`\n`, "\n", `\N`, "\n", `\,`, ",", `\;`, ";", `\\`, `\`)

func icsTimeValue(value string, params map[string]string) (time.Time, bool) {
	if params["VALUE"] == "DATE" || len(value) == 8 {
		t, err := time.ParseInLocation("20060102", value, time.Local)
		return t, err == nil
	}
	if strings.HasSuffix(value, "Z") {
		t, err := time.Parse(icalUTC, value)
		return t, err == nil
	}
	loc := time.Local
	if tz := params["TZID"]; tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	t, err := time.ParseInLocation("20060102T150405", value, loc)
	return t, err == nil
}

var durationRe = regexp.MustCompile(`^([+-])?P(?:(\d+)W)?(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?)?$`)

func icsDuration(v string) time.Duration {
	m := durationRe.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return 0
	}
	n := func(s string) time.Duration { x, _ := strconv.Atoi(s); return time.Duration(x) }
	d := n(m[2])*7*24*time.Hour + n(m[3])*24*time.Hour + n(m[4])*time.Hour + n(m[5])*time.Minute + n(m[6])*time.Second
	if m[1] == "-" {
		d = -d
	}
	return d
}

// parseICS reads the events of a VCALENDAR.
func parseICS(data string) []event {
	var out []event
	var cur *event
	var hasEnd bool
	var dur time.Duration
	depth := 0
	for _, line := range unfold(data) {
		name, params, value := prop(strings.TrimRight(line, "\r"))
		switch {
		case name == "BEGIN" && strings.EqualFold(value, "VEVENT"):
			cur, hasEnd, dur, depth = &event{}, false, 0, 0
			continue
		case cur == nil:
			continue
		case name == "BEGIN":
			depth++ // an alarm inside the event
			continue
		case name == "END" && strings.EqualFold(value, "VEVENT"):
			if !hasEnd {
				switch {
				case dur > 0:
					cur.End = cur.Start.Add(dur)
				case cur.AllDay:
					cur.End = cur.Start.AddDate(0, 0, 1)
				default:
					cur.End = cur.Start
				}
			}
			out = append(out, *cur)
			cur = nil
			continue
		case name == "END":
			depth--
			continue
		case depth > 0:
			continue
		}
		switch name {
		case "UID":
			cur.UID = value
		case "SUMMARY":
			cur.Summary = icsUnescape.Replace(value)
		case "LOCATION":
			cur.Location = icsUnescape.Replace(value)
		case "DESCRIPTION":
			cur.Description = icsUnescape.Replace(value)
		case "DTSTART":
			cur.Start, _ = icsTimeValue(value, params)
			cur.AllDay = params["VALUE"] == "DATE" || len(value) == 8
		case "DTEND":
			cur.End, hasEnd = icsTimeValue(value, params)
		case "DURATION":
			dur = icsDuration(value)
		case "TRANSP":
			cur.Transparent = strings.EqualFold(value, "TRANSPARENT")
		case "RRULE":
			cur.Recurring = true
		}
	}
	return out
}

var icsEscape = strings.NewReplacer(`\`, `\\`, "\n", `\n`, ",", `\,`, ";", `\;`)

func icsText(v string) string { return icsEscape.Replace(strings.ReplaceAll(v, "\r\n", "\n")) }

func icsTime(name string, t time.Time, allDay bool) string {
	if allDay {
		return name + ";VALUE=DATE:" + t.Format("20060102")
	}
	return name + ":" + t.UTC().Format(icalUTC)
}

// fold breaks a line at 75 octets without splitting a character.
func fold(line string) string {
	if len(line) <= 75 {
		return line
	}
	var b strings.Builder
	n := 0
	for _, r := range line {
		size := len(string(r))
		if n+size > 75 {
			b.WriteString("\r\n ")
			n = 1
		}
		b.WriteRune(r)
		n += size
	}
	return b.String()
}

func joinICS(lines []string) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(fold(l))
		b.WriteString("\r\n")
	}
	return b.String()
}

// ics writes a new event as a VCALENDAR.
func (e event) ics(now time.Time) string {
	lines := []string{"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//Toskar//Calendar//EN", "BEGIN:VEVENT",
		"UID:" + e.UID, "DTSTAMP:" + now.UTC().Format(icalUTC), icsTime("DTSTART", e.Start, e.AllDay), icsTime("DTEND", e.End, e.AllDay),
		"SUMMARY:" + icsText(e.Summary)}
	if e.Location != "" {
		lines = append(lines, "LOCATION:"+icsText(e.Location))
	}
	if e.Description != "" {
		lines = append(lines, "DESCRIPTION:"+icsText(e.Description))
	}
	lines = append(lines, "END:VEVENT", "END:VCALENDAR")
	return joinICS(lines)
}

// rewriteEvent replaces properties of the event with uid, keeping the rest
// (attendees, alarms, and properties Yggdrasil does not know). An empty
// replacement removes the property.
func rewriteEvent(raw, uid string, changes map[string]string) string {
	var lines []string
	for _, l := range unfold(raw) {
		if l = strings.TrimRight(l, "\r"); l != "" {
			lines = append(lines, l)
		}
	}
	var out []string
	for i := 0; i < len(lines); i++ {
		name, _, value := prop(lines[i])
		if name != "BEGIN" || !strings.EqualFold(value, "VEVENT") {
			out = append(out, lines[i])
			continue
		}
		// The whole VEVENT, to its matching END.
		depth, j := 0, i+1
		for ; j < len(lines); j++ {
			n, _, v := prop(lines[j])
			if n == "BEGIN" {
				depth++
			} else if n == "END" {
				if depth == 0 && strings.EqualFold(v, "VEVENT") {
					break
				}
				depth--
			}
		}
		if j >= len(lines) {
			j = len(lines) - 1
		}
		block := lines[i : j+1]
		if blockUID(block) == uid {
			block = applyChanges(block, changes)
		}
		out = append(out, block...)
		i = j
	}
	return joinICS(out)
}

func blockUID(block []string) string {
	depth := 0
	for _, l := range block[1:] {
		n, _, v := prop(l)
		switch {
		case n == "BEGIN":
			depth++
		case n == "END":
			depth--
		case depth == 0 && n == "UID":
			return v
		}
	}
	return ""
}

// applyChanges changes a VEVENT's own properties, not those of its alarms.
func applyChanges(block []string, changes map[string]string) []string {
	applied := map[string]bool{}
	out := []string{block[0]}
	depth := 0
	for _, l := range block[1 : len(block)-1] {
		n, _, _ := prop(l)
		switch n {
		case "BEGIN":
			depth++
		case "END":
			depth--
		}
		if repl, ok := changes[n]; ok && depth == 0 && n != "UID" && n != "BEGIN" && n != "END" {
			if repl != "" && !applied[n] {
				out = append(out, repl)
				applied[n] = true
			}
			continue
		}
		out = append(out, l)
	}
	for _, key := range []string{"SUMMARY", "DTSTART", "DTEND", "LOCATION", "DESCRIPTION", "DTSTAMP"} {
		if v := changes[key]; v != "" && !applied[key] {
			out = append(out, v)
		}
	}
	return append(out, block[len(block)-1])
}
