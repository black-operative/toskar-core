package app

import (
	"context"
	"fmt"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/tools/internet"
)

// triggerWatcher checks automation triggers (#204): it fetches a page or a
// feed, public addresses only, as the web tools do, or looks at a folder in
// the home folder, and compares it with the last check.
type triggerWatcher struct {
	fetch internet.HTTPFetcher
}

func (w triggerWatcher) Check(ctx context.Context, t automations.Trigger, state []byte) (automations.Found, []byte, error) {
	switch t.Kind {
	case automations.TriggerPage:
		page, err := w.fetch.Open(ctx, t.URL)
		if err != nil {
			return automations.Found{}, nil, err
		}
		return automations.CheckPage(page.Content, state)
	case automations.TriggerFeed:
		body, err := w.fetch.Raw(ctx, t.URL)
		if err != nil {
			return automations.Found{}, nil, err
		}
		return automations.CheckFeed(body, state)
	case automations.TriggerFolder:
		return automations.CheckFolder(t.Path, state)
	}
	return automations.Found{}, nil, fmt.Errorf("unsupported trigger kind %q", t.Kind)
}
