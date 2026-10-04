package app

import (
	"context"
	"encoding/json"

	"github.com/yeixio/toskar-core/internal/personal"
)

// personalKey is the setting that holds how the person likes answers.
const personalKey = "personalization"

// PersonalStyle returns how the person likes answers (spec §38).
func (a *App) PersonalStyle(ctx context.Context) (personal.Style, error) {
	if a.Settings == nil {
		return personal.Style{}, nil
	}
	raw, err := a.Settings.GetString(ctx, personalKey, "")
	if err != nil || raw == "" {
		return personal.Style{}, err
	}
	var s personal.Style
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return personal.Style{}, nil
	}
	return s, nil
}

// SetPersonalStyle checks and saves a style. It is kept apart from tool
// permissions, which it cannot change.
func (a *App) SetPersonalStyle(ctx context.Context, s personal.Style) (personal.Style, error) {
	s, err := s.Clean()
	if err != nil {
		return personal.Style{}, err
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return personal.Style{}, err
	}
	return s, a.Settings.Set(ctx, personalKey, string(raw))
}

// personalBlock is the style as instructions for a turn, or "".
func (a *App) personalBlock(ctx context.Context) string {
	s, err := a.PersonalStyle(ctx)
	if err != nil {
		return ""
	}
	return s.Block()
}
