package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/yggdrasil-core/internal/config"
	"github.com/yeixio/yggdrasil-core/internal/egress"
	"github.com/yeixio/yggdrasil-core/internal/nodes"
	"github.com/yeixio/yggdrasil-core/internal/profiles"
	"github.com/yeixio/yggdrasil-core/internal/runlog"
	"github.com/yeixio/yggdrasil-core/internal/runtimes/external"
	"github.com/yeixio/yggdrasil-core/internal/scheduler"
	"github.com/yeixio/yggdrasil-core/internal/share"
	"github.com/yeixio/yggdrasil-core/internal/structured"
	"github.com/yeixio/yggdrasil-core/pkg/contracts"
	"github.com/yeixio/yggdrasil-core/pkg/pluginapi"
)

func (a *App) peerClient(n contracts.Node) *nodes.Client {
	return nodes.NewClient(normalizeNodeAddr(n.Address), a.Identity, n.ID)
}

// listNodesWithHardware returns the cluster node list with fresh hardware (incl. disk)
// from online paired peers so Computers can show free space and block tight deploys.
func (a *App) listNodesWithHardware(ctx context.Context) ([]contracts.Node, error) {
	if a.Nodes != nil {
		a.Nodes.RefreshPairedLiveness(ctx)
	}
	list, err := a.Nodes.List(ctx)
	if err != nil {
		return nil, err
	}
	a.enrichRemoteHardware(ctx, list)
	return list, nil
}

func (a *App) enrichRemoteHardware(ctx context.Context, list []contracts.Node) {
	if a.Identity == nil || len(list) == 0 {
		return
	}
	var wg sync.WaitGroup
	for i := range list {
		n := &list[i]
		if n.IsLocal || !n.Paired || n.Address == "" || n.Status == contracts.NodeStatusOffline {
			continue
		}
		wg.Add(1)
		go func(n *contracts.Node) {
			defer wg.Done()
			client := a.peerClient(*n)
			inv, err := client.Hardware(ctx)
			if err != nil {
				return
			}
			cp := inv
			n.Hardware = &cp
			if inv.OS != "" {
				n.OS = inv.OS
			}
			if inv.Arch != "" {
				n.Arch = inv.Arch
			}
			if a.Nodes != nil && a.Nodes.Pairing() != nil {
				_ = a.Nodes.Pairing().SetNodeHardware(ctx, n.ID, inv)
			}
		}(n)
	}
	wg.Wait()
}

func (a *App) syncInternalBind() error {
	return a.Config.Update(func(c *config.Config) {
		if c.DiscoveryEnabled {
			c.InternalHost = "0.0.0.0"
		} else {
			c.InternalHost = config.DefaultBindLoopback
		}
	})
}

func (a *App) bifrostAdvertiseAddr() string {
	cfg := a.Config.Get()
	host := cfg.AdvertiseHost
	if host == "" {
		host = firstNonLoopbackIPv4()
	}
	if host == "" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, strconv.Itoa(cfg.InternalPort))
}

func firstNonLoopbackIPv4() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			ip = ip.To4()
			if ip != nil {
				return ip.String()
			}
		}
	}
	return ""
}

func (a *App) buildInstalledMap(ctx context.Context, nodeList []contracts.Node, modelID string) map[string][]string {
	installed := map[string][]string{}
	if modelID == "" {
		return installed
	}
	for _, n := range nodeList {
		if n.IsLocal {
			if a.Models != nil {
				if m, err := a.Models.Get(ctx, modelID); err == nil && m.Installed {
					installed[n.ID] = []string{modelID}
				}
			}
			continue
		}
		if !n.Paired || n.Address == "" {
			continue
		}
		client := a.peerClient(n)
		models, err := client.ListModels(ctx)
		if err != nil {
			continue
		}
		for _, m := range models {
			if m.ID == modelID && m.Installed {
				installed[n.ID] = []string{modelID}
				break
			}
		}
	}
	return installed
}

func (a *App) placeRole(ctx context.Context, profile profiles.Profile, role, modelID string) (string, error) {
	return a.placeRoleAvoiding(ctx, profile, role, modelID, nil)
}

func (a *App) placeRoleAvoiding(ctx context.Context, profile profiles.Profile, role, modelID string, avoidNodeIDs []string) (string, error) {
	return a.placeRoleWith(ctx, profile, role, modelID, avoidNodeIDs, nil, false)
}

// placeRoleWith places a role, never on an excluded computer, and only on
// one that has the model when requireModel is set.
func (a *App) placeRoleWith(ctx context.Context, profile profiles.Profile, role, modelID string, avoidNodeIDs, excludeNodeIDs []string, requireModel bool) (string, error) {
	if a.Nodes != nil {
		// A chat must not wait on a full probe of every paired computer.
		a.Nodes.RefreshPairedLivenessIfStale(ctx)
	}
	nodeList, err := a.Nodes.List(ctx)
	if err != nil {
		return "", err
	}
	installed := a.buildInstalledMap(ctx, nodeList, modelID)
	candidates := scheduler.BuildCandidates(nodeList, installed, nil)
	decision, err := a.Scheduler.PlaceRole(ctx, scheduler.ScoreInput{
		Role: role, ModelID: modelID, Profile: profile, Nodes: candidates,
		AvoidNodeIDs:   a.rememberUnstable(profile.NodePolicy.Mode, modelID, avoidNodeIDs),
		ExcludeNodeIDs: excludeNodeIDs,
		RequireModel:   requireModel,
		ActiveTasks:    a.activeWork(),
		Training:       a.Nodes.TrainingNodes(),
	})
	if err != nil {
		return "", err
	}
	return decision.NodeID, nil
}

// generateOnNode streams a turn from modelID on a node. adapter applies a
// specialized AI's LoRA adapter; adapters exist only on this computer.
func (a *App) generateOnNode(ctx context.Context, nodeID, modelID, role, adapter string, messages []pluginapi.ChatMessage) (<-chan pluginapi.ChatChunk, error) {
	if external.IsModel(modelID) {
		return a.generateExternal(ctx, modelID, messages)
	}
	cfg := a.Config.Get()
	if adapter != "" && nodeID != "" && nodeID != cfg.NodeID {
		return nil, fmt.Errorf("specialized AIs run on the computer that trained them")
	}
	if nodeID == "" || nodeID == cfg.NodeID {
		if a.stubInference {
			return a.stubGenerate(modelID, messages), nil
		}
		loadStarted := time.Now()
		endpoint, err := a.ensureLocalModel(ctx, modelID)
		if err != nil {
			return nil, err
		}
		// A model that was already running answers at once; count only a
		// real start as load time (§35).
		if d := time.Since(loadStarted); d >= minLoad {
			runlog.From(ctx).Loaded(modelID, d)
		}
		ch, err := a.Runtimes.Chat(ctx, pluginapi.ChatRequest{
			ModelEndpoint:  endpoint,
			Messages:       messages,
			Stream:         true,
			Adapter:        adapter,
			ResponseSchema: structured.SchemaFrom(ctx),
		})
		if err != nil {
			return nil, err
		}
		return countWork(ctx, a.guardLocalChat(ctx, endpoint, ch), a.beginWork(cfg.NodeID)), nil
	}
	n, err := a.findPairedNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	if n.Status != "" && n.Status != contracts.NodeStatusOnline {
		return nil, fmt.Errorf( //nolint:staticcheck // ST1005: sentence shown in the UI
			"%s is offline. Turn that computer on and open Toskar, or change the role to Automatic placement.",
			nodeDisplayName(n),
		)
	}
	client := a.peerClient(n)
	if _, err := client.StartModel(ctx, modelID); err != nil && isConnectivityErr(err) {
		return nil, remoteUnreachableErr(n, err)
	}
	a.Egress.Add(ctx, egress.PairedComputer, nodeDisplayName(n), fmt.Sprintf("prompt and context for %s", modelID))
	ch, err := client.Chat(ctx, nodes.RemoteChatRequest{
		ModelID:           modelID,
		Messages:          messages,
		Role:              role,
		RequesterNodeID:   cfg.NodeID,
		RequesterNodeName: cfg.NodeName,
	})
	if err != nil {
		return nil, remoteUnreachableErr(n, err)
	}
	return countWork(ctx, wrapRemoteChat(ch, n), a.beginWork(n.ID)), nil
}

// beginWork counts a turn streaming on a computer until the returned
// function is called, so placement can prefer a computer that isn't busy
// (#111).
func (a *App) beginWork(nodeID string) func() {
	a.workMu.Lock()
	defer a.workMu.Unlock()
	if a.work == nil {
		a.work = map[string]int{}
	}
	a.work[nodeID]++
	var once sync.Once
	return func() {
		once.Do(func() {
			a.workMu.Lock()
			defer a.workMu.Unlock()
			if a.work[nodeID]--; a.work[nodeID] <= 0 {
				delete(a.work, nodeID)
			}
		})
	}
}

// activeWork is the number of turns streaming on each computer.
func (a *App) activeWork() map[string]int {
	a.workMu.Lock()
	defer a.workMu.Unlock()
	out := make(map[string]int, len(a.work))
	for k, v := range a.work {
		out[k] = v
	}
	return out
}

// countWork passes a turn's chunks through and ends its count when the
// stream closes or the turn ends.
func countWork(ctx context.Context, in <-chan pluginapi.ChatChunk, done func()) <-chan pluginapi.ChatChunk {
	out := make(chan pluginapi.ChatChunk, cap(in))
	go func() {
		defer close(out)
		defer done()
		for c := range in {
			select {
			case out <- c:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

func nodeDisplayName(n contracts.Node) string {
	if n.Name != "" {
		return n.Name
	}
	if n.ID != "" {
		return n.ID
	}
	return "That computer"
}

func remoteUnreachableErr(n contracts.Node, err error) error {
	if err == nil {
		return nil
	}
	if !isConnectivityErr(err) {
		return err
	}
	return fmt.Errorf( //nolint:staticcheck // ST1005: sentence shown in the UI
		"%s is offline or unreachable. Check that Toskar is running on that computer, or change placement to Automatic.",
		nodeDisplayName(n),
	)
}

func isConnectivityErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{
		"connection refused",
		"no such host",
		"i/o timeout",
		"timeout",
		"network is unreachable",
		"connection reset",
		"broken pipe",
		"dial tcp",
		"connectex",
		"actively refused",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	var opErr *net.OpError
	return errors.As(err, &opErr)
}

// wrapRemoteChat remaps mid-stream transport failures into a clear offline message.
func wrapRemoteChat(in <-chan pluginapi.ChatChunk, n contracts.Node) <-chan pluginapi.ChatChunk {
	out := make(chan pluginapi.ChatChunk)
	go func() {
		defer close(out)
		for chunk := range in {
			if chunk.Error != "" && isConnectivityErr(fmt.Errorf("%s", chunk.Error)) {
				chunk.Error = fmt.Sprintf(
					"%s went offline during this step. Try again when it is back, or set the role to Automatic placement.",
					nodeDisplayName(n),
				)
			}
			out <- chunk
		}
	}()
	return out
}

func (a *App) ensureLocalModel(ctx context.Context, modelID string) (string, error) {
	return a.ensureLocalModelWith(ctx, modelID, a.localAdapters(ctx, modelID))
}

// ensureLocalModelWith starts modelID with the given LoRA adapters loaded.
func (a *App) ensureLocalModelWith(ctx context.Context, modelID string, adapters []pluginapi.Adapter) (string, error) {
	if a.stubInference {
		return "stub://" + a.Config.Get().NodeID, nil
	}
	if modelID == "" {
		return "", contracts.Errorf("NO_MODEL_ASSIGNED", nil, "no model assigned for role")
	}
	path, err := a.Models.Path(ctx, modelID)
	if err != nil {
		return "", err
	}
	running, err := a.Runtimes.StartModel(ctx, "llamacpp", pluginapi.ModelStartConfig{
		ModelID: modelID, ModelPath: path, Adapters: adapters,
	})
	if err != nil {
		return "", err
	}
	a.trackRunning(running)
	a.markModelUsed(modelID)
	return running.Endpoint, nil
}

func (a *App) internalChat(ctx context.Context, req nodes.RemoteChatRequest) (<-chan pluginapi.ChatChunk, error) {
	// A paired computer's request counts as chat until its stream ends, so
	// training on this computer waits for it (§60).
	work, _ := a.enterWork(ctx, share.Interactive, "paired computer", nil)
	ch, err := a.internalChatStream(ctx, req)
	if err != nil {
		work.Done()
		return nil, err
	}
	if work == nil {
		return ch, nil
	}
	out := make(chan pluginapi.ChatChunk, 16)
	go func() {
		defer close(out)
		defer work.Done()
		for chunk := range ch {
			out <- chunk
		}
	}()
	return out, nil
}

func (a *App) internalChatStream(ctx context.Context, req nodes.RemoteChatRequest) (<-chan pluginapi.ChatChunk, error) {
	var ch <-chan pluginapi.ChatChunk
	var err error
	if a.stubInference {
		ch = a.stubGenerate(req.ModelID, req.Messages)
	} else {
		endpoint, err2 := a.ensureLocalModel(ctx, req.ModelID)
		if err2 != nil {
			return nil, err2
		}
		ch, err = a.Runtimes.Chat(ctx, pluginapi.ChatRequest{
			ModelEndpoint: endpoint,
			Messages:      req.Messages,
			Stream:        true,
			Temperature:   req.Temperature,
			MaxTokens:     req.MaxTokens,
		})
		if err != nil {
			return nil, err
		}
	}
	// Cluster requests (from a paired computer) should appear on this node's Performance tab.
	if req.RequesterNodeID != "" || req.RequesterNodeName != "" {
		return a.tapClusterServeMetrics(ch, req), nil
	}
	return ch, nil
}

func (a *App) tapClusterServeMetrics(in <-chan pluginapi.ChatChunk, req nodes.RemoteChatRequest) <-chan pluginapi.ChatChunk {
	out := make(chan pluginapi.ChatChunk, 16)
	go func() {
		defer close(out)
		var metrics *pluginapi.GenerationMetrics
		for chunk := range in {
			if chunk.Metrics != nil {
				metrics = chunk.Metrics
			}
			out <- chunk
		}
		if metrics != nil {
			a.recordClusterServe(req, metrics)
		}
	}()
	return out
}

func (a *App) recordClusterServe(req nodes.RemoteChatRequest, metrics *pluginapi.GenerationMetrics) {
	if a.Metrics == nil || metrics == nil {
		return
	}
	cfg := a.Config.Get()
	from := strings.TrimSpace(req.RequesterNodeName)
	if from == "" {
		from = "another computer"
	}
	role := strings.TrimSpace(req.Role)
	if role == "" {
		role = "worker"
	}
	run := contracts.GenerationRun{
		ConversationTitle: "Helped " + from,
		ProfileName:       "Cluster request",
		ModelID:           req.ModelID,
		RuntimeID:         "llamacpp",
		PromptTokens:      metrics.PromptTokens,
		CompletionTokens:  metrics.CompletionTokens,
		TotalTokens:       metrics.TotalTokens,
		TTFTMs:            metrics.TTFTMs,
		PromptMs:          metrics.PromptMs,
		EvalMs:            metrics.EvalMs,
		TotalMs:           metrics.TotalMs,
		PromptTokPerSec:   metrics.PromptTokPerSec,
		EvalTokPerSec:     metrics.EvalTokPerSec,
		CrossMachine:      true,
		RoleSteps: []contracts.GenerationRoleStep{{
			Role:             role,
			NodeID:           cfg.NodeID,
			NodeName:         cfg.NodeName,
			ModelID:          req.ModelID,
			PromptTokens:     metrics.PromptTokens,
			CompletionTokens: metrics.CompletionTokens,
			TotalTokens:      metrics.TotalTokens,
			TTFTMs:           metrics.TTFTMs,
			PromptMs:         metrics.PromptMs,
			EvalMs:           metrics.EvalMs,
			TotalMs:          metrics.TotalMs,
			PromptTokPerSec:  metrics.PromptTokPerSec,
			EvalTokPerSec:    metrics.EvalTokPerSec,
		}},
		CreatedAt: time.Now().UTC(),
	}
	if _, err := a.Metrics.Insert(context.Background(), run); err != nil {
		a.Logger.Warn("record cluster serve metrics", "error", err)
	}
}

// stubGenerate returns a canned completion that names this node so e2e can verify placement.
func (a *App) stubGenerate(modelID string, messages []pluginapi.ChatMessage) <-chan pluginapi.ChatChunk {
	cfg := a.Config.Get()
	ch := make(chan pluginapi.ChatChunk, 2)
	go func() {
		defer close(ch)
		content := fmt.Sprintf("stub reply from %s (%s) model=%s", cfg.NodeName, cfg.NodeID, modelID)
		if a.StubReply != nil {
			content = a.StubReply(modelID, messages)
		}
		ch <- pluginapi.ChatChunk{Content: content}
		ch <- pluginapi.ChatChunk{
			Done: true,
			Metrics: &pluginapi.GenerationMetrics{
				CompletionTokens: 8,
				TotalTokens:      8,
				TotalMs:          1,
			},
		}
	}()
	return ch
}

// alternateNode is another online computer that has modelID, for retrying a
// turn whose model failed on failedNode before trying another model (§20,
// Execution; O6). The profile's placement rules apply.
func (a *App) alternateNode(ctx context.Context, profile profiles.Profile, role, modelID string, exclude []string) (string, bool) {
	if a.Nodes == nil || a.Scheduler == nil || modelID == "" {
		return "", false
	}
	nodeID, err := a.placeRoleWith(ctx, profile, role, modelID, nil, exclude, true)
	if err != nil || nodeID == "" || slices.Contains(exclude, nodeID) {
		return "", false
	}
	return nodeID, true
}
