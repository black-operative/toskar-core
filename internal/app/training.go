package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/yeixio/toskar-core/internal/egress"
	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/hardware"
	"github.com/yeixio/toskar-core/internal/runtimes/llamacpp"
	"github.com/yeixio/toskar-core/internal/share"
	"github.com/yeixio/toskar-core/internal/training"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// newTrainingService wires Train Your Own AI to the rest of the daemon.
func (a *App) newTrainingService() *training.Service {
	cfg := a.Config.Get()
	return training.NewService(training.Deps{
		Repo:      training.NewRepo(a.DB.SQL),
		Knowledge: a.Mimir,
		Catalog:   a.Models.Catalog().Get,
		Installed: func(ctx context.Context, modelID string) bool {
			ok, _, err := a.Models.Storage().IsInstalled(ctx, modelID)
			return err == nil && ok
		},
		Nodes:    a.trainingNodes,
		Python:   a.python,
		Trainers: []training.Trainer{training.MLX{}, training.PEFT{}},
		DataDir:  filepath.Join(cfg.DataDir, "training"),
		HFHome:   filepath.Join(cfg.DataDir, "training", "hf-cache"),
		LogsDir:  cfg.LogsDir,
		Publish: func(eventType string, payload map[string]any) {
			a.Bus.Publish(events.New(eventType, payload))
		},
		UnloadLocalModels: a.unloadLocalModels,
		Admit:             a.admitTraining,
		Generate:          a.generateOnce,
		Conversation:      a.Conversations.ListMessages,
		Logger:            a.Logger,
		LocalNodeID:       cfg.NodeID,
		Sent: func(ctx context.Context, nodeName, detail string) {
			a.Egress.Add(egress.WithRun(ctx, egress.Run{Source: egress.SourceTraining}), egress.PairedComputer, nodeName, detail)
		},
		Peer: func(ctx context.Context, nodeID string) (training.Peer, error) {
			n, err := a.findPairedNode(ctx, nodeID)
			if err != nil {
				return nil, err
			}
			return a.peerClient(n), nil
		},
		ModelPath:  a.Models.Path,
		ExportTool: a.exportTool,
		FreeDisk:   hardware.FreeDisk,
	})
}

// exportTool finds llama-export-lora in the installed llama.cpp.
func (a *App) exportTool(context.Context) (string, error) {
	rt, err := a.Runtimes.Get("llamacpp")
	if err != nil {
		return "", err
	}
	lc, ok := rt.(*llamacpp.Runtime)
	if !ok {
		return "", fmt.Errorf("llama.cpp is not available")
	}
	return lc.Tool("llama-export-lora")
}

func (a *App) trainingNodes(ctx context.Context) ([]training.Node, error) {
	cfg := a.Config.Get()
	list, err := a.listNodesWithHardware(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]training.Node, 0, len(list))
	sawLocal := false
	for _, n := range list {
		local := n.IsLocal || n.ID == cfg.NodeID
		sawLocal = sawLocal || local
		node := training.Node{ID: n.ID, Name: n.Name, Local: local, Online: n.Status == contracts.NodeStatusOnline}
		if n.Hardware != nil {
			node.Hardware = *n.Hardware
		}
		out = append(out, node)
	}
	if !sawLocal {
		hw, _ := a.detectHardware(ctx)
		out = append(out, training.Node{ID: cfg.NodeID, Name: cfg.NodeName, Local: true, Online: true, Hardware: hw})
	}
	return out, nil
}

// unloadLocalModels stops every local llama-server so training has the memory.
func (a *App) unloadLocalModels(ctx context.Context) int {
	running, err := a.Runtimes.ListRunning(ctx, "llamacpp")
	if err != nil {
		return 0
	}
	n := 0
	for _, r := range running {
		if a.stopModelLocal(ctx, r.ID) == nil {
			n++
		}
	}
	return n
}

// localAdapters lists the LoRA adapters a base model's process should load.
func (a *App) localAdapters(ctx context.Context, modelID string) []pluginapi.Adapter {
	if a.Training == nil {
		return nil
	}
	list, err := a.Training.AdaptersFor(ctx, modelID)
	if err != nil {
		a.Logger.Warn("list adapters", "model_id", modelID, "error", err)
		return nil
	}
	return list
}

// generateOnce runs one non-streaming turn for an evaluation.
func (a *App) generateOnce(ctx context.Context, baseModelID, adapter string, loaded []pluginapi.Adapter, messages []pluginapi.ChatMessage) (string, error) {
	endpoint, err := a.ensureLocalModelWith(ctx, baseModelID, loaded)
	if err != nil {
		return "", err
	}
	ch, err := a.Runtimes.Chat(ctx, pluginapi.ChatRequest{
		ModelEndpoint: endpoint, Messages: messages, Adapter: adapter,
		Temperature: 0.2, MaxTokens: 512,
	})
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for chunk := range ch {
		if chunk.Error != "" {
			return b.String(), errString(chunk.Error)
		}
		b.WriteString(chunk.Content)
	}
	return strings.TrimSpace(b.String()), nil
}

// specializedChat is how a sai: model id changes one chat turn.
type specializedChat struct {
	// id and name are the specialized AI's model id and name.
	id, name     string
	baseModelID  string
	adapter      string
	instructions string
	knowledge    []string
}

func (a *App) resolveSpecialized(ctx context.Context, modelID string) (*specializedChat, error) {
	if a.Training == nil || !strings.HasPrefix(modelID, training.ModelPrefix) {
		return nil, nil
	}
	res, err := a.Training.Resolve(ctx, modelID)
	if err != nil {
		return nil, err
	}
	return &specializedChat{id: modelID, name: res.AI.Name, baseModelID: res.BaseModelID, adapter: res.Adapter,
		instructions: res.AI.Instructions, knowledge: res.AI.Knowledge}, nil
}

// admitTraining waits for chat, automations, and benchmarks to finish with
// this computer, then holds it for training (§60).
func (a *App) admitTraining(ctx context.Context, name string, waiting func(string)) (training.Hold, error) {
	work, err := a.enterWork(ctx, share.Training, name, waiting)
	if err != nil {
		return nil, err
	}
	if work == nil {
		return noHold{}, nil
	}
	return work, nil
}

type noHold struct{}

func (noHold) SetRemaining(time.Duration) {}
func (noHold) Done()                      {}
