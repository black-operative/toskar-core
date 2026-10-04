package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// ProbeStaticPeers fetches /internal/v1/node from each host:port and returns discovered nodes.
// Used when mDNS cannot cross Docker bridge networks.
func ProbeStaticPeers(ctx context.Context, peers []string, localNodeID string) []DiscoveredNode {
	if len(peers) == 0 {
		return nil
	}
	client := &http.Client{Timeout: 3 * time.Second}
	var out []DiscoveredNode
	for _, peer := range peers {
		peer = strings.TrimSpace(peer)
		if peer == "" {
			continue
		}
		base := peer
		if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
			base = "http://" + base
		}
		base = strings.TrimRight(base, "/")
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/internal/v1/node", nil)
		if err != nil {
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		var info struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		err = json.NewDecoder(resp.Body).Decode(&info)
		_ = resp.Body.Close()
		if err != nil || info.ID == "" || info.ID == localNodeID {
			continue
		}
		addr := strings.TrimPrefix(strings.TrimPrefix(base, "https://"), "http://")
		out = append(out, DiscoveredNode{
			Node: contracts.Node{
				ID:      info.ID,
				Name:    firstNonEmpty(info.Name, info.ID),
				Status:  contracts.NodeStatusOnline,
				Address: addr,
			},
			Pairing: true,
			TXT:     map[string]string{"source": "static"},
		})
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// FormatPeerAddr documents expected peer address form.
func FormatPeerAddr(host string, port int) string {
	return fmt.Sprintf("%s:%d", host, port)
}
