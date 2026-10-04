package app

import (
	"context"
	"time"

	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/locale"
	modelhealth "github.com/yeixio/toskar-core/internal/models/health"
	"github.com/yeixio/toskar-core/internal/share"
)

// EventWorkWaiting reports work queued behind higher-priority work (§60).
const EventWorkWaiting = "work.waiting"

// enterWork admits work to this computer by priority (spec §60). Work that
// has to wait publishes why. Without a gate, everything runs at once.
func (a *App) enterWork(ctx context.Context, class share.Class, label string, waiting func(reason string)) (*share.Work, error) {
	if a.Share == nil {
		return nil, nil
	}
	return a.Share.Enter(ctx, class, label, func(reason string) {
		if a.Logger != nil {
			a.Logger.Info("work waiting", "class", class.String(), "label", label, "reason", reason)
		}
		if a.Bus != nil {
			a.Bus.Publish(events.New(EventWorkWaiting, map[string]any{"class": class.String(), "label": label, "reason": reason}))
		}
		if waiting != nil {
			waiting(reason)
		}
	})
}

// trainingStep says, in the App language lang, that a training run holding
// this computer may slow the answer, and about how long it has left.
func (a *App) trainingStep(lang string) (string, bool) {
	w, ok := a.training()
	if !ok {
		return "", false
	}
	first := locale.Key("chat:steps.training", nil)
	if w.Label() != "" {
		first = locale.Key("chat:steps.trainingNamed", map[string]any{"name": w.Label()})
	}
	_, body := locale.Message{Body: append([]locale.Text{first}, timeLeft(w)...)}.Render(lang)
	return body, true
}

// isTraining reports whether a training run holds this computer.
func (a *App) isTraining() bool {
	_, ok := a.training()
	return ok
}

// training is the training run holding this computer, if there is one.
func (a *App) training() (*share.Work, bool) {
	if a.Share == nil {
		return nil, false
	}
	return a.Share.Running(share.Training)
}

// timeLeft is a sentence saying about how long work has left, or none.
func timeLeft(w *share.Work) []locale.Text {
	m, ok := minutesLeft(w)
	switch {
	case !ok:
		return nil
	case m < 1:
		return []locale.Text{locale.Key("chat:steps.leftUnderMinute", nil)}
	case m < 120:
		return []locale.Text{locale.Key("chat:steps.leftMinutes", map[string]any{"count": m})}
	default:
		return []locale.Text{locale.Key("chat:steps.leftHours", map[string]any{"count": (m + 30) / 60})}
	}
}

// minutesLeft is a work's time left, rounded to minutes.
func minutesLeft(w *share.Work) (int, bool) {
	d, ok := w.Remaining()
	return int((d + 30*time.Second) / time.Minute), ok
}

// explainFailure writes a chat error for the person in the App language
// lang. A model failure is shown as written, so its sentence is replaced:
// an out-of-memory failure while training holds this computer says why and
// when to retry, and a generic one is translated. Other errors are left as
// they are; clients show them by their code.
func (a *App) explainFailure(lang, errText string) string {
	w, ok := a.training()
	if !ok || !modelhealth.OutOfMemory(errText) {
		if f, isHealth := modelhealth.Parse(errText); isHealth && f.Message == modelhealth.UserMessage(f.LikelyMemoryPressure) {
			key := "chat:modelFailure.stopped"
			if f.LikelyMemoryPressure {
				key = "chat:modelFailure.outOfMemory"
			}
			return modelhealth.WithMessage(errText, locale.T(lang, key, nil))
		}
		return errText
	}
	first := locale.Key("chat:errors.trainingMemory.busy", nil)
	if w.Label() != "" {
		first = locale.Key("chat:errors.trainingMemory.busyNamed", map[string]any{"name": w.Label()})
	}
	body := append([]locale.Text{first}, timeLeft(w)...)
	_, message := locale.Message{Body: append(body, locale.Key("chat:errors.trainingMemory.advice", nil))}.Render(lang)
	if _, isHealth := modelhealth.Parse(errText); isHealth {
		return modelhealth.WithMessage(errText, message)
	}
	return modelhealth.Encode(modelhealth.Failure{Reason: modelhealth.ReasonOOM, LikelyMemoryPressure: true, Message: message})
}
