package app

import (
	"context"

	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/internal/egress"
	"github.com/yeixio/toskar-core/internal/remotetools"
	"github.com/yeixio/toskar-core/internal/share"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// newToolNetwork places heavy tools, such as images and transcription, on
// the paired computer that suits them (Gungnir §14–16).
func (a *App) newToolNetwork(cfg config.Config) *remotetools.Network {
	return &remotetools.Network{LocalID: cfg.NodeID, LocalName: cfg.NodeName, Peers: a.toolPeers,
		Sent: func(ctx context.Context, p remotetools.Peer, tool string) {
			if a.Egress != nil {
				a.Egress.Add(ctx, egress.PairedComputer, p.Name, "files and arguments for "+tool)
			}
		}}
}

// registerPortable registers a tool that can run on any paired computer.
func (a *App) registerPortable(p remotetools.Portable) {
	a.portable = append(a.portable, p)
	a.Tools.Register(a.toolNet.Proxy(p))
}

// toolPeers lists the paired computers that may run tools.
func (a *App) toolPeers(ctx context.Context) []remotetools.Peer {
	if a.Nodes == nil {
		return nil
	}
	list, err := a.Nodes.List(ctx)
	if err != nil {
		return nil
	}
	var out []remotetools.Peer
	for _, n := range list {
		if n.IsLocal || !n.Paired || n.Address == "" {
			continue
		}
		out = append(out, remotetools.Peer{ID: n.ID, Name: nodeDisplayName(n),
			Online: n.Status == contracts.NodeStatusOnline, Client: a.peerClient(n)})
	}
	return out
}

// enterToolWork admits a tool run another computer sent, after this
// computer's own chats.
func (a *App) enterToolWork(ctx context.Context, label string) (func(), error) {
	w, err := a.enterWork(ctx, share.Interactive, label, nil)
	if err != nil {
		return nil, err
	}
	if w == nil {
		return func() {}, nil
	}
	return w.Done, nil
}

// toolProviders is each computer's tool providers, for Diagnostics.
func (a *App) toolProviders(ctx context.Context) any {
	if a.toolNet == nil {
		return []remotetools.NodeProviders{}
	}
	return a.toolNet.Status(ctx, a.portable)
}
