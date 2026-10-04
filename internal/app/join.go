package app

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/yeixio/yggdrasil-core/internal/api"
	"github.com/yeixio/yggdrasil-core/internal/config"
	"github.com/yeixio/yggdrasil-core/internal/egress"
	"github.com/yeixio/yggdrasil-core/internal/events"
	"github.com/yeixio/yggdrasil-core/internal/join"
	"github.com/yeixio/yggdrasil-core/internal/nodes"
	"github.com/yeixio/yggdrasil-core/internal/version"
	"github.com/yeixio/yggdrasil-core/pkg/contracts"
)

// networkKey is the setting for this computer's network ID: made with the
// first join token, or taken from the network it joined.
const networkKey = "network_id"

// newJoinAcceptor answers one-line joins (#40) on this computer.
func (a *App) newJoinAcceptor() *join.Acceptor {
	return &join.Acceptor{
		Tokens:    a.joinTokens,
		NodeID:    a.identity.NodeID,
		Name:      func() string { return a.Config.Get().NodeName },
		Key:       a.identity.PrivateKey,
		NetworkID: a.networkID,
		Address:   a.bifrostAdvertiseAddr,
		Admit:     a.admitJoining,
		Joined: func(ctx context.Context, node join.JoiningNode, name, tokenID, source string) {
			a.Logger.Info("computer joined", "node_id", node.ID, "name", name, "join_token_id", tokenID, "source", source)
			a.Bus.Publish(events.New(events.NodeJoined, map[string]any{"node_id": node.ID, "name": name, "join_token_id": tokenID, "source": source}))
			a.Bus.Publish(events.New(events.NodePaired, map[string]any{"node_id": node.ID, "name": name}))
			go a.Nodes.RefreshPairedLiveness(context.WithoutCancel(ctx))
		},
		Refused: func(_ context.Context, reason, tokenID, source string) {
			a.Logger.Warn("join refused", "reason", reason, "join_token_id", tokenID, "source", source)
			a.Bus.Publish(events.New(events.NodeJoinRefused, map[string]any{"reason": reason, "join_token_id": tokenID, "source": source}))
		},
		Logger: a.Logger,
	}
}

// networkID returns this computer's network ID, making one if it has none.
func (a *App) networkID(ctx context.Context) (string, error) {
	a.networkMu.Lock()
	defer a.networkMu.Unlock()
	id, err := a.Settings.GetString(ctx, networkKey, "")
	if err != nil || id != "" {
		return id, err
	}
	id = uuid.NewString()
	return id, a.Settings.Set(ctx, networkKey, id)
}

// admitJoining trusts a computer that proved a join token, naming it
// apart from computers already here.
func (a *App) admitJoining(ctx context.Context, node join.JoiningNode) (string, error) {
	taken := map[string]bool{strings.ToLower(a.Config.Get().NodeName): true}
	rows, err := a.DB.SQL.QueryContext(ctx, `SELECT name FROM nodes WHERE id != ?`, node.ID)
	if err != nil {
		return "", err
	}
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil {
			taken[strings.ToLower(n)] = true
		}
	}
	_ = rows.Close()
	name := strings.TrimSpace(node.Name)
	for i := 2; taken[strings.ToLower(name)]; i++ {
		name = fmt.Sprintf("%s-%d", strings.TrimSpace(node.Name), i)
	}
	return name, a.Pairing.Trust(ctx, node.ID, name, node.Address, []byte(node.PublicKeyPEM))
}

// installerBase is where the install scripts are: attached to each
// release (#40).
const installerBase = "https://github.com/yeixio/yggdrasil-core/releases/latest/download"

// errNotReachable is a computer other computers cannot reach.
var errNotReachable = contracts.Errorf("JOIN_NOT_REACHABLE", nil,
	"other computers can't reach this one: turn on Find other computers (discovery_enabled) and restart Toskar")

// reachable reports whether paired computers can reach this computer's
// Bifrost.
func (a *App) reachable() bool {
	cfg := a.Config.Get()
	if ip := net.ParseIP(cfg.InternalHost); ip != nil && ip.IsLoopback() {
		return false
	}
	host, _, _ := net.SplitHostPort(a.bifrostAdvertiseAddr())
	ip := net.ParseIP(host)
	return ip == nil || !ip.IsLoopback()
}

// CreateJoinToken makes a one-time join token and the command to use it.
func (a *App) CreateJoinToken(ctx context.Context, ttl time.Duration) (api.JoinTokenCreated, error) {
	if !a.reachable() {
		return api.JoinTokenCreated{}, errNotReachable
	}
	if _, err := a.networkID(ctx); err != nil {
		return api.JoinTokenCreated{}, err
	}
	raw, t, err := a.joinTokens.Create(ctx, ttl)
	if err != nil {
		return api.JoinTokenCreated{}, contracts.NewError("JOIN_TOKEN_FAILED", nil, err)
	}
	server := a.bifrostAdvertiseAddr()
	fp := join.Fingerprint(a.identity.PublicKey)
	a.Logger.Info("join token created", "join_token_id", t.ID, "expires_at", t.ExpiresAt)
	a.Bus.Publish(events.New(events.JoinTokenCreated, map[string]any{"join_token_id": t.ID, "expires_at": t.ExpiresAt}))
	created := api.JoinTokenCreated{
		Token: raw, Server: server, Fingerprint: fp,
		// yggctl, not toskarctl: the joining computer may be on a version
		// from before the rename, and newer ones have yggctl too (#237).
		Command: fmt.Sprintf("yggctl join --server %s --token %s --fingerprint %s", server, raw, fp),
		InstallCommand: fmt.Sprintf("curl -fsSL %s/install.sh | sh -s -- join --server %s --token %s --fingerprint %s",
			installerBase, server, raw, fp),
		WindowsCommand: fmt.Sprintf("& ([scriptblock]::Create((irm %s/install.ps1))) join -Server %s -Token %s -Fingerprint %s",
			installerBase, server, raw, fp),
	}
	created.ID, created.CreatedAt, created.ExpiresAt, created.Status = t.ID, t.CreatedAt, t.ExpiresAt, t.Status
	return created, nil
}

// ListJoinTokens lists recent join tokens, never their secrets.
func (a *App) ListJoinTokens(ctx context.Context) ([]join.Token, error) {
	return a.joinTokens.List(ctx)
}

// RevokeJoinToken stops an unused token.
func (a *App) RevokeJoinToken(ctx context.Context, id string) (join.Token, error) {
	t, err := a.joinTokens.Revoke(ctx, strings.TrimPrefix(id, "jt_"))
	if err == nil {
		a.Logger.Info("join token revoked", "join_token_id", t.ID)
		a.Bus.Publish(events.New(events.JoinTokenRevoked, map[string]any{"join_token_id": t.ID}))
	}
	return t, err
}

// errAlreadyJoined stops a join to a computer this one already trusts.
var errAlreadyJoined = errors.New("already joined")

// JoinNetwork joins this computer to the network of the computer that made
// the token.
func (a *App) JoinNetwork(ctx context.Context, req api.JoinRequest) (api.JoinResult, error) {
	server, err := join.NormalizeServer(req.Server)
	if err != nil {
		return api.JoinResult{}, contracts.NewError("JOIN_BAD_REQUEST", nil, err)
	}
	if _, _, err := join.ParseToken(req.Token); err != nil {
		return api.JoinResult{}, contracts.NewError("JOIN_BAD_REQUEST", nil, err)
	}
	if _, err := join.NormalizeFingerprint(req.Fingerprint); err != nil {
		return api.JoinResult{}, contracts.NewError("JOIN_BAD_REQUEST", nil, err)
	}
	if !a.reachable() {
		return api.JoinResult{}, errNotReachable
	}
	cfg := a.Config.Get()
	name := cfg.NodeName
	if req.Name != "" {
		if name, err = validName(req.Name); err != nil {
			return api.JoinResult{}, contracts.NewError("JOIN_BAD_REQUEST", nil, err)
		}
	}
	result := api.JoinResult{Node: api.NetworkNode{ID: cfg.NodeID, Name: name}}
	c := &join.Client{
		Server: server, Token: req.Token, Fingerprint: req.Fingerprint,
		Node: join.JoiningNode{ID: cfg.NodeID, Name: name, PublicKeyPEM: string(a.identity.CertPEM),
			Address: a.bifrostAdvertiseAddr(), Version: version.Version},
		Check: func(h join.HelloResponse) error {
			result.Server = api.NetworkNode{ID: h.NodeID, Name: h.Name, Address: server}
			result.NetworkID = h.NetworkID
			// Already trusting that computer, with the key the command
			// names: nothing to do.
			if pem, err := a.Pairing.TrustedCertPEM(ctx, h.NodeID); err == nil {
				if pub, err := join.PublicKeyFromPEM(string(pem)); err == nil && join.Fingerprint(pub) == join.Fingerprint(ed25519.PublicKey(mustB64(h.PublicKey))) {
					return errAlreadyJoined
				}
			}
			mine, err := a.Settings.GetString(ctx, networkKey, "")
			if err != nil {
				return err
			}
			if mine != "" && mine != h.NetworkID && len(a.pairedPeers(ctx)) > 0 {
				return contracts.Errorf("JOIN_OTHER_NETWORK", nil, "this computer is already in another Toskar network; run toskarctl leave first to join this one")
			}
			return nil
		},
	}
	a.Egress.Add(ctx, egress.PairedComputer, server, "Asked to join the network with this computer's name, ID, public key, and address")
	acc, _, err := c.Join(ctx)
	switch {
	case errors.Is(err, errAlreadyJoined):
		if name != cfg.NodeName {
			if err := a.rename(name); err != nil {
				return api.JoinResult{}, err
			}
		}
		result.Status = "already_joined"
		return result, nil
	case err != nil:
		return api.JoinResult{}, joinError(err)
	}
	if err := a.Pairing.Trust(ctx, acc.Server.ID, acc.Server.Name, server, []byte(acc.Server.PublicKeyPEM)); err != nil {
		return api.JoinResult{}, err
	}
	if err := a.Settings.Set(ctx, networkKey, acc.NetworkID); err != nil {
		return api.JoinResult{}, err
	}
	name = joinedName(name, acc.Name)
	if name != cfg.NodeName {
		if err := a.rename(name); err != nil {
			return api.JoinResult{}, err
		}
	}
	a.Logger.Info("joined network", "network_id", acc.NetworkID, "server", acc.Server.ID, "name", acc.Name)
	a.Bus.Publish(events.New(events.NodePaired, map[string]any{"node_id": acc.Server.ID, "name": acc.Server.Name}))
	go a.Nodes.RefreshPairedLiveness(context.WithoutCancel(ctx))
	result.Status, result.NetworkID, result.Node.Name = "joined", acc.NetworkID, acc.Name
	result.Server = api.NetworkNode{ID: acc.Server.ID, Name: acc.Server.Name, Address: server}
	return result, nil
}

func mustB64(s string) []byte {
	b, _ := base64.StdEncoding.DecodeString(s)
	return b
}

// validName is a computer name as people type it: 1 to 63 characters,
// without control characters.
func validName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || utf8.RuneCountInString(s) > 63 || strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return "", errors.New("a computer name is 1 to 63 characters")
	}
	return s, nil
}

// joinedName is this computer's name after joining: the one the issuer
// gave it, which has a suffix such as "-2" when the name was taken, so both
// computers agree. requested stays when the issuer's name is not usable.
func joinedName(requested, issued string) string {
	if n, err := validName(issued); err == nil {
		return n
	}
	return requested
}

// rename gives this computer a new name and tells the parts that show it.
func (a *App) rename(name string) error {
	if err := a.Config.Update(func(c *config.Config) { c.NodeName = name }); err != nil {
		return err
	}
	a.renamed(name)
	return nil
}

// renamed passes a new name to the computer list and discovery, which
// otherwise kept the old one until a restart.
func (a *App) renamed(name string) {
	if a.Nodes != nil {
		a.Nodes.SetLocalName(name)
	}
	a.restartAdvertiser()
}

// joinError gives a join failure its error code.
func joinError(err error) error {
	var refused *join.RefusedError
	var wrong *join.WrongServerError
	var unreachable *join.UnreachableError
	if code, _ := contracts.ErrorCode(err); code != "" {
		return err
	}
	switch {
	case errors.Is(err, join.ErrExpired):
		return contracts.NewError("JOIN_TOKEN_EXPIRED", nil, err)
	case errors.Is(err, join.ErrUsed):
		return contracts.NewError("JOIN_TOKEN_USED", nil, err)
	case errors.Is(err, join.ErrRevoked):
		return contracts.NewError("JOIN_TOKEN_REVOKED", nil, err)
	case errors.Is(err, join.ErrRateLimited):
		return contracts.NewError("JOIN_RATE_LIMITED", nil, err)
	case errors.Is(err, join.ErrInvalid):
		return contracts.NewError("JOIN_TOKEN_INVALID", nil, err)
	case errors.As(err, &refused) && refused.Code == "JOIN_BAD_REQUEST":
		return contracts.NewError("JOIN_BAD_REQUEST", nil, err)
	case errors.As(err, &wrong):
		return contracts.NewError("JOIN_WRONG_SERVER", map[string]any{"expected": wrong.Expected, "received": wrong.Received}, err)
	case errors.Is(err, join.ErrBadSignature):
		return contracts.NewError("JOIN_WRONG_SERVER", nil, err)
	case errors.As(err, &unreachable):
		return contracts.NewError("JOIN_UNREACHABLE", map[string]any{"server": unreachable.Server}, err)
	}
	return contracts.NewError("JOIN_FAILED", nil, err)
}

// pairedPeers are the computers this one trusts.
func (a *App) pairedPeers(ctx context.Context) []contracts.Node {
	list, err := a.Nodes.List(ctx)
	if err != nil {
		return nil
	}
	var out []contracts.Node
	for _, n := range list {
		if n.Paired && !n.IsLocal {
			out = append(out, n)
		}
	}
	return out
}

// NetworkStatus is this computer's network and the computers it trusts.
func (a *App) NetworkStatus(ctx context.Context) (api.NetworkStatus, error) {
	cfg := a.Config.Get()
	id, err := a.Settings.GetString(ctx, networkKey, "")
	if err != nil {
		return api.NetworkStatus{}, err
	}
	st := api.NetworkStatus{NetworkID: id, Reachable: a.reachable(), Peers: []api.NetworkNode{},
		Node: api.NetworkNode{ID: cfg.NodeID, Name: cfg.NodeName, Address: a.bifrostAdvertiseAddr(), Fingerprint: join.Fingerprint(a.identity.PublicKey)}}
	for _, n := range a.pairedPeers(ctx) {
		st.Peers = append(st.Peers, api.NetworkNode{ID: n.ID, Name: n.Name, Address: n.Address, Status: string(n.Status)})
	}
	return st, nil
}

// LeaveNetwork tells each paired computer this one is leaving, forgets
// them all, and clears the network. Models and settings stay.
func (a *App) LeaveNetwork(ctx context.Context) (api.LeaveResult, error) {
	res := api.LeaveResult{Left: []string{}, Unreachable: []string{}}
	peers := a.pairedPeers(ctx)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, p := range peers {
		wg.Add(1)
		go func(p contracts.Node) {
			defer wg.Done()
			told := false
			if p.Address != "" {
				cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				resp, err := nodes.NewClient("http://"+p.Address, a.identity, p.ID).Do(cctx, http.MethodPost, join.LeavePath, nil)
				if err == nil {
					_ = resp.Body.Close()
					told = resp.StatusCode < 300
				}
				a.Egress.Add(ctx, egress.PairedComputer, p.Name, "Said this computer is leaving the network")
			}
			mu.Lock()
			defer mu.Unlock()
			if told {
				res.Left = append(res.Left, p.Name)
			} else {
				res.Unreachable = append(res.Unreachable, p.Name)
			}
		}(p)
	}
	wg.Wait()
	for _, p := range peers {
		if err := a.Pairing.RevokeTrust(ctx, p.ID); err != nil {
			return res, err
		}
	}
	if err := a.Settings.Set(ctx, networkKey, ""); err != nil {
		return res, err
	}
	a.Logger.Info("left network", "told", len(res.Left), "unreachable", len(res.Unreachable))
	a.Bus.Publish(events.New(events.NodeLeft, map[string]any{"node_id": a.Config.Get().NodeID, "self": true}))
	return res, nil
}

// peerLeft forgets a paired computer that said it is leaving.
func (a *App) peerLeft(ctx context.Context, nodeID string) error {
	if err := a.Pairing.RevokeTrust(ctx, nodeID); err != nil {
		return err
	}
	a.Logger.Info("computer left the network", "node_id", nodeID)
	a.Bus.Publish(events.New(events.NodeLeft, map[string]any{"node_id": nodeID}))
	return nil
}
