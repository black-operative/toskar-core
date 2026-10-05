package app

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/internal/models"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// runtimeLastUsed tracks in-memory activity for idle unload.
var runtimeLastUsed sync.Map // modelID -> time.Time

func (a *App) markModelUsed(modelID string) {
	now := time.Now().UTC()
	runtimeLastUsed.Store(modelID, now)
	if a.Models != nil {
		a.Models.TouchLastUsed(context.Background(), modelID)
	}
}

func (a *App) localRunningViews(ctx context.Context) ([]contracts.RunningModelView, error) {
	if a.Runtimes == nil {
		return nil, nil
	}
	running, err := a.Runtimes.ListRunning(ctx, "llamacpp")
	if err != nil {
		return nil, err
	}
	cfg := a.Config.Get()
	gpu, cpuBuild := a.accelerationFacts(ctx)
	speeds := a.recentSpeeds(ctx)
	profileNames := a.profilesUsingModels(ctx)
	out := make([]contracts.RunningModelView, 0, len(running))
	for _, r := range running {
		display := r.ModelID
		mem := uint64(0)
		if a.Models != nil {
			if m, err := a.Models.Get(ctx, r.ModelID); err == nil {
				display = m.DisplayName
				mem = m.MemoryNeeded
			}
		}
		view := contracts.RunningModelView{
			ModelID:        r.ModelID,
			DisplayName:    display,
			InstanceID:     r.ID,
			NodeID:         cfg.NodeID,
			NodeName:       cfg.NodeName,
			Status:         r.Status,
			MemoryBytes:    mem,
			Endpoint:       r.Endpoint,
			UsedByProfiles: profileNames[r.ModelID],
			Mode:           r.Mode,
			SpeedTokPerSec: speeds[r.ModelID],
			Acceleration:   accelerationView(r.Acceleration, gpu, cpuBuild),
		}
		// The device the model is on, from the runtime, not a guess from
		// the hardware list.
		if acc := view.Acceleration; acc != nil && len(acc.Devices) > 0 {
			view.Accelerator = strings.Join(acc.Devices, ", ")
		} else if acc != nil {
			view.Accelerator = "CPU"
		}
		if t, ok := runtimeLastUsed.Load(r.ModelID); ok {
			tt := t.(time.Time)
			view.LastUsedAt = &tt
		}
		out = append(out, view)
	}
	return out, nil
}

func (a *App) profilesUsingModels(ctx context.Context) map[string][]string {
	out := map[string][]string{}
	if a.Profiles == nil {
		return out
	}
	profiles, err := a.Profiles.List(ctx)
	if err != nil {
		return out
	}
	for _, p := range profiles {
		for _, role := range p.Roles {
			if role.ModelID == "" {
				continue
			}
			out[role.ModelID] = appendUnique(out[role.ModelID], p.Name)
		}
	}
	return out
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

func (a *App) listRunningAll(ctx context.Context) ([]contracts.RunningModelView, error) {
	local, err := a.localRunningViews(ctx)
	if err != nil {
		return nil, err
	}
	out := append([]contracts.RunningModelView{}, local...)
	if a.Nodes == nil {
		return out, nil
	}
	nodeList, err := a.Nodes.List(ctx)
	if err != nil {
		return out, nil
	}
	cfg := a.Config.Get()
	for _, n := range nodeList {
		if n.IsLocal || !n.Paired || n.Address == "" {
			continue
		}
		client := a.peerClient(n)
		remote, err := client.ListRunning(ctx)
		if err != nil {
			continue
		}
		for i := range remote {
			if remote[i].NodeID == "" {
				remote[i].NodeID = n.ID
			}
			if remote[i].NodeName == "" {
				remote[i].NodeName = n.Name
			}
			_ = cfg
			out = append(out, remote[i])
		}
	}
	return out, nil
}

func normalizeNodeAddr(addr string) string {
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return strings.TrimRight(addr, "/")
	}
	return "http://" + strings.TrimRight(addr, "/")
}

func (a *App) startModelLocal(ctx context.Context, modelID string) (contracts.RunningModelView, error) {
	cfg := a.Config.Get()
	if a.stubInference {
		a.markModelUsed(modelID)
		return contracts.RunningModelView{
			ModelID:     modelID,
			DisplayName: modelID,
			InstanceID:  "stub-" + modelID,
			NodeID:      cfg.NodeID,
			NodeName:    cfg.NodeName,
			Status:      "running",
			Endpoint:    "stub://" + cfg.NodeID,
		}, nil
	}
	path, err := a.Models.Path(ctx, modelID)
	if err != nil {
		return contracts.RunningModelView{}, err
	}
	running, err := a.Runtimes.StartModel(ctx, "llamacpp", pluginapi.ModelStartConfig{
		ModelID: modelID, ModelPath: path, Adapters: a.localAdapters(ctx, modelID),
	})
	if err != nil {
		return contracts.RunningModelView{}, err
	}
	a.trackRunning(running)
	a.markModelUsed(modelID)
	views, _ := a.localRunningViews(ctx)
	for _, v := range views {
		if v.InstanceID == running.ID || v.ModelID == modelID {
			return v, nil
		}
	}
	return contracts.RunningModelView{
		ModelID: modelID, DisplayName: modelID, InstanceID: running.ID,
		NodeID: cfg.NodeID, NodeName: cfg.NodeName, Status: running.Status, Endpoint: running.Endpoint,
	}, nil
}

func (a *App) stopModelLocal(ctx context.Context, instanceID string) error {
	if a.Health != nil {
		a.Health.MarkIntentional(instanceID)
	}
	return a.Runtimes.StopModel(ctx, "llamacpp", instanceID)
}

func (a *App) startModel(ctx context.Context, modelID, nodeID string) (contracts.RunningModelView, error) {
	cfg := a.Config.Get()
	if nodeID == "" || nodeID == cfg.NodeID || nodeID == "local" {
		return a.startModelLocal(ctx, modelID)
	}
	n, err := a.findPairedNode(ctx, nodeID)
	if err != nil {
		return contracts.RunningModelView{}, err
	}
	client := a.peerClient(n)
	return client.StartModel(ctx, modelID)
}

func (a *App) stopModel(ctx context.Context, instanceID, nodeID string) error {
	cfg := a.Config.Get()
	if nodeID == "" || nodeID == cfg.NodeID || nodeID == "local" {
		return a.stopModelLocal(ctx, instanceID)
	}
	n, err := a.findPairedNode(ctx, nodeID)
	if err != nil {
		return err
	}
	client := a.peerClient(n)
	return client.StopInstance(ctx, instanceID)
}

func (a *App) installModelOn(ctx context.Context, modelID, nodeID string, wait bool) error {
	if nodeID == "all" || nodeID == "everywhere" {
		return a.installModelEverywhere(ctx, modelID, wait)
	}
	cfg := a.Config.Get()
	if nodeID == "" || nodeID == "automatic" || nodeID == cfg.NodeID || nodeID == "local" {
		return a.Models.Install(ctx, modelID, wait)
	}
	n, err := a.findPairedNode(ctx, nodeID)
	if err != nil {
		return err
	}
	client := a.peerClient(n)
	return client.InstallModel(ctx, modelID, wait)
}

func (a *App) deleteModelOn(ctx context.Context, modelID, nodeID string) error {
	cfg := a.Config.Get()
	if nodeID == "" || nodeID == "automatic" || nodeID == cfg.NodeID || nodeID == "local" {
		return a.Models.Delete(ctx, modelID)
	}
	n, err := a.findPairedNode(ctx, nodeID)
	if err != nil {
		return err
	}
	client := a.peerClient(n)
	return client.DeleteModel(ctx, modelID)
}

func (a *App) installModelEverywhere(ctx context.Context, modelID string, wait bool) error {
	var errs []string
	if err := a.Models.Install(ctx, modelID, wait); err != nil {
		errs = append(errs, fmt.Sprintf("this computer: %v", err))
	}
	if a.Nodes == nil {
		if len(errs) > 0 {
			return fmt.Errorf("%s", strings.Join(errs, "; "))
		}
		return nil
	}
	nodesList, err := a.Nodes.List(ctx)
	if err != nil {
		return err
	}
	for _, n := range nodesList {
		if n.IsLocal || !n.Paired || n.Address == "" {
			continue
		}
		client := a.peerClient(n)
		if err := client.InstallModel(ctx, modelID, wait); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", n.Name, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("install on all computers had errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (a *App) installFromURLOn(ctx context.Context, req contracts.InstallFromURLRequest, wait bool) (string, error) {
	nodeID := req.NodeID
	if nodeID == "all" || nodeID == "everywhere" {
		return a.installFromURLEverywhere(ctx, req, wait)
	}
	cfg := a.Config.Get()
	if nodeID == "" || nodeID == "automatic" || nodeID == cfg.NodeID || nodeID == "local" {
		return a.Models.InstallFromURL(ctx, req, wait)
	}
	n, err := a.findPairedNode(ctx, nodeID)
	if err != nil {
		return "", err
	}
	client := a.peerClient(n)
	return client.InstallFromURL(ctx, req)
}

func (a *App) installFromURLEverywhere(ctx context.Context, req contracts.InstallFromURLRequest, wait bool) (string, error) {
	localReq := req
	localReq.NodeID = ""
	id, err := a.Models.InstallFromURL(ctx, localReq, wait)
	if err != nil {
		return "", err
	}
	if a.Nodes == nil {
		return id, nil
	}
	nodesList, err := a.Nodes.List(ctx)
	if err != nil {
		return id, err
	}
	var errs []string
	peerReq := req
	peerReq.ID = id
	peerReq.NodeID = ""
	for _, n := range nodesList {
		if n.IsLocal || !n.Paired || n.Address == "" {
			continue
		}
		client := a.peerClient(n)
		if _, err := client.InstallFromURL(ctx, peerReq); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", n.Name, err))
		}
	}
	if len(errs) > 0 {
		return id, fmt.Errorf("installed locally as %s, but some computers failed: %s", id, strings.Join(errs, "; "))
	}
	return id, nil
}

// listModelsCluster annotates catalog models with where they are installed
// (this computer and paired peers) so Chat/Models can offer cluster-wide picks.
func (a *App) listModelsCluster(ctx context.Context) ([]contracts.Model, error) {
	local, err := a.Models.List(ctx)
	if err != nil {
		return nil, err
	}
	cfg := a.Config.Get()
	presence := map[string][]contracts.ModelOnNode{}
	for _, m := range local {
		if m.Installed {
			presence[m.ID] = append(presence[m.ID], contracts.ModelOnNode{
				NodeID: cfg.NodeID, NodeName: cfg.NodeName,
			})
		}
	}
	remoteOnly := map[string]contracts.Model{}
	if a.Nodes != nil {
		if nodesList, err := a.Nodes.List(ctx); err == nil {
			for _, n := range nodesList {
				if n.IsLocal || !n.Paired || n.Address == "" {
					continue
				}
				client := a.peerClient(n)
				remote, err := client.ListModels(ctx)
				if err != nil {
					continue
				}
				for _, rm := range remote {
					if !rm.Installed {
						continue
					}
					presence[rm.ID] = append(presence[rm.ID], contracts.ModelOnNode{
						NodeID: n.ID, NodeName: n.Name,
					})
					foundLocal := false
					for _, lm := range local {
						if lm.ID == rm.ID {
							foundLocal = true
							break
						}
					}
					if !foundLocal {
						remoteOnly[rm.ID] = rm
					}
				}
			}
		}
	}
	out := make([]contracts.Model, 0, len(local)+len(remoteOnly))
	for _, m := range local {
		m.InstalledOn = presence[m.ID]
		if len(m.InstalledOn) > 0 {
			m.Installed = true
		}
		out = append(out, m)
	}
	for id, m := range remoteOnly {
		m.Installed = true
		m.InstalledOn = presence[id]
		out = append(out, m)
	}
	// The external server's models, to choose; Auto never picks them.
	if ext, err := a.externalModels(ctx); err == nil {
		out = append(out, ext...)
	}
	return out, nil
}

func (a *App) findPairedNode(ctx context.Context, nodeID string) (contracts.Node, error) {
	if a.Nodes == nil {
		return contracts.Node{}, fmt.Errorf("nodes unavailable")
	}
	list, err := a.Nodes.List(ctx)
	if err != nil {
		return contracts.Node{}, err
	}
	for _, n := range list {
		if n.ID == nodeID {
			if !n.Paired {
				return contracts.Node{}, fmt.Errorf("%s is not paired", nodeDisplayName(n))
			}
			if n.Address == "" {
				return contracts.Node{}, fmt.Errorf( //nolint:staticcheck // ST1005: sentence shown in the UI
					"%s is offline or has no network address yet. Tap Refresh on Computers, or set the role to Automatic placement.",
					nodeDisplayName(n),
				)
			}
			return n, nil
		}
	}
	return contracts.Node{}, fmt.Errorf("computer %q was not found in your cluster", nodeID)
}

func (a *App) modelsFitAll(ctx context.Context) ([]contracts.ModelsFitResponse, error) {
	hw, err := a.detectHardware(ctx)
	if err != nil {
		hw = contracts.HardwareInventory{}
	}
	cfg := a.Config.Get()
	running, _ := a.listRunningAll(ctx)
	local := a.fitOptions(ctx, cfg.NodeID, true, running)
	out := []contracts.ModelsFitResponse{a.Models.Fit(hw, cfg.NodeID, cfg.NodeName, local)}
	if a.Nodes == nil {
		return out, nil
	}
	nodesList, err := a.listNodesWithHardware(ctx)
	if err != nil {
		return out, nil
	}
	for _, n := range nodesList {
		if n.IsLocal || n.Hardware == nil {
			continue
		}
		out = append(out, a.Models.Fit(*n.Hardware, n.ID, n.Name, a.fitOptions(ctx, n.ID, false, running)))
	}
	return out, nil
}

// fitOptions reuses last-used history and completed benchmark samples.
// A remote computer does not inherit this machine's proof that a model ran.
func (a *App) fitOptions(ctx context.Context, nodeID string, local bool, running []contracts.RunningModelView) models.FitOptions {
	opts := models.FitOptions{}
	if local {
		// Community ratings are for hardware like this computer's.
		opts.Community = a.communitySignals(ctx)
		opts.Proven = map[string]bool{}
		opts.Measured = map[string]models.MeasuredRun{}
		if a.Models != nil {
			if list, err := a.Models.List(ctx); err == nil {
				for _, m := range list {
					if m.LastUsedAt != nil {
						opts.Proven[m.ID] = true
					}
				}
			}
		}
		if a.Benchmarks != nil {
			for _, job := range a.Benchmarks.List() {
				for _, sample := range job.Samples {
					if sample.Warmup || sample.Error != "" || sample.EvalTokPerSec <= 0 {
						continue
					}
					prev := opts.Measured[sample.ModelID]
					if sample.EvalTokPerSec > prev.TokPerSec {
						prev.TokPerSec = sample.EvalTokPerSec
					}
					opts.Measured[sample.ModelID] = prev
					opts.Proven[sample.ModelID] = true
				}
			}
		}
	}
	for _, view := range running {
		if nodeID != "" && view.NodeID != "" && view.NodeID != nodeID {
			continue
		}
		name := view.DisplayName
		if name == "" {
			name = view.ModelID
		}
		opts.LoadedModelNames = append(opts.LoadedModelNames, name)
	}
	return opts
}
