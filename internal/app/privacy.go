package app

import (
	"context"
	"fmt"
	"time"

	"github.com/yeixio/toskar-core/internal/api"
	"github.com/yeixio/toskar-core/internal/cache"
	"github.com/yeixio/toskar-core/internal/egress"
	"github.com/yeixio/toskar-core/internal/retention"
)

// retentionKey is the setting for how long run records are kept, in days.
const retentionKey = "run_retention_days"

// EgressRecords lists what left this computer (§63).
func (a *App) EgressRecords(ctx context.Context, f egress.Filter) ([]egress.Record, error) {
	return a.Egress.List(ctx, f)
}

func (a *App) runRetentionDays(ctx context.Context) int {
	if a.Settings == nil {
		return retention.DefaultDays
	}
	days, err := a.Settings.GetInt(ctx, retentionKey, retention.DefaultDays)
	if err != nil {
		return retention.DefaultDays
	}
	return days
}

// PrivacyOverview summarizes what left this computer and the retention.
func (a *App) PrivacyOverview(ctx context.Context) (api.PrivacyOverview, error) {
	counts, err := a.Egress.Summary(ctx, time.Now().Add(-30*24*time.Hour))
	if err != nil {
		return api.PrivacyOverview{}, err
	}
	return api.PrivacyOverview{RetentionDays: a.runRetentionDays(ctx), Last30Days: counts}, nil
}

// SetRunRetention sets how long run records are kept; 0 keeps them.
func (a *App) SetRunRetention(ctx context.Context, days int) error {
	if days < 0 || days > 3650 {
		return fmt.Errorf("retention must be between 0 (keep) and 3650 days")
	}
	if err := a.Settings.SetInt(ctx, retentionKey, days); err != nil {
		return err
	}
	a.sweepRunRecords(ctx)
	return nil
}

// DeleteRunRecords deletes run records now.
func (a *App) DeleteRunRecords(ctx context.Context) (retention.Counts, error) {
	c, err := retention.All(ctx, a.DB.SQL)
	// Personal caches, such as recent web searches, go with run records.
	if a.Caches != nil {
		a.Caches.ClearPrivacy(cache.Personal)
	}
	if err == nil && a.Logger != nil {
		a.Logger.Info("run records deleted", "tasks", c.Tasks, "runs", c.Runs, "automation_runs", c.AutomationRuns, "egress", c.Egress)
	}
	return c, err
}

// sweepRunRecords removes run records older than the retention period.
func (a *App) sweepRunRecords(ctx context.Context) {
	days := a.runRetentionDays(ctx)
	if days <= 0 || a.DB == nil {
		return
	}
	c, err := retention.Before(ctx, a.DB.SQL, time.Now().Add(-time.Duration(days)*24*time.Hour))
	if a.Logger == nil {
		return
	}
	if err != nil {
		a.Logger.Warn("run record retention failed", "error", err)
		return
	}
	if c.Tasks+c.Runs+c.AutomationRuns+c.Egress > 0 {
		a.Logger.Info("old run records removed", "days", days, "tasks", c.Tasks, "runs", c.Runs, "automation_runs", c.AutomationRuns, "egress", c.Egress)
	}
}

// keepRunRecordsTidy sweeps now and then once a day until ctx ends.
func (a *App) keepRunRecordsTidy(ctx context.Context) {
	go func() {
		a.sweepRunRecords(ctx)
		t := time.NewTicker(24 * time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				a.sweepRunRecords(ctx)
			}
		}
	}()
}
