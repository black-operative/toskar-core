package app

import (
	"context"
	"strings"
	"time"

	modelhealth "github.com/yeixio/toskar-core/internal/models/health"
	"github.com/yeixio/toskar-core/internal/runtimes"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// llamaStopper unloads the failed llama-server. StopModel interrupts, then kills if it is still running.
type llamaStopper struct {
	rt *runtimes.Manager
}

func (s llamaStopper) Graceful(ctx context.Context, inst modelhealth.Instance) error {
	if s.rt == nil {
		return nil
	}
	return ignoreMissing(s.rt.StopModel(ctx, "llamacpp", inst.RunningModelID))
}

func (s llamaStopper) Force(ctx context.Context, inst modelhealth.Instance) error {
	return s.Graceful(ctx, inst)
}

func (s llamaStopper) Alive(inst modelhealth.Instance) bool {
	if s.rt == nil {
		return false
	}
	running, err := s.rt.ListRunning(context.Background(), "llamacpp")
	if err != nil {
		return false
	}
	for _, item := range running {
		if item.ID == inst.RunningModelID && item.Status == "running" {
			return true
		}
	}
	return false
}

func ignoreMissing(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "not found") {
		return nil
	}
	return err
}

func (a *App) trackRunning(running pluginapi.RunningModel) {
	if a.Health == nil || running.ID == "" {
		return
	}
	nodeID := ""
	if a.Config != nil {
		nodeID = a.Config.Get().NodeID
	}
	a.Health.Register(modelhealth.Instance{
		ModelID:        running.ModelID,
		RunningModelID: running.ID,
		NodeID:         nodeID,
		RuntimeID:      "llamacpp",
		Endpoint:       running.Endpoint,
	})
}

func (a *App) memoryPressure(ctx context.Context) bool {
	if a.hw == nil {
		return false
	}
	inv, err := a.hw.Detect(ctx)
	if err != nil {
		return false
	}
	const low = 256 * 1024 * 1024
	if inv.Memory.AvailableBytes > 0 && inv.Memory.AvailableBytes < low {
		return true
	}
	if inv.Memory.SwapUsedBytes > 512*1024*1024 && inv.Memory.AvailableBytes > 0 && inv.Memory.AvailableBytes < 1024*1024*1024 {
		return true
	}
	return false
}

// guardLocalChat stops the chat request when the running model dies or stalls and the runtime probe fails.
func (a *App) guardLocalChat(ctx context.Context, endpoint string, in <-chan pluginapi.ChatChunk) <-chan pluginapi.ChatChunk {
	if a == nil || a.Health == nil {
		return in
	}
	inst, ok := a.Health.ByEndpoint(endpoint)
	if !ok {
		return in
	}
	if !a.Health.Accepting(inst.RunningModelID) {
		return failedChat(a.Health, inst.RunningModelID)
	}
	_, cancel := a.Health.BeginGeneration(inst.RunningModelID)
	out := make(chan pluginapi.ChatChunk, 8)
	go func() {
		defer close(out)
		defer cancel()
		defer a.Health.EndGeneration(inst.RunningModelID)
		wake := a.Health.Wake(inst.RunningModelID)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-wake:
				if fail, ok := a.Health.FailureOf(inst.RunningModelID); ok {
					out <- pluginapi.ChatChunk{Error: modelhealth.Encode(fail), Done: true}
				}
				return
			case <-ticker.C:
				if fail, ok := a.Health.CheckGeneration(inst.RunningModelID); ok {
					out <- pluginapi.ChatChunk{Error: modelhealth.Encode(fail), Done: true}
					return
				}
			case chunk, ok := <-in:
				if !ok {
					return
				}
				if chunk.Content != "" {
					a.Health.NoteProgress(inst.RunningModelID)
				}
				if chunk.Error != "" && a.Health.ReportRuntimeError(inst.RunningModelID, chunk.Error) {
					if fail, found := a.Health.FailureOf(inst.RunningModelID); found {
						chunk.Error = modelhealth.Encode(fail)
					}
				}
				if fail, found := a.Health.FailureOf(inst.RunningModelID); found && chunk.Error == "" {
					out <- pluginapi.ChatChunk{Error: modelhealth.Encode(fail), Done: true}
					return
				}
				out <- chunk
				if chunk.Done || chunk.Error != "" {
					return
				}
			}
		}
	}()
	return out
}

func failedChat(mon *modelhealth.Monitor, runningID string) <-chan pluginapi.ChatChunk {
	out := make(chan pluginapi.ChatChunk, 1)
	fail, ok := mon.FailureOf(runningID)
	if !ok {
		fail = modelhealth.Failure{Kind: "model_health", Reason: modelhealth.ReasonUnknown, Message: modelhealth.UserMessage(false)}
	}
	out <- pluginapi.ChatChunk{Error: modelhealth.Encode(fail), Done: true}
	close(out)
	return out
}

func (a *App) rememberUnstable(profileNodeMode, modelID string, avoid []string) []string {
	if a.Health == nil || profileNodeMode == "manual" || modelID == "" {
		return avoid
	}
	extra := a.Health.UnstableNodes(modelID)
	if len(extra) == 0 {
		return avoid
	}
	out := make([]string, 0, len(avoid)+len(extra))
	out = append(out, avoid...)
	return append(out, extra...)
}
