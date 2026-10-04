package nodes

import (
	"context"
	"time"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// HealthChecker probes node liveness.
type HealthChecker struct {
	timeout time.Duration
}

func NewHealthChecker() *HealthChecker {
	return &HealthChecker{timeout: 5 * time.Second}
}

// NewHealthCheckerWithTimeout builds a checker with a custom probe timeout.
func NewHealthCheckerWithTimeout(d time.Duration) *HealthChecker {
	if d <= 0 {
		d = 5 * time.Second
	}
	return &HealthChecker{timeout: d}
}

// CheckLocal always returns online for the local node.
func (h *HealthChecker) CheckLocal(inv contracts.HardwareInventory) contracts.NodeStatus {
	return contracts.NodeStatusOnline
}

// CheckRemote performs a health request against a remote node (via client).
func (h *HealthChecker) CheckRemote(ctx context.Context, client *Client) contracts.NodeStatus {
	status, _ := h.CheckRemoteTraining(ctx, client)
	return status
}

// CheckRemoteTraining is CheckRemote, and whether the computer is training.
func (h *HealthChecker) CheckRemoteTraining(ctx context.Context, client *Client) (contracts.NodeStatus, bool) {
	ctx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()
	info, err := client.HealthInfo(ctx)
	if err != nil {
		return contracts.NodeStatusOffline, false
	}
	return contracts.NodeStatusOnline, info.Training
}
