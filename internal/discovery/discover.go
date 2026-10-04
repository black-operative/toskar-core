package discovery

import (
	"context"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/grandcat/zeroconf"
	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// DiscoveredNode is a node found via mDNS.
type DiscoveredNode struct {
	Node    contracts.Node
	Pairing bool
	TXT     map[string]string
}

// browseWindow is how long Discover listens for mDNS answers.
const browseWindow = 500 * time.Millisecond

// Discover browses for peer nodes for browseWindow, or until ctx is cancelled.
func Discover(ctx context.Context, bus *events.Bus, localNodeID string) ([]DiscoveredNode, error) {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return nil, err
	}
	entries := make(chan *zeroconf.ServiceEntry)
	result := make(chan []DiscoveredNode, 1)
	go func() { result <- collect(entries, bus, localNodeID) }()

	browseCtx, cancel := context.WithTimeout(ctx, browseWindow)
	defer cancel()
	// Browse returns at once and keeps listening in the background. zeroconf
	// closes entries when browseCtx ends, or straight away if the first query
	// fails, so collect always finishes and its result is safe to read.
	err = resolver.Browse(browseCtx, config.ServiceType, config.ServiceDomain, entries)
	if err != nil {
		found := <-result
		if ctx.Err() == nil {
			return found, err
		}
		return found, nil
	}
	return <-result, nil
}

// collect turns service entries into discovered nodes until entries is closed,
// skipping the local node and entries without a node ID.
func collect(entries <-chan *zeroconf.ServiceEntry, bus *events.Bus, localNodeID string) []DiscoveredNode {
	var found []DiscoveredNode
	for entry := range entries {
		meta := parseTXT(entry.Text)
		nodeID := meta["node_id"]
		if nodeID == "" || nodeID == localNodeID {
			continue
		}
		addr := ""
		if len(entry.AddrIPv4) > 0 {
			addr = entry.AddrIPv4[0].String()
		} else if len(entry.AddrIPv6) > 0 {
			addr = entry.AddrIPv6[0].String()
		}
		port := entry.Port
		if port <= 0 {
			port = config.DefaultInternalPort
		}
		if addr != "" {
			addr = net.JoinHostPort(addr, strconv.Itoa(port))
		}
		n := contracts.Node{
			ID:      nodeID,
			Name:    meta["name"],
			Status:  contracts.NodeStatusOnline,
			Paired:  false,
			Address: addr,
		}
		pairing := meta["pairing"] == "true"
		found = append(found, DiscoveredNode{Node: n, Pairing: pairing, TXT: meta})
		if bus != nil {
			bus.Publish(events.New(events.NodeDiscovered, map[string]any{
				"node_id": nodeID,
				"name":    n.Name,
				"address": addr,
			}))
		}
	}
	return found
}

func parseTXT(lines []string) map[string]string {
	out := make(map[string]string)
	for _, line := range lines {
		k, v, ok := strings.Cut(line, "=")
		if ok {
			out[k] = v
		}
	}
	return out
}
