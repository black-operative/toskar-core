package discovery

import (
	"net"
	"strconv"
	"testing"

	"github.com/grandcat/zeroconf"
	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/internal/events"
)

func TestCollect(t *testing.T) {
	bus := events.NewBus(8)
	_, sub := bus.Subscribe()

	entries := make(chan *zeroconf.ServiceEntry)
	result := make(chan []DiscoveredNode, 1)
	go func() { result <- collect(entries, bus, "local") }()

	entries <- &zeroconf.ServiceEntry{Text: []string{"node_id=local"}, AddrIPv4: []net.IP{net.ParseIP("10.0.0.1")}}
	entries <- &zeroconf.ServiceEntry{Text: []string{"name=no-id"}}
	entries <- &zeroconf.ServiceEntry{
		Text:     []string{"node_id=peer-1", "name=Peer One", "pairing=true"},
		AddrIPv4: []net.IP{net.ParseIP("10.0.0.2")},
		Port:     9000,
	}
	entries <- &zeroconf.ServiceEntry{
		Text:     []string{"node_id=peer-2"},
		AddrIPv6: []net.IP{net.ParseIP("fe80::1")},
	}
	close(entries)

	found := <-result
	if len(found) != 2 {
		t.Fatalf("found %d nodes, want 2: %+v", len(found), found)
	}
	if n := found[0]; n.Node.ID != "peer-1" || n.Node.Name != "Peer One" || !n.Pairing || n.Node.Address != "10.0.0.2:9000" {
		t.Errorf("peer-1 = %+v", n)
	}
	want := net.JoinHostPort("fe80::1", strconv.Itoa(config.DefaultInternalPort))
	if n := found[1]; n.Node.ID != "peer-2" || n.Pairing || n.Node.Address != want {
		t.Errorf("peer-2 = %+v, want address %s", n, want)
	}

	for _, id := range []string{"peer-1", "peer-2"} {
		ev := <-sub
		if ev.Type != events.NodeDiscovered || ev.Payload["node_id"] != id {
			t.Errorf("event = %+v, want %s for %s", ev, events.NodeDiscovered, id)
		}
	}
}
