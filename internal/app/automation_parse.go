package app

import (
	"context"
	"time"

	"github.com/yeixio/toskar-core/internal/automations"
)

// parseAutomation reads an automation request (#204). An empty language is
// the App language, and an empty time zone this computer's.
func (a *App) parseAutomation(ctx context.Context, text, timeZone, language string) (automations.ParsedRequest, error) {
	if language == "" {
		language = a.appLanguage(ctx)
	}
	if timeZone == "" {
		timeZone = time.Now().Location().String()
	}
	return automations.ParseRequest(text, time.Now(), timeZone, language)
}
