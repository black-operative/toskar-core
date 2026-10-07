package automations

import (
	"context"
	"strings"
)

// "Notify on change" compares what changed, not the model's wording (#204).
// A model words the same facts differently on every run, so comparing text
// notified nearly every time. A run is compared with the last successful
// one by, in order:
//
//  1. its structured values (price, availability, significance), when both
//     results carry them;
//  2. what its tools read (SourceHash), which is the same when the pages
//     and results it looked at didn't change;
//  3. the model judging whether anything meaningful changed (Change), which
//     the executor asks while the run's model is still loaded;
//  4. and, when none of those can tell, the text itself.

// Previous is the last successful run, which a new one is compared with.
type Previous struct {
	Text       string
	Notified   bool
	SourceHash string
}

// Change is the model's judgment of whether a result differs in a way the
// person would care about, and what changed, in the result's language.
type Change struct {
	Changed bool   `json:"changed"`
	What    string `json:"what"`
}

type previousKey struct{}

// WithPrevious gives the executor the run its result is compared with.
func WithPrevious(ctx context.Context, p Previous) context.Context {
	return context.WithValue(ctx, previousKey{}, p)
}

// PreviousFrom is the run set with WithPrevious.
func PreviousFrom(ctx context.Context) (Previous, bool) {
	p, ok := ctx.Value(previousKey{}).(Previous)
	return p, ok
}

// compareChange settles whether a result changed without asking a model:
// settled is false when only a judgment can tell.
func compareChange(prev Previous, cur Execution) (changed, settled bool, reason string) {
	before, okBefore := parseSignal(prev.Text)
	after, okAfter := parseSignal(cur.Text)
	if okBefore && okAfter {
		if sameSignal(before, after) {
			return false, true, "the values are unchanged"
		}
		return true, true, "the values changed"
	}
	if prev.SourceHash != "" && prev.SourceHash == cur.SourceHash {
		return false, true, "what it read is unchanged"
	}
	if strings.TrimSpace(prev.Text) == strings.TrimSpace(cur.Text) {
		return false, true, "result is unchanged"
	}
	return false, false, ""
}

// NeedsJudgment reports a change-mode result only a model can compare with
// the previous one.
func NeedsJudgment(n Notification, prev Previous, cur Execution) bool {
	if n.Mode != NotifyOnChange {
		return false
	}
	_, settled, _ := compareChange(prev, cur)
	return !settled
}

func sameSignal(a, b parsedSignal) bool {
	return sameFloat(a.Price, b.Price) && sameBool(a.Available, b.Available) && sameBool(a.Significant, b.Significant)
}

func sameFloat(a, b *float64) bool {
	return (a == nil) == (b == nil) && (a == nil || *a == *b)
}

func sameBool(a, b *bool) bool {
	return (a == nil) == (b == nil) && (a == nil || *a == *b)
}

// DecideRun is Decide for a finished run, with what change mode compares:
// the run's values, sources, and the model's judgment. prev is nil for the
// first success.
func DecideRun(n Notification, cur Execution, prev *Previous) Decision {
	if n.Mode != NotifyOnChange {
		var text *string
		notified := false
		if prev != nil {
			text, notified = &prev.Text, prev.Notified
		}
		return Decide(n, cur.Text, text, notified)
	}
	if prev == nil {
		return Decision{Reason: "waiting for a baseline result", Detail: "firstSaved"}
	}
	changed, settled, reason := compareChange(*prev, cur)
	if !settled && cur.Change != nil {
		changed, settled = cur.Change.Changed, true
		reason = "the model found no meaningful change"
		if changed {
			reason = "the model found a meaningful change"
		}
	}
	if !settled {
		// Nothing else could tell: the text differs.
		changed, reason = true, "result changed"
	}
	if !changed {
		return Decision{Reason: reason, Detail: "unchanged"}
	}
	notice := noticeFor(n, cur.Text)
	if cur.Change != nil && cur.Change.Changed && strings.TrimSpace(cur.Change.What) != "" {
		notice = changeNotice(cur.Change.What)
	}
	return Decision{Notify: true, Notice: notice, Reason: reason, Detail: "changed"}
}
