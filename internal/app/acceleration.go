package app

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/internal/runtimes"
	"github.com/yeixio/toskar-core/internal/store/repositories"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// accelFacts are what the acceleration state needs besides the runtime's
// report: whether this computer has a GPU, and whether the CPU-only build
// of llama.cpp is installed where a GPU build would run. Both cost a
// hardware scan, and rarely change, so they are kept for a few minutes.
type accelFacts struct {
	mu       sync.Mutex
	at       time.Time
	gpu      bool
	cpuBuild bool
}

const accelFactsFor = 5 * time.Minute

func (a *App) accelerationFacts(ctx context.Context) (gpu, cpuBuild bool) {
	f := &a.accel
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.at.IsZero() && time.Since(f.at) < accelFactsFor {
		return f.gpu, f.cpuBuild
	}
	f.gpu, f.cpuBuild = false, false
	if hw, err := a.detectHardware(ctx); err == nil {
		for _, ac := range hw.Accelerators {
			if ac.Kind == "gpu" && !softwareAdapter(ac.Model) {
				f.gpu = true
			}
		}
	}
	if a.Runtimes != nil {
		if rt, err := a.Runtimes.Get("llamacpp"); err == nil {
			if u, ok := rt.(runtimes.Upgradable); ok {
				f.cpuBuild = u.UpgradeAvailable(ctx)
			}
		}
	}
	f.at = time.Now()
	return f.gpu, f.cpuBuild
}

// softwareAdapter reports display adapters that are not a GPU, such as the
// one Windows uses before a driver is installed or over Remote Desktop.
func softwareAdapter(model string) bool {
	lower := strings.ToLower(model)
	return strings.Contains(lower, "basic display") || strings.Contains(lower, "basic render") ||
		strings.Contains(lower, "remote display")
}

// accelerationView turns the runtime's report into the state the app
// shows: gpu, partial, cpu (a GPU sits unused), or cpu_expected (no GPU),
// with the reason for anything short of gpu.
func accelerationView(acc *pluginapi.Acceleration, gpu, cpuBuild bool) *contracts.Acceleration {
	if acc == nil {
		return nil
	}
	v := &contracts.Acceleration{
		Backend:         acc.Backend,
		Devices:         acc.Devices,
		LayersOffloaded: acc.LayersOffloaded,
		LayersTotal:     acc.LayersTotal,
		GPUMemoryBytes:  acc.GPUMemoryBytes,
	}
	switch {
	case acc.Backend != "cpu" && (acc.LayersTotal == 0 || acc.LayersOffloaded >= acc.LayersTotal):
		v.State = contracts.AccelerationGPU
	case acc.Backend != "cpu":
		v.State, v.Reason = contracts.AccelerationPartial, contracts.AccelerationReasonGPUMemory
	case !gpu:
		v.State, v.Reason = contracts.AccelerationCPUExpected, contracts.AccelerationReasonNoGPU
	case cpuBuild:
		v.State, v.Reason = contracts.AccelerationCPU, contracts.AccelerationReasonCPUBuild
	default:
		v.State, v.Reason = contracts.AccelerationCPU, contracts.AccelerationReasonGPUUnavailable
	}
	return v
}

// accelerationSummary is health's one word for where the loaded models
// run. Chat models decide it when any are loaded, since supporting models
// for knowledge search are small; the least accelerated one wins.
func accelerationSummary(views []contracts.RunningModelView) string {
	rank := map[string]int{
		contracts.AccelerationCPU:         4,
		contracts.AccelerationPartial:     3,
		contracts.AccelerationGPU:         2,
		contracts.AccelerationCPUExpected: 1,
	}
	pick := func(chatOnly bool) string {
		best, bestRank := "", 0
		for _, v := range views {
			if v.Acceleration == nil || (chatOnly && v.Mode != "") {
				continue
			}
			if r := rank[v.Acceleration.State]; r > bestRank {
				best, bestRank = v.Acceleration.State, r
			}
		}
		return best
	}
	if s := pick(true); s != "" {
		return s
	}
	if s := pick(false); s != "" {
		return s
	}
	if len(views) == 0 {
		return contracts.AccelerationIdle
	}
	return ""
}

// recentSpeeds is each model's generation speed in its latest replies, in
// tokens per second, from the performance records.
func (a *App) recentSpeeds(ctx context.Context) map[string]float64 {
	out := map[string]float64{}
	if a.Metrics == nil {
		return out
	}
	runs, err := a.Metrics.List(ctx, repositories.ListFilter{Sort: "created_at", Order: "desc", Limit: 50})
	if err != nil {
		return out
	}
	for _, r := range runs {
		if _, ok := out[r.ModelID]; !ok && r.EvalTokPerSec > 0 {
			out[r.ModelID] = r.EvalTokPerSec
		}
	}
	return out
}

// healthAcceleration is accelerationSummary for this computer's loaded
// models, without the database or paired computers, since health is
// polled often.
func (a *App) healthAcceleration(ctx context.Context) string {
	if a.Runtimes == nil {
		return ""
	}
	running, err := a.Runtimes.ListRunning(ctx, "llamacpp")
	if err != nil {
		return ""
	}
	gpu, cpuBuild := a.accelerationFacts(ctx)
	views := make([]contracts.RunningModelView, 0, len(running))
	for _, r := range running {
		views = append(views, contracts.RunningModelView{Mode: r.Mode, Acceleration: accelerationView(r.Acceleration, gpu, cpuBuild)})
	}
	return accelerationSummary(views)
}

// localAcceleration is where this computer's running chat instance of a
// model runs, or nil when it isn't running here or that isn't known.
func (a *App) localAcceleration(ctx context.Context, modelID string) *pluginapi.Acceleration {
	if a.Runtimes == nil || modelID == "" {
		return nil
	}
	running, err := a.Runtimes.ListRunning(ctx, "llamacpp")
	if err != nil {
		return nil
	}
	for _, r := range running {
		if r.ModelID == modelID && r.Mode == "" && r.Acceleration != nil {
			return r.Acceleration
		}
	}
	return nil
}

// accelerationLabel is a report as the backend and device a record keeps:
// "vulkan" and "AMD Radeon RX 7900 XTX", or "cpu" and "".
func accelerationLabel(acc *pluginapi.Acceleration) (backend, device string) {
	if acc == nil {
		return "", ""
	}
	return acc.Backend, strings.Join(acc.Devices, ", ")
}
