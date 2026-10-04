package nodes

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/yggdrasil-core/internal/auth"
	"github.com/yeixio/yggdrasil-core/internal/discovery"
	"github.com/yeixio/yggdrasil-core/internal/events"
	"github.com/yeixio/yggdrasil-core/pkg/contracts"
)

// Manager tracks local, discovered, and paired nodes.
type Manager struct {
	db            *sql.DB
	bus           *events.Bus
	pairing       *auth.PairingManager
	localNodeID   string
	nameMu        sync.RWMutex
	localName     string
	advertiseAddr func() string // reachable host:port for this node
	staticPeers   []string
	hardware      func(ctx context.Context) (contracts.HardwareInventory, error)

	mu         sync.RWMutex
	discovered map[string]discovery.DiscoveredNode
	health     *HealthChecker

	livenessMu sync.Mutex
	livenessAt time.Time

	// trainingMu guards training: the paired computers whose last health
	// answer said they were training. localTraining says so for this one.
	trainingMu    sync.Mutex
	training      map[string]bool
	localTraining func() bool
}

// Liveness timing for the chat path. A background loop refreshes every
// LivenessInterval; placement reuses a result younger than livenessMaxAge
// and otherwise probes with a short deadline, so an offline paired computer
// costs a chat at most livenessProbeCap instead of a full HTTP timeout.
const (
	LivenessInterval = 10 * time.Second
	livenessMaxAge   = 20 * time.Second
	livenessProbeCap = 1500 * time.Millisecond
)

// RefreshPairedLivenessIfStale probes paired computers only when the last
// probe is older than livenessMaxAge, and caps that probe's wait.
func (m *Manager) RefreshPairedLivenessIfStale(ctx context.Context) {
	m.livenessMu.Lock()
	fresh := time.Since(m.livenessAt) < livenessMaxAge
	m.livenessMu.Unlock()
	if fresh {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, livenessProbeCap)
	defer cancel()
	m.RefreshPairedLiveness(ctx)
}

func NewManager(db *sql.DB, bus *events.Bus, pairing *auth.PairingManager, localNodeID, localName string, hw func(context.Context) (contracts.HardwareInventory, error)) *Manager {
	return &Manager{
		db:          db,
		bus:         bus,
		pairing:     pairing,
		localNodeID: localNodeID,
		localName:   localName,
		hardware:    hw,
		discovered:  make(map[string]discovery.DiscoveredNode),
		health:      NewHealthCheckerWithTimeout(2 * time.Second),
	}
}

// SetAdvertiseAddr sets a callback returning this node's Bifrost listen address for peers.
func (m *Manager) SetAdvertiseAddr(fn func() string) {
	m.advertiseAddr = fn
}

// SetStaticPeers configures host:port peers probed when mDNS is unavailable.
func (m *Manager) SetStaticPeers(peers []string) {
	m.staticPeers = append([]string(nil), peers...)
}

// SetLocalName renames this computer, as other computers will see it.
func (m *Manager) SetLocalName(name string) {
	m.nameMu.Lock()
	defer m.nameMu.Unlock()
	m.localName = name
}

func (m *Manager) name() string {
	m.nameMu.RLock()
	defer m.nameMu.RUnlock()
	return m.localName
}

// SetLocalTraining tells the manager how to know this computer is training.
func (m *Manager) SetLocalTraining(fn func() bool) { m.localTraining = fn }

// TrainingNodes are the computers training now, by node ID: this one, and
// paired computers as of their last health probe.
func (m *Manager) TrainingNodes() map[string]bool {
	m.trainingMu.Lock()
	out := make(map[string]bool, len(m.training)+1)
	for id, t := range m.training {
		if t {
			out[id] = true
		}
	}
	m.trainingMu.Unlock()
	if m.localTraining != nil && m.localTraining() {
		out[m.localNodeID] = true
	}
	return out
}

// Pairing returns the pairing manager.
func (m *Manager) Pairing() *auth.PairingManager { return m.pairing }

// List returns local + paired + discovered nodes.
func (m *Manager) List(ctx context.Context) ([]contracts.Node, error) {
	inv, _ := m.hardware(ctx)
	now := time.Now().UTC()
	local := contracts.Node{
		ID:         m.localNodeID,
		Name:       m.name(),
		OS:         inv.OS,
		Arch:       inv.Arch,
		Status:     contracts.NodeStatusOnline,
		IsLocal:    true,
		Paired:     true,
		Hardware:   &inv,
		LastSeenAt: &now,
	}
	if m.advertiseAddr != nil {
		local.Address = m.advertiseAddr()
	}
	training := m.TrainingNodes()
	local.Training = training[local.ID]

	paired, err := m.loadPaired(ctx)
	if err != nil {
		return nil, err
	}

	m.mu.RLock()
	disc := make([]discovery.DiscoveredNode, 0, len(m.discovered))
	for _, d := range m.discovered {
		disc = append(disc, d)
	}
	m.mu.RUnlock()

	// Merge discovery addresses onto paired rows.
	discByID := map[string]discovery.DiscoveredNode{}
	for _, d := range disc {
		discByID[d.Node.ID] = d
	}
	for i := range paired {
		if d, ok := discByID[paired[i].ID]; ok && d.Node.Address != "" {
			paired[i].Address = d.Node.Address
			_ = m.pairing.SetNodeAddress(ctx, paired[i].ID, d.Node.Address)
		}
		if paired[i].Address == "" {
			paired[i].Status = contracts.NodeStatusOffline
		}
		// Keep persisted status (online/offline) from last liveness probe; discovery
		// alone must not force "online" — RefreshPairedLiveness owns that.
		paired[i].Training = training[paired[i].ID] && paired[i].Status == contracts.NodeStatusOnline
	}

	seen := map[string]bool{local.ID: true}
	out := []contracts.Node{local}
	for _, p := range paired {
		if !seen[p.ID] {
			out = append(out, p)
			seen[p.ID] = true
		}
	}
	for _, d := range disc {
		if seen[d.Node.ID] {
			continue
		}
		out = append(out, d.Node)
	}
	return out, nil
}

// RefreshPairedLiveness probes each paired remote over Bifrost health and updates
// persisted online/offline status. Call before placement so offline pins fail clearly
// and automatic placement can skip dead peers.
func (m *Manager) RefreshPairedLiveness(ctx context.Context) {
	if m.health == nil || m.pairing == nil {
		return
	}
	defer func() {
		m.livenessMu.Lock()
		m.livenessAt = time.Now()
		m.livenessMu.Unlock()
	}()
	paired, err := m.loadPaired(ctx)
	if err != nil || len(paired) == 0 {
		return
	}
	m.mu.RLock()
	disc := make(map[string]string, len(m.discovered))
	for id, d := range m.discovered {
		if d.Node.Address != "" {
			disc[id] = d.Node.Address
		}
	}
	m.mu.RUnlock()

	type result struct {
		id, name, addr string
		prev, next     contracts.NodeStatus
		training       bool
	}
	results := make([]result, len(paired))
	var wg sync.WaitGroup
	for i, n := range paired {
		wg.Add(1)
		go func(i int, n contracts.Node) {
			defer wg.Done()
			addr := n.Address
			if a, ok := disc[n.ID]; ok && a != "" {
				addr = a
			}
			res := result{id: n.ID, name: n.Name, addr: addr, prev: n.Status}
			if addr == "" {
				res.next = contracts.NodeStatusOffline
			} else {
				client := NewProbeClient(normalizeHTTP(ensureHostPort(addr, 7332)))
				res.next, res.training = m.health.CheckRemoteTraining(ctx, client)
			}
			results[i] = res
		}(i, n)
	}
	wg.Wait()

	// Record results even when the probe deadline has passed; a capped probe
	// that finds a peer offline must still say so.
	ctx = context.WithoutCancel(ctx)
	m.trainingMu.Lock()
	if m.training == nil {
		m.training = map[string]bool{}
	}
	for _, res := range results {
		if res.id != "" {
			m.training[res.id] = res.training
		}
	}
	m.trainingMu.Unlock()
	for _, res := range results {
		if res.id == "" {
			continue
		}
		if res.next == contracts.NodeStatusOnline && res.addr != "" {
			_ = m.pairing.SetNodeAddress(ctx, res.id, res.addr)
		}
		_ = m.pairing.SetNodeStatus(ctx, res.id, string(res.next))
		if m.bus != nil && res.prev != res.next {
			evt := events.NodeOnline
			if res.next == contracts.NodeStatusOffline {
				evt = events.NodeOffline
			}
			m.bus.Publish(events.New(evt, map[string]any{
				"node_id": res.id,
				"name":    res.name,
				"status":  string(res.next),
			}))
		}
	}
}

func (m *Manager) loadPaired(ctx context.Context) ([]contracts.Node, error) {
	rows, err := m.db.QueryContext(ctx, `
		SELECT n.id, n.name, n.os, n.arch, n.status, n.last_seen_at, n.hardware_json, COALESCE(n.address,'')
		FROM nodes n
		JOIN node_trust t ON t.node_id = n.id
		WHERE t.revoked_at IS NULL AND n.is_local = 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []contracts.Node
	for rows.Next() {
		var n contracts.Node
		var osName, arch, status, lastSeen, hwJSON, address sql.NullString
		if err := rows.Scan(&n.ID, &n.Name, &osName, &arch, &status, &lastSeen, &hwJSON, &address); err != nil {
			return nil, err
		}
		n.OS = osName.String
		n.Arch = arch.String
		n.Status = contracts.NodeStatus(status.String)
		n.Paired = true
		n.Address = address.String
		if lastSeen.Valid {
			t, _ := time.Parse(time.RFC3339, lastSeen.String)
			n.LastSeenAt = &t
		}
		if hwJSON.Valid && hwJSON.String != "" {
			var inv contracts.HardwareInventory
			_ = json.Unmarshal([]byte(hwJSON.String), &inv)
			n.Hardware = &inv
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// RefreshDiscovery runs mDNS browse plus static peer probes and updates cache.
// When StaticPeers are configured, mDNS is skipped (Docker bridge has no multicast).
func (m *Manager) RefreshDiscovery(ctx context.Context) error {
	var found []discovery.DiscoveredNode
	var err error
	if len(m.staticPeers) == 0 {
		found, err = discovery.Discover(ctx, m.bus, m.localNodeID)
	}
	static := discovery.ProbeStaticPeers(ctx, m.staticPeers, m.localNodeID)
	if err != nil && len(found) == 0 && len(static) == 0 {
		return err
	}
	m.mu.Lock()
	for _, d := range found {
		m.discovered[d.Node.ID] = d
		if d.Node.Address != "" {
			_ = m.pairing.SetNodeAddress(ctx, d.Node.ID, d.Node.Address)
		}
	}
	for _, d := range static {
		m.discovered[d.Node.ID] = d
		if d.Node.Address != "" {
			_ = m.pairing.SetNodeAddress(ctx, d.Node.ID, d.Node.Address)
		}
	}
	m.mu.Unlock()
	return nil
}

// StartPairing begins pairing with a discovered node and sends an offer.
func (m *Manager) StartPairing(remoteNodeID string) (*auth.PairingSession, error) {
	m.mu.RLock()
	d, ok := m.discovered[remoteNodeID]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("node %q is not in the discovery list — tap Refresh and try again", remoteNodeID)
	}
	if d.Node.Address == "" {
		return nil, fmt.Errorf("node %q has no network address yet — wait for discovery or tap Refresh", firstNonEmpty(d.Node.Name, remoteNodeID))
	}

	peerAddr := ensureHostPort(d.Node.Address, 7332)
	client := NewProbeClient(normalizeHTTP(peerAddr))
	info, err := client.NodeInfo(context.Background())
	if err != nil {
		return nil, fmt.Errorf(
			"cannot reach %s at %s (Bifrost). On that Mac: allow Local Network / Firewall for Toskar, then retry. Detail: %w",
			firstNonEmpty(d.Node.Name, remoteNodeID), peerAddr, err,
		)
	}
	cert := []byte(info.CertPEM)
	if len(cert) == 0 {
		return nil, fmt.Errorf("peer at %s returned an empty certificate", peerAddr)
	}
	if info.ID != "" && info.ID != remoteNodeID {
		return nil, fmt.Errorf("the computer at %s is not %s any more — tap Refresh and try again", peerAddr, firstNonEmpty(d.Node.Name, remoteNodeID))
	}

	session, err := m.pairing.StartPairing(remoteNodeID, firstNonEmpty(info.Name, d.Node.Name), peerAddr, cert)
	if err != nil {
		return nil, err
	}

	offer := m.offerFor(session)

	delivered := false
	if err := client.SendPairingOffer(context.Background(), offer); err == nil {
		delivered = true
	}
	if apiBase := controlAPIBase(peerAddr); apiBase != "" {
		if err := NewProbeClient(apiBase).SendControlPairingOffer(context.Background(), offer); err == nil {
			delivered = true
		}
	}
	if !delivered {
		// Keep the local session so the other Mac can claim by code.
		return session, nil
	}
	return session, nil
}

// ClaimPairing pulls an outbound offer from a peer by code and stores it as incoming.
func (m *Manager) ClaimPairing(ctx context.Context, remoteNodeID, code string) (*auth.PairingSession, error) {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return nil, fmt.Errorf("enter the 6-digit code shown on the other computer")
	}
	m.mu.RLock()
	d, ok := m.discovered[remoteNodeID]
	m.mu.RUnlock()
	addr := ""
	if ok {
		addr = d.Node.Address
	}
	if addr == "" {
		if n, err := m.Get(ctx, remoteNodeID); err == nil {
			addr = n.Address
		}
	}
	if addr == "" {
		return nil, fmt.Errorf("no address for that computer — tap Refresh and try again")
	}
	client := NewProbeClient(normalizeHTTP(ensureHostPort(addr, 7332)))
	offer, err := client.FetchOutboundOffer(ctx, code)
	if err != nil {
		// Fallback: control API on :7331 (only works if that peer exposed LAN API).
		err2 := err
		if apiBase := controlAPIBase(addr); apiBase != "" {
			offer, err2 = NewProbeClient(apiBase).FetchOutboundOfferControl(ctx, code)
		}
		if err2 != nil {
			return nil, fmt.Errorf(
				"could not reach Bifrost on %s (%v). On that Mac: fully quit Toskar (Cmd+Q), reopen it, keep Settings → Find other computers On, and allow Local Network for Toskar",
				ensureHostPort(addr, 7332), err,
			)
		}
	}
	// The offer must be the one that computer made for this code.
	if offer.FromNodeID != remoteNodeID || offer.Code != code {
		return nil, fmt.Errorf("that computer answered with a different pairing — start pairing again")
	}
	return m.pairing.ReceiveOffer(offer)
}

// LookupOutbound returns the signed offer for a pending outbound session, for
// a peer that claims it by code. source is the caller's address.
func (m *Manager) LookupOutbound(source, code string) (auth.PairingOffer, error) {
	session, err := m.pairing.OutboundByCode(source, code)
	if err != nil {
		return auth.PairingOffer{}, err
	}
	return m.offerFor(session), nil
}

// offerFor is this computer's signed offer for an outbound session.
func (m *Manager) offerFor(session *auth.PairingSession) auth.PairingOffer {
	fromAddr := ""
	if m.advertiseAddr != nil {
		fromAddr = m.advertiseAddr()
	}
	offer := auth.PairingOffer{
		SessionID:   session.ID,
		FromNodeID:  m.localNodeID,
		ToNodeID:    session.RemoteNodeID,
		FromName:    firstNonEmpty(m.name(), m.localNodeID),
		FromCertPEM: string(m.pairing.Identity().CertPEM),
		FromAddress: fromAddr,
		Code:        session.Code,
		ExpiresAt:   session.ExpiresAt.Format(time.RFC3339),
	}
	m.pairing.SignOffer(&offer)
	return offer
}

// ApprovePairing approves by session or code and completes mutual trust when incoming.
func (m *Manager) ApprovePairing(ctx context.Context, sessionID, code string) (*auth.PairingSession, error) {
	var session *auth.PairingSession
	var err error
	if code != "" {
		session, err = m.pairing.ApproveByCode(ctx, code)
	} else {
		session, err = m.pairing.ApproveSession(ctx, sessionID)
	}
	if err != nil {
		return nil, err
	}

	// If this was an incoming offer, notify the initiator.
	incoming, ok := m.pairing.GetIncoming(session.ID)
	if !ok && session.Incoming {
		incoming = session
		ok = true
	}
	if ok && incoming.RemoteAddr != "" {
		fromAddr := ""
		if m.advertiseAddr != nil {
			fromAddr = m.advertiseAddr()
		}
		complete := auth.PairingComplete{
			SessionID:   session.ID,
			FromNodeID:  m.localNodeID,
			FromName:    m.name(),
			FromCertPEM: string(m.pairing.Identity().CertPEM),
			FromAddress: fromAddr,
		}
		m.pairing.SignComplete(&complete, incoming.RemoteNodeID, incoming.Code)
		_ = NewProbeClient(normalizeHTTP(incoming.RemoteAddr)).CompletePairing(ctx, complete)
	}
	return session, nil
}

// ReceiveOffer stores a remote pairing offer.
func (m *Manager) ReceiveOffer(offer auth.PairingOffer) (*auth.PairingSession, error) {
	return m.pairing.ReceiveOffer(offer)
}

// CompletePairing finalizes an outbound session after peer approval.
// source is the caller's address.
func (m *Manager) CompletePairing(ctx context.Context, source string, complete auth.PairingComplete) (*auth.PairingSession, error) {
	return m.pairing.CompleteFromPeer(ctx, source, complete)
}

// ListIncomingOffers returns pending pairing requests.
func (m *Manager) ListIncomingOffers() []auth.PairingSession {
	return m.pairing.ListIncoming()
}

// Revoke removes trust for a node.
func (m *Manager) Revoke(ctx context.Context, nodeID string) error {
	if nodeID == m.localNodeID {
		return fmt.Errorf("cannot revoke local node")
	}
	return m.pairing.RevokeTrust(ctx, nodeID)
}

// Get returns a node by ID from List.
func (m *Manager) Get(ctx context.Context, nodeID string) (contracts.Node, error) {
	list, err := m.List(ctx)
	if err != nil {
		return contracts.Node{}, err
	}
	for _, n := range list {
		if n.ID == nodeID {
			return n, nil
		}
	}
	return contracts.Node{}, fmt.Errorf("node %q not found", nodeID)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func normalizeHTTP(addr string) string {
	if len(addr) >= 7 && (addr[:7] == "http://" || (len(addr) >= 8 && addr[:8] == "https://")) {
		return addr
	}
	return "http://" + addr
}

func ensureHostPort(addr string, defaultPort int) string {
	addr = strings.TrimSpace(addr)
	addr = strings.TrimPrefix(strings.TrimPrefix(addr, "http://"), "https://")
	if addr == "" {
		return addr
	}
	if _, _, err := net.SplitHostPort(addr); err == nil {
		return addr
	}
	// Bare IPv6 without port is rare in our discovery path; treat as host.
	return net.JoinHostPort(addr, strconv.Itoa(defaultPort))
}

func controlAPIBase(internalAddr string) string {
	hostPort := ensureHostPort(strings.TrimPrefix(strings.TrimPrefix(internalAddr, "http://"), "https://"), 7332)
	host, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		return ""
	}
	apiPort := "7331"
	if port != "7332" && port != "7331" {
		apiPort = "7331"
	}
	return "http://" + net.JoinHostPort(host, apiPort)
}
