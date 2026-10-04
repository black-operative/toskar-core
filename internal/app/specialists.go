package app

import (
	"context"
	"fmt"

	"github.com/yeixio/toskar-core/internal/huginn"
	"github.com/yeixio/toskar-core/internal/locale"
)

// chooseSpecialist routes a message to a deployed specialized AI when it is
// what that AI was trained for (spec §61). It returns the sai: model id and
// why it was chosen.
func (a *App) chooseSpecialist(ctx context.Context, message string) (string, locale.Text, bool) {
	if a.Training == nil {
		return "", locale.Text{}, false
	}
	deployed, err := a.Training.Deployed(ctx)
	if err != nil || len(deployed) == 0 {
		return "", locale.Text{}, false
	}
	installed := map[string]bool{}
	for _, m := range a.installedModels(ctx) {
		if m.Installed {
			installed[m.ID] = true
		}
	}
	var list []huginn.Specialist
	for _, d := range deployed {
		// Its base model must be here too, with the adapter.
		if !installed[d.BaseModelID] || a.recentlyFailed(d.BaseModelID) {
			continue
		}
		list = append(list, huginn.Specialist{ID: d.ModelID, Name: d.AI.Name, Goal: d.AI.Goal, Questions: d.Questions})
	}
	s, reason, ok := huginn.ChooseSpecialist(message, huginn.Classify(message), list)
	if !ok {
		return "", locale.Text{}, false
	}
	return s.ID, reason, true
}

// refuseSupporting explains that an embedding, reranker, or classifier model
// cannot answer a chat (spec §61).
func (a *App) refuseSupporting(ctx context.Context, modelID string) error {
	for _, m := range a.installedModels(ctx) {
		if m.ID != modelID {
			continue
		}
		if role := huginn.SupportRoleOf(m); role != "" {
			return fmt.Errorf("%s is %s model. It helps Toskar search and sort, but it cannot chat. Choose another model or Auto", huginn.Name(m), article(role))
		}
	}
	return nil
}

func article(role string) string {
	switch role {
	case "embedding":
		return "an embedding"
	case "reranker":
		return "a reranker"
	case "classifier":
		return "a classifier"
	}
	return "a supporting"
}
