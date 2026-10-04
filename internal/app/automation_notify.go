package app

import (
	"context"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/gjallarhorn"
	"github.com/yeixio/toskar-core/internal/locale"
	"github.com/yeixio/toskar-core/internal/training"
)

type boolSettings interface {
	GetBool(ctx context.Context, key string, def bool) (bool, error)
}

// desktopChannel posts a notification with the operating system, while the
// user still wants desktop notices for scheduled tasks. The daemon posts it,
// so it appears while the desktop UI is closed.
type desktopChannel struct {
	settings boolSettings
	send     automations.Notifier
}

func (desktopChannel) Name() string { return "desktop" }

func (c desktopChannel) Deliver(ctx context.Context, n gjallarhorn.Notification) error {
	if c.settings != nil {
		ok, err := c.settings.GetBool(ctx, "notify_task_finish", true)
		if err != nil {
			return err
		}
		if !ok {
			return gjallarhorn.ErrSuppressed
		}
	}
	if c.send == nil {
		return gjallarhorn.ErrSuppressed
	}
	return c.send.Notify(ctx, automations.Notice{Title: n.Title, Body: n.Body})
}

// automationNotifier turns an automation's notice into a Gjallarhorn
// notification: always kept in the notification center, and posted to the
// desktop unless the user turned that off. Without a hub it posts to the
// desktop directly.
type automationNotifier struct {
	settings boolSettings
	send     automations.Notifier
	hub      *gjallarhorn.Hub
}

func (n automationNotifier) Notify(ctx context.Context, notice automations.Notice) error {
	if n.hub == nil {
		return desktopOnly(ctx, n.settings, n.send, notice)
	}
	severity := gjallarhorn.SeveritySuccess
	if notice.Failure {
		severity = gjallarhorn.SeverityError
	}
	link := "/automations"
	if notice.AutomationID != "" {
		link += "?id=" + notice.AutomationID
	}
	_, err := n.hub.Notify(ctx, gjallarhorn.Request{
		SourceType: "automation",
		SourceID:   notice.AutomationID,
		Category:   gjallarhorn.CategoryAutomation,
		Severity:   severity,
		Title:      notice.Title,
		Body:       notice.Body,
		Message:    notice.Message,
		Link:       link,
		Channels:   []string{"desktop"},
	})
	return err
}

func desktopOnly(ctx context.Context, settings boolSettings, send automations.Notifier, notice automations.Notice) error {
	if settings != nil {
		ok, err := settings.GetBool(ctx, "notify_task_finish", true)
		if err != nil {
			return err
		}
		if !ok {
			return automations.ErrNotifyDisabled
		}
	}
	if send == nil {
		return automations.ErrNotifyDisabled
	}
	return send.Notify(ctx, notice)
}

// notifyFromEvents keeps the notification center up to date with things
// that finish while nobody is looking: model downloads, training deploys,
// and pairings (Gjallarhorn §31–32), and health changes: computers going
// offline and coming back, and models that keep crashing (§23, §29). These
// are not posted to the desktop; destinations that take their category
// receive them.
func (a *App) notifyFromEvents(ctx context.Context) {
	if a.Notifications == nil || a.Bus == nil {
		return
	}
	id, ch := a.Bus.Subscribe()
	go func() {
		defer a.Bus.Unsubscribe(id)
		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-ch:
				if !ok {
					return
				}
				req, ok := noticeForEvent(a, evt)
				if !ok {
					req, ok = a.healthNotice(ctx, evt)
				}
				if ok {
					if _, err := a.Notifications.Notify(context.WithoutCancel(ctx), req); err != nil && a.Logger != nil {
						a.Logger.Warn("notification failed", "event", evt.Type, "error", err)
					}
				}
			}
		}
	}()
}

func noticeForEvent(a *App, evt events.Event) (gjallarhorn.Request, bool) {
	str := func(k string) string { v, _ := evt.Payload[k].(string); return v }
	switch evt.Type {
	case events.ModelDownloadCompleted:
		return gjallarhorn.Request{SourceType: "model", SourceID: str("model_id"), Category: gjallarhorn.CategoryModel,
			Severity: gjallarhorn.SeveritySuccess, Link: "/models", DedupeKey: "model.download:" + str("model_id"),
			Message: notice("modelReady", nil, "modelReadyBody", map[string]any{"model": a.modelName(str("model_id"))})}, true
	case events.ModelDownloadFailed:
		return gjallarhorn.Request{SourceType: "model", SourceID: str("model_id"), Category: gjallarhorn.CategoryModel,
			Severity: gjallarhorn.SeverityError, Link: "/models", DedupeKey: "model.download:" + str("model_id"),
			Message: notice("downloadFailed", nil, "downloadFailedBody", map[string]any{"model": a.modelName(str("model_id")), "error": str("error")})}, true
	case training.EventExportCompleted:
		return gjallarhorn.Request{SourceType: "training", SourceID: str("ai_id"), Category: gjallarhorn.CategoryTraining,
			Severity: gjallarhorn.SeveritySuccess, Link: "/train",
			Message: notice("modelExported", nil, "modelExportedBody", map[string]any{"name": str("name")})}, true
	case training.EventExportFailed:
		return gjallarhorn.Request{SourceType: "training", SourceID: str("ai_id"), Category: gjallarhorn.CategoryTraining,
			Severity: gjallarhorn.SeverityError, Link: "/train",
			Message: notice("exportFailed", nil, "exportFailedBody", map[string]any{"name": str("name"), "error": str("error")})}, true
	case "training.deployed":
		return gjallarhorn.Request{SourceType: "training", SourceID: str("ai_id"), Category: gjallarhorn.CategoryTraining,
			Severity: gjallarhorn.SeveritySuccess, Link: "/train",
			Message: notice("aiDeployed", nil, "aiDeployedBody", nil)}, true
	case events.NodePaired:
		return gjallarhorn.Request{SourceType: "node", SourceID: str("node_id"), Category: gjallarhorn.CategorySystem,
			Severity: gjallarhorn.SeveritySuccess, Link: "/nodes", DedupeKey: "node.paired:" + str("node_id"),
			Message: notice("computerPaired", nil, "computerPairedBody", nil)}, true
	}
	return gjallarhorn.Request{}, false
}

// notice is a notification's message from the notices in
// i18n/locales/<language>/notifications.json: a title and a body sentence.
func notice(title string, titleParams map[string]any, body string, bodyParams map[string]any) *locale.Message {
	m := &locale.Message{Title: locale.Key("notifications:notices."+title, titleParams)}
	if body != "" {
		m.Body = []locale.Text{locale.Key("notifications:notices."+body, bodyParams)}
	}
	return m
}
