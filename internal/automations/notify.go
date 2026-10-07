package automations

import (
	"context"
	"errors"
	"github.com/yeixio/toskar-core/internal/locale"
	"github.com/yeixio/toskar-core/internal/structured"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ErrNotifyDisabled means the user turned notifications off. The run still succeeded.
var ErrNotifyDisabled = contracts.NewError("NOTIFICATIONS_DISABLED", nil, errors.New("notifications are disabled"))

// Notice is a notification about an automation.
type Notice struct {
	// Title and Body are in English, for the desktop sender and logs.
	Title string
	Body  string
	// Message is the title and body as catalog keys and literal text (the
	// automation's name, what the model wrote), so each place that shows the
	// notice writes it in its own language (multilingual spec §22).
	Message *locale.Message
	// AutomationID links the notice to its automation.
	AutomationID string
	// Failure is true when the notice reports a failed run.
	Failure bool
}

// Notifier delivers a notice. The OS sender and tests both implement it.
type Notifier interface {
	Notify(ctx context.Context, notice Notice) error
}

// Decision is the result of comparing a stored run with its notification rule.
type Decision struct {
	Notify bool
	Notice Notice
	// Reason says why, in English, for logs and the preview.
	Reason string
	// Detail is the same as a key the apps show under the run
	// (automations:notice.<detail>), with Values for its placeholders, so
	// they explain what the server decided instead of deciding again
	// (#204). ConditionNotMet marks a condition that didn't hold.
	Detail          string
	Values          map[string]any
	ConditionNotMet bool
}

type parsedSignal struct {
	Price       *float64
	Available   *bool
	Significant *bool
	// prose is the result with the machine-readable object removed.
	prose string
}

// Decide reports whether a successful result should notify.
// previous is the prior successful result. It is nil when this is the first success.
// previousNotified is true when that earlier success already produced a notice.
// A threshold notifies whenever the comparison holds. Availability notifies when the item
// is in stock and the previous notice did not already say so. Change mode uses the first
// success as a baseline and notifies when later text differs.
func Decide(n Notification, result string, previous *string, previousNotified bool) Decision {
	switch n.Mode {
	case NotifyNone:
		return Decision{Reason: "notifications are off for this automation", Detail: "storesResult"}
	case NotifyOnFailure:
		return Decision{Reason: "notifications are only for failures"}
	case NotifyAlways:
		return Decision{Notify: true, Notice: noticeFor(n, result), Reason: "always"}
	case NotifyOnChange:
		if previous == nil {
			return Decision{Reason: "waiting for a baseline result", Detail: "firstSaved"}
		}
		if normalizeResult(result) == normalizeResult(*previous) {
			return Decision{Reason: "result is unchanged", Detail: "unchanged"}
		}
		return Decision{Notify: true, Notice: noticeFor(n, result), Reason: "result changed", Detail: "changed"}
	case NotifyOnCondition:
		return decideCondition(n, result, previous, previousNotified)
	default:
		return Decision{Reason: "unknown notification mode"}
	}
}

func decideCondition(n Notification, result string, previous *string, previousNotified bool) Decision {
	if n.Condition == nil {
		return Decision{Reason: "notification condition is missing"}
	}
	signal, ok := parseSignal(result)
	switch n.Condition.Kind {
	case ConditionThreshold:
		currency := currencyOf(n)
		if !ok || signal.Price == nil {
			return Decision{Reason: "result did not include a price", Detail: "noPrice"}
		}
		price := *signal.Price
		values := map[string]any{"price": price, "amount": n.Condition.Value, "currency": currency}
		above := n.Condition.Op == OpAbove
		matched := above && price > n.Condition.Value || n.Condition.Op == OpBelow && price < n.Condition.Value
		if !matched {
			detail := "notBelow"
			if above {
				detail = "notAbove"
			}
			return Decision{Reason: "price is not " + n.Condition.Op + " the threshold", Detail: detail, Values: values, ConditionNotMet: true}
		}
		detail := "priceBelow"
		if above {
			detail = "priceAbove"
		}
		return Decision{Notify: true, Notice: noticeFor(n, result), Reason: "price is " + n.Condition.Op + " the threshold", Detail: detail, Values: values}
	case ConditionAvailable:
		available, known := itemAvailable(result)
		if !known || !available {
			return Decision{Reason: "item is not available", Detail: "notAvailable", ConditionNotMet: true}
		}
		if previousNotified && previouslyAvailable(previous) {
			return Decision{Reason: "item was already available", Detail: "alreadyAvailable"}
		}
		return Decision{Notify: true, Notice: noticeFor(n, result), Reason: "item is in stock", Detail: "inStock"}
	case ConditionSignificant:
		if !ok || signal.Significant == nil || !*signal.Significant {
			return Decision{Reason: "result is not significant", Detail: "notSignificant", ConditionNotMet: true}
		}
		return Decision{Notify: true, Notice: noticeFor(n, result), Reason: "result is significant", Detail: "significant"}
	default:
		return Decision{Reason: "unknown notification condition"}
	}
}

func previouslyAvailable(previous *string) bool {
	if previous == nil {
		return false
	}
	available, known := itemAvailable(*previous)
	return known && available
}

// itemAvailable reads an availability flag from the result. A JSON available
// field wins. Otherwise a plain statement that the item is in stock counts.
func itemAvailable(result string) (available bool, known bool) {
	signal, ok := parseSignal(result)
	if ok && signal.Available != nil {
		return *signal.Available, true
	}
	return inferAvailable(result)
}

func inferAvailable(text string) (available bool, known bool) {
	positive, negative := false, false
	for _, sentence := range strings.FieldsFunc(text, func(r rune) bool {
		return r == '.' || r == '!' || r == '?' || r == '\n'
	}) {
		line := strings.ToLower(strings.TrimSpace(sentence))
		if line == "" {
			continue
		}
		if availabilityDenied(line) {
			negative = true
			continue
		}
		if availabilityStated(line) {
			positive = true
		}
	}
	if positive && !negative {
		return true, true
	}
	if negative && !positive {
		return false, true
	}
	return false, false
}

func availabilityDenied(line string) bool {
	phrases := []string{
		"out of stock",
		"not in stock",
		"isn't in stock",
		"is not in stock",
		"sold out",
		"unavailable",
		"not available",
		"isn't available",
		"is not available",
		"no longer available",
	}
	for _, phrase := range phrases {
		if strings.Contains(line, phrase) {
			return true
		}
	}
	return false
}

func availabilityStated(line string) bool {
	if strings.Contains(line, "if ") || strings.Contains(line, "whether ") || strings.Contains(line, "unable") || strings.Contains(line, "cannot") || strings.Contains(line, "could not") || strings.Contains(line, "can't") {
		return false
	}
	return strings.Contains(line, "in stock") || strings.Contains(line, "back in stock") || strings.Contains(line, "now available") || strings.Contains(line, "is available") || strings.Contains(line, "are available")
}

func noticeFor(n Notification, result string) Notice {
	body := []locale.Text{locale.Key("notifications:notices.automationFinished", nil)}
	signal, ok := parseSignal(result)
	if ok && strings.TrimSpace(signal.prose) != "" {
		body = []locale.Text{locale.Literal(oneLine(signal.prose, 180))}
	} else if prose := strings.TrimSpace(result); prose != "" && !ok {
		body = []locale.Text{locale.Literal(oneLine(prose, 180))}
	} else if ok {
		body = signal.sentence(currencyOf(n))
	}
	return withMessage(Notice{}, locale.Message{Title: locale.Key("notifications:notices.automationUnnamed", nil), Body: body})
}

// changeNotice says what changed, as the model put it.
func changeNotice(what string) Notice {
	return withMessage(Notice{}, locale.Message{
		Title: locale.Key("notifications:notices.automationUnnamed", nil),
		Body:  []locale.Text{locale.Literal(oneLine(what, 180))},
	})
}

// withMessage is notice with message, and its title and body in English.
func withMessage(notice Notice, m locale.Message) Notice {
	notice.Message = &m
	notice.Title, notice.Body = m.Render(locale.Source)
	return notice
}

// noticeTitle replaces the generic title once the caller knows the automation name.
func noticeTitle(name string, notice Notice) Notice {
	m := locale.Message{Title: locale.Key("notifications:notices.automationUnnamed", nil)}
	if notice.Message != nil {
		m = *notice.Message
	} else if notice.Title != "" || notice.Body != "" {
		// A notice written as text keeps its text.
		m = locale.Message{Title: locale.Literal(oneLine(notice.Title, 80))}
		if notice.Body != "" {
			m.Body = []locale.Text{locale.Literal(notice.Body)}
		}
	}
	if name = strings.TrimSpace(name); name != "" {
		m.Title = locale.Literal(oneLine(name, 80))
	} else if m.Title.Key == "" && m.Title.Text == "" {
		m.Title = locale.Key("notifications:notices.automationUnnamed", nil)
	}
	if len(m.Body) == 0 {
		m.Body = []locale.Text{locale.Key("notifications:notices.automationFinished", nil)}
	}
	return withMessage(notice, m)
}

// currencyOf is the currency a threshold's price is in, or "" when the
// notification has no threshold.
func currencyOf(n Notification) string {
	if n.Condition == nil || n.Condition.Kind != ConditionThreshold {
		return ""
	}
	if n.Condition.Currency == "" {
		return "USD"
	}
	return n.Condition.Currency
}

func (s parsedSignal) sentence(currency string) []locale.Text {
	var parts []locale.Text
	if s.Price != nil {
		price := strconv.FormatFloat(*s.Price, 'f', -1, 64)
		if currency != "" {
			price += " " + currency
		}
		parts = append(parts, locale.Key("notifications:notices.automationPrice", map[string]any{"price": price}))
	}
	if s.Available != nil {
		if *s.Available {
			parts = append(parts, locale.Key("notifications:notices.automationAvailable", nil))
		} else {
			parts = append(parts, locale.Key("notifications:notices.automationUnavailable", nil))
		}
	}
	if s.Significant != nil && *s.Significant {
		parts = append(parts, locale.Key("notifications:notices.automationSignificant", nil))
	}
	if len(parts) == 0 {
		return []locale.Text{locale.Key("notifications:notices.automationFinished", nil)}
	}
	return parts
}

// signalSchema is every field a result's JSON may carry. None is required
// here; ConditionSchema says what a condition needs.
var signalSchema = &structured.Schema{Type: "object", Properties: map[string]*structured.Schema{
	"price": {Type: "number"}, "available": {Type: "boolean"}, "significant": {Type: "boolean"},
}}

// ConditionSchema is the JSON a condition needs at the end of a result
// (spec §27), or nil when the text alone is enough.
func ConditionSchema(n Notification) *structured.Schema {
	if n.Mode != NotifyOnCondition || n.Condition == nil {
		return nil
	}
	switch n.Condition.Kind {
	case ConditionThreshold:
		return structured.Object(map[string]string{"price": "number"})
	case ConditionAvailable:
		// A flag, not English phrases, so it works in every language (#204).
		return structured.Object(map[string]string{"available": "boolean"})
	case ConditionSignificant:
		return structured.Object(map[string]string{"significant": "boolean"})
	}
	return nil
}

// parseSignal reads the machine-readable part of a result, with safe
// repairs such as "$1,299" for a price (spec §27).
func parseSignal(result string) (parsedSignal, bool) {
	f, ok := structured.Extract(result)
	if !ok {
		return parsedSignal{}, false
	}
	obj, isObj := structured.Coerce(f.Value, signalSchema).(map[string]any)
	if !isObj {
		return parsedSignal{}, false
	}
	var found parsedSignal
	if v, ok := obj["price"].(float64); ok {
		found.Price = &v
	}
	if v, ok := obj["available"].(bool); ok {
		found.Available = &v
	}
	if v, ok := obj["significant"].(bool); ok {
		found.Significant = &v
	}
	if found.Price == nil && found.Available == nil && found.Significant == nil {
		return parsedSignal{}, false
	}
	found.prose = structured.Prose(result, f)
	return found, true
}

func normalizeResult(s string) string {
	return strings.TrimSpace(s)
}

func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return ""
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	if max < 4 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}
