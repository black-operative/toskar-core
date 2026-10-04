package app

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/yeixio/yggdrasil-core/internal/config"
	"github.com/yeixio/yggdrasil-core/internal/egress"
	"github.com/yeixio/yggdrasil-core/internal/models"
	modelhealth "github.com/yeixio/yggdrasil-core/internal/models/health"
	"github.com/yeixio/yggdrasil-core/internal/ratings"
	"github.com/yeixio/yggdrasil-core/internal/version"
	"github.com/yeixio/yggdrasil-core/pkg/contracts"
)

// newRatings sets up community model ratings (#37). Ratings stay on this
// computer unless the person shares one; each rating shared or withdrawn,
// and each download of the public summary, is recorded in What left this
// computer.
func (a *App) newRatings(cfg config.Config) *ratings.Service {
	var (
		mu  sync.Mutex
		inv contracts.HardwareInventory
		at  time.Time
	)
	return &ratings.Service{
		DB:       a.DB.SQL,
		Settings: a.Settings,
		Client: &ratings.Client{
			ServiceURL: cfg.RatingsURL, SummaryURL: cfg.RatingsSummaryURL,
			HTTP:      &http.Client{Timeout: 20 * time.Second},
			UserAgent: "Toskar/" + version.Version + " (+https://github.com/yeixio/yggdrasil-core)",
		},
		// Hardware detection is slow on some computers, and it is the same
		// from one minute to the next.
		Hardware: func(ctx context.Context) (contracts.HardwareInventory, error) {
			mu.Lock()
			defer mu.Unlock()
			if time.Since(at) < 10*time.Minute {
				return inv, nil
			}
			got, err := a.detectHardware(ctx)
			if err != nil {
				return got, err
			}
			inv, at = got, time.Now()
			return inv, nil
		},
		Models: a.Models.List,
		Record: func(ctx context.Context, destination, detail string) {
			if a.Egress != nil {
				a.Egress.Add(ctx, egress.CommunityRatings, destination, detail)
			}
		},
		OutOfMemory: modelhealth.OutOfMemory,
		LocalNode:   func() string { return a.Config.Get().NodeID },
		AppVersion:  version.Version,
	}
}

// withLanguageRatings moves the models' levels for lang by how the
// community rates them in it (multilingual spec §23), so Auto counts them.
// Nothing changes with community ratings off or no language.
func (a *App) withLanguageRatings(ctx context.Context, list []contracts.Model, lang string) []contracts.Model {
	if a.Ratings == nil || lang == "" {
		return list
	}
	byModel, err := a.Ratings.Languages(ctx)
	if err != nil {
		a.Logger.Debug("community ratings by language", "error", err)
		return list
	}
	tag, _ := ratings.RatingLanguage(lang)
	out := make([]contracts.Model, len(list))
	for i, m := range list {
		var stats []ratings.LanguageStats
		for _, st := range byModel[m.ID] {
			if st.Language == tag {
				stats = append(stats, st)
			}
		}
		out[i] = ratings.WithLanguageRatings(m, stats)
	}
	return out
}

// communitySignals are community ratings as a recommendation signal (#37),
// or nil when they are off or there is no summary yet.
func (a *App) communitySignals(ctx context.Context) map[string]models.CommunitySignal {
	if a.Ratings == nil {
		return nil
	}
	signals, err := a.Ratings.Signals(ctx)
	if err != nil {
		a.Logger.Debug("community ratings signals", "error", err)
		return nil
	}
	return signals
}
