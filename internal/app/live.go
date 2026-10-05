package app

import (
	"context"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/internal/telemetry"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// startLive reads this computer's CPU, memory, and GPU figures, often while
// a model is loaded and once a minute otherwise (#317).
func (a *App) startLive(ctx context.Context) {
	a.live = telemetry.NewSampler(telemetry.NewReader(), func() bool {
		if a.Runtimes == nil {
			return false
		}
		running, err := a.Runtimes.ListRunning(ctx, "llamacpp")
		return err == nil && len(running) > 0
	})
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		a.live.Run(ctx)
	}()
}

// localLive is this computer's live figures.
func (a *App) localLive(ctx context.Context) (contracts.LiveFigures, error) {
	cfg := a.Config.Get()
	out := contracts.LiveFigures{NodeID: cfg.NodeID, NodeName: cfg.NodeName, Recent: []contracts.LiveSample{}, Day: []contracts.LiveSample{}}
	if a.live != nil {
		out.Current, out.Recent, out.Day = a.live.Figures()
	}
	return out, nil
}

// liveAll is this computer's live figures and each paired computer's, asked
// for side by side; a computer that doesn't answer in time is left out.
func (a *App) liveAll(ctx context.Context) ([]contracts.LiveFigures, error) {
	local, _ := a.localLive(ctx)
	out := []contracts.LiveFigures{local}
	if a.Nodes == nil {
		return out, nil
	}
	nodeList, err := a.Nodes.List(ctx)
	if err != nil {
		return out, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, n := range nodeList {
		if n.IsLocal || !n.Paired || n.Address == "" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			live, err := a.peerClient(n).Live(ctx)
			if err != nil {
				return
			}
			if live.NodeID == "" {
				live.NodeID = n.ID
			}
			if live.NodeName == "" {
				live.NodeName = n.Name
			}
			mu.Lock()
			out = append(out, live)
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out, nil
}
