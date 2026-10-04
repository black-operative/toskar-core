package remotetools

import (
	"context"
	"errors"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Peer is a paired computer that may run tools.
type Peer struct {
	ID     string
	Name   string
	Online bool
	Client Doer
}

// Network knows which computers can run which portable tools, and places
// each call on the best one (Norn for tools, Gungnir §14–15).
type Network struct {
	// LocalID and LocalName name this computer.
	LocalID, LocalName string
	// Peers lists the paired computers.
	Peers func(ctx context.Context) []Peer
	// Sent records a job sent to another computer, for What left this
	// computer.
	Sent func(ctx context.Context, peer Peer, tool string)
	// TTL is how long a computer's providers are remembered.
	TTL time.Duration

	mu    sync.Mutex
	known map[string]peerProviders
}

type peerProviders struct {
	at        time.Time
	providers []Provider
	err       error
	fetching  bool
}

const (
	defaultTTL   = 30 * time.Second
	fetchTimeout = 4 * time.Second
)

func (n *Network) ttl() time.Duration {
	if n.TTL > 0 {
		return n.TTL
	}
	return defaultTTL
}

// providers returns what a peer can run, fetching it when it is stale.
// With wait false it never blocks: it answers from what it knows and
// refreshes in the background.
func (n *Network) providers(ctx context.Context, p Peer, wait bool) ([]Provider, error) {
	n.mu.Lock()
	if n.known == nil {
		n.known = map[string]peerProviders{}
	}
	k, ok := n.known[p.ID]
	fresh := ok && time.Since(k.at) < n.ttl()
	if fresh || (!wait && k.fetching) {
		n.mu.Unlock()
		return k.providers, k.err
	}
	k.fetching = true
	n.known[p.ID] = k
	n.mu.Unlock()

	fetch := func(ctx context.Context) ([]Provider, error) {
		ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
		defer cancel()
		list, err := FetchProviders(ctx, p.Client)
		n.mu.Lock()
		n.known[p.ID] = peerProviders{at: time.Now(), providers: list, err: err}
		n.mu.Unlock()
		return list, err
	}
	if wait {
		return fetch(ctx)
	}
	go func() { _, _ = fetch(context.Background()) }()
	return k.providers, k.err
}

// Forget drops what is known about a computer, such as after it went
// offline or was removed.
func (n *Network) Forget(id string) {
	n.mu.Lock()
	delete(n.known, id)
	n.mu.Unlock()
}

// candidate is one place a call could run.
type candidate struct {
	local    bool
	peer     Peer
	provider Provider
	score    int
}

type policyKey struct{}

// WithPolicy carries the chat profile's computer policy to placement.
func WithPolicy(ctx context.Context, p contracts.NodePolicy) context.Context {
	return context.WithValue(ctx, policyKey{}, p)
}

func policyFrom(ctx context.Context) contracts.NodePolicy {
	p, _ := ctx.Value(policyKey{}).(contracts.NodePolicy)
	return p
}

// place ranks the computers that can run a tool now. An image goes to a
// computer whose GPU makes it; otherwise this computer is preferred, since
// nothing has to travel. The profile's policy keeps calls here, avoids
// computers, or favors some. With a language, a computer whose provider
// works in it comes before one whose doesn't (multilingual spec §20); when
// none does, every computer stays, so the tool can say why.
func (n *Network) place(ctx context.Context, local Portable, wait bool, lang string) []candidate {
	policy := policyFrom(ctx)
	var out []candidate
	if lp := local.Provider(); lp.Ready() {
		c := candidate{local: true, provider: lp, score: 20}
		if policy.Mode == "prefer_local" {
			c.score += 200
		}
		out = append(out, c)
	}
	if policy.Remote != "off" && n.Peers != nil {
		for _, p := range n.Peers(ctx) {
			if !p.Online || p.Client == nil || slices.Contains(policy.DeniedNodes, p.ID) {
				continue
			}
			list, _ := n.providers(ctx, p, wait)
			for _, pr := range list {
				if pr.Tool != local.ID() || !pr.Ready() {
					continue
				}
				c := candidate{peer: p, provider: pr}
				if slices.Contains(policy.PreferredNodes, p.ID) {
					c.score += 120
				}
				out = append(out, c)
			}
		}
	}
	for i := range out {
		if out[i].provider.Accelerated {
			out[i].score += 100
		}
	}
	if lang != "" {
		var speaks []candidate
		for _, c := range out {
			if c.provider.Speaks(lang) {
				speaks = append(speaks, c)
			}
		}
		if len(speaks) > 0 {
			out = speaks
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })
	return out
}

// Proxy makes a portable tool run wherever it is placed. It is what the
// tool registry holds.
func (n *Network) Proxy(local Portable) *Proxy { return &Proxy{net: n, local: local} }

// Proxy is a portable tool placed by the network.
type Proxy struct {
	net   *Network
	local Portable
}

func (p *Proxy) ID() string          { return p.local.ID() }
func (p *Proxy) DisplayName() string { return p.local.DisplayName() }
func (p *Proxy) Description() string { return p.local.Description() }

// Local is the tool on this computer.
func (p *Proxy) Local() Portable { return p.local }

// Available reports whether any computer can run the tool now. It answers
// from what it knows about other computers and never waits for them.
func (p *Proxy) Available() (bool, string) {
	ready, why := p.local.Available()
	if ready {
		return true, ""
	}
	if len(p.net.place(context.Background(), p.local, false, "")) > 0 {
		return true, ""
	}
	return false, why
}

// Execute runs the call on the best computer. When another computer cannot
// be reached, or turns out not to be ready, the next one is tried.
func (p *Proxy) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	places := p.net.place(ctx, p.local, true, CallLanguage(args))
	if len(places) == 0 {
		_, why := p.local.Available()
		if why == "" {
			why = p.local.ID() + " cannot run on any computer right now"
		}
		return nil, errors.New(why)
	}
	var lastErr error
	for _, c := range places {
		if c.local {
			return Execute(ctx, p.local, args)
		}
		res, err := p.runOn(ctx, c.peer, args)
		if err == nil {
			return res, nil
		}
		var re *RemoteError
		if errors.As(err, &re) && !re.NotReady {
			return nil, err
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Unreachable or not ready: ask it again next time.
		p.net.Forget(c.peer.ID)
		lastErr = err
	}
	return nil, lastErr
}

func (p *Proxy) runOn(ctx context.Context, peer Peer, args map[string]any) (map[string]any, error) {
	job, err := p.local.Prepare(ctx, args)
	if err != nil {
		return nil, err
	}
	if p.net.Sent != nil {
		p.net.Sent(ctx, peer, p.local.ID())
	}
	out, err := RunOn(ctx, peer.Client, p.local.ID(), job)
	if err != nil {
		return nil, err
	}
	res, err := p.local.Finish(ctx, out)
	if err != nil {
		return nil, err
	}
	res["computer"] = peer.Name
	return res, nil
}

// NodeProviders is one computer's providers, for Diagnostics (§16).
type NodeProviders struct {
	NodeID    string     `json:"node_id"`
	Name      string     `json:"name"`
	Local     bool       `json:"local"`
	Online    bool       `json:"online"`
	Note      string     `json:"note,omitempty"`
	Providers []Provider `json:"providers"`
}

// Status lists every computer's providers for the given tools, asking the
// online ones.
func (n *Network) Status(ctx context.Context, local []Portable) []NodeProviders {
	here := NodeProviders{NodeID: n.LocalID, Name: n.LocalName, Local: true, Online: true, Providers: []Provider{}}
	for _, p := range local {
		here.Providers = append(here.Providers, p.Provider())
	}
	out := []NodeProviders{here}
	if n.Peers == nil {
		return out
	}
	for _, p := range n.Peers(ctx) {
		np := NodeProviders{NodeID: p.ID, Name: p.Name, Online: p.Online, Providers: []Provider{}}
		switch {
		case !p.Online || p.Client == nil:
			np.Note = "offline"
		default:
			list, err := n.providers(ctx, p, true)
			if errors.Is(err, ErrOldPeer) {
				np.Note = ErrOldPeer.Error()
			} else if err != nil {
				np.Note = "could not be reached"
			}
			np.Providers = append(np.Providers, list...)
		}
		out = append(out, np)
	}
	return out
}
