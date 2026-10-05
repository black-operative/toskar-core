//go:build !linux && !darwin && !windows

package telemetry

import (
	"context"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

type noReader struct{}

// NewReader has no figures on this operating system.
func NewReader() Reader { return noReader{} }

func (noReader) Read(context.Context) contracts.LiveSample { return contracts.LiveSample{} }
