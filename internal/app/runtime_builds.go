package app

import "context"

// upgradeRuntimeBuilds swaps in a better build of each runtime that has one
// once the daemon starts, such as llama.cpp's GPU build on a computer that
// got the CPU build before. Models are not running yet then, so no
// llama-server loses its files; one started meanwhile defers the upgrade to
// the next start.
func (a *App) upgradeRuntimeBuilds(ctx context.Context) {
	if a.Runtimes == nil {
		return
	}
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		done, err := a.Runtimes.UpgradeBuilds(ctx)
		if err != nil && ctx.Err() == nil && a.Logger != nil {
			a.Logger.Warn("runtime build not upgraded", "error", err)
		}
		if len(done) > 0 {
			a.invalidateCapabilities()
			if a.Logger != nil {
				a.Logger.Info("runtime build upgraded", "runtimes", done)
			}
		}
	}()
}
