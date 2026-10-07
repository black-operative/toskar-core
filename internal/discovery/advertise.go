package discovery

import (
	"fmt"
	"strconv"

	"github.com/grandcat/zeroconf"
	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/internal/version"
)

// Advertiser publishes mDNS service records.
type Advertiser struct {
	server *zeroconf.Server
}

// StartAdvertise registers _localai._tcp with node metadata.
func StartAdvertise(cfg config.Config, pairingEnabled bool) (*Advertiser, error) {
	txt := advertisedTXT(cfg, pairingEnabled)
	svc, err := zeroconf.Register(
		cfg.NodeName,
		config.ServiceType,
		config.ServiceDomain,
		cfg.InternalPort,
		txt,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("mdns advertise: %w", err)
	}
	return &Advertiser{server: svc}, nil
}

// advertisedTXT is the service's TXT record. The service's own port is
// Bifrost's, for computers pairing with each other; api_port says where the
// API is, for apps that find Yggdrasil on the network, such as the iPhone
// app, and api_lan whether it answers there or only on this computer, so
// the app can say to turn on Connect a device instead of "unreachable"
// (#216).
func advertisedTXT(cfg config.Config, pairingEnabled bool) []string {
	return []string{
		"node_id=" + cfg.NodeID,
		"name=" + cfg.NodeName,
		"version=" + version.Version,
		"pairing=" + strconv.FormatBool(pairingEnabled),
		"api_port=" + strconv.Itoa(cfg.APIPort),
		"api_lan=" + strconv.FormatBool(config.ListensBeyondLoopback(cfg.APIHost)),
	}
}

// Stop shuts down advertisement.
func (a *Advertiser) Stop() {
	if a != nil && a.server != nil {
		a.server.Shutdown()
	}
}
