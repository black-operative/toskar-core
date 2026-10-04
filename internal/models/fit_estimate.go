package models

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Runtime-memory thresholds and buffers live here so the model cards and the
// recommendation path do not each invent a multiplier.
//
// Headroom is total machine capacity divided by the low runtime estimate.
// Capacity is unified memory on Apple Silicon, or system RAM everywhere else.
// Dedicated VRAM is not added on top of system RAM.
//
//	>= 1.50  excellent
//	>= 1.20  good
//	>= 0.95  tight
//	>= 0.75  heavy
//	<  0.75  unsupported
//
// Swap is reported and mentioned as a degraded fallback. It is not added to capacity.
const (
	headroomExcellent = 1.50
	headroomGood      = 1.20
	headroomTight     = 0.95
	headroomHeavy     = 0.75

	// runningContextTokens matches llamacpp.ContextWindow: the server starts
	// at 8192 unless the catalog window is smaller.
	runningContextTokens = 8192

	// Weights are memory-mapped. The low estimate is the file plus KV cache
	// and a scratch buffer. The high estimate adds runtimeOverheadFraction
	// of the weights for copies and backend buffers.
	runtimeScratchBytes     = 512 << 20
	runtimeOverheadFraction = 0.08

	// KV cache, bytes per token per billion parameters.
	// The low rate matches grouped-query attention (Qwen, Llama 3).
	// The high rate matches full multi-head attention.
	kvBytesPerTokenPerBillionLow  = 8000
	kvBytesPerTokenPerBillionHigh = 20000

	// Used only when the artifact size is missing, so the estimate is marked approximate.
	unknownQuantBytesPerParam = 0.55
)

// FitOptions carries measurements and the context window the daemon actually runs.
type FitOptions struct {
	// ContextTokens overrides the running window. Zero uses runningContextTokens,
	// capped by the catalog window when that window is smaller.
	ContextTokens    int
	Proven           map[string]bool
	Measured         map[string]MeasuredRun
	LoadedModelNames []string
	// Community is community ratings by model ID, from hardware like this
	// computer's; nil leaves recommendations to the curated order.
	Community map[string]CommunitySignal
}

// MeasuredRun is a prior successful load or benchmark on this machine.
type MeasuredRun struct {
	TokPerSec       float64
	PeakMemoryBytes uint64
	UsedSwap        bool
}

// ClassifyFit estimates runtime memory and grades one model against one machine.
func ClassifyFit(e CatalogEntry, hw contracts.HardwareInventory, nodeID, nodeName string, opts FitOptions) contracts.ModelFit {
	fit := contracts.ModelFit{
		ModelID:        e.ID,
		NodeID:         nodeID,
		NodeName:       nodeName,
		Quantization:   strings.TrimSpace(e.Variant),
		InstallAllowed: true,
	}
	if !backendSupported(e, hw) {
		fit.Label = contracts.FitUnsupported
		fit.Reason = "This model is not compatible with this machine."
		fit.InstallAllowed = false
		return fit
	}

	capacity, kind := machineCapacity(hw)
	fit.TotalMemoryBytes = capacity
	fit.AvailableMemoryBytes = hw.Memory.AvailableBytes
	fit.MemoryKind = kind
	fit.ContextTokens = configuredContext(e.Context, opts.ContextTokens)

	weights, approx := weightBytes(e)
	fit.WeightBytes = weights
	fit.Approximate = approx
	if weights == 0 {
		fit.Label = contracts.FitGood
		fit.Reason = "Memory requirement unknown; treat as provisional."
		return fit
	}
	if capacity == 0 {
		fit.Label = contracts.FitTight
		fit.Reason = "Could not measure this computer's memory."
		fit.ExpectedMemoryBytes = weights
		return fit
	}

	billions := parameterBillions(e.Parameters)
	if billions <= 0 {
		bpp := quantBytesPerParam(e.Variant)
		billions = float64(weights) / (bpp * 1e9)
		fit.Approximate = true
	}
	ctx := float64(fit.ContextTokens)
	kvLow := uint64(billions * ctx * kvBytesPerTokenPerBillionLow)
	kvHigh := uint64(billions * ctx * kvBytesPerTokenPerBillionHigh)
	low := weights + kvLow + runtimeScratchBytes
	high := weights + kvHigh + runtimeScratchBytes + uint64(float64(weights)*runtimeOverheadFraction)
	if high < low {
		high = low
	}

	measured, hasMeasured := opts.Measured[e.ID]
	if hasMeasured && measured.PeakMemoryBytes > 0 {
		low = measured.PeakMemoryBytes
		high = measured.PeakMemoryBytes
		fit.Approximate = false
	}

	fit.RuntimeMemoryLowBytes = low
	fit.RuntimeMemoryHighBytes = high
	fit.ExpectedMemoryBytes = low
	fit.Headroom = float64(capacity) / float64(low)
	fit.Label = labelForHeadroom(fit.Headroom)
	fit.Reason = reasonForLabel(fit.Label)
	fit.EstTokPerSec = estimateTokPerSec(e, hw, low, capacity)
	fit.GPUNote = gpuNote(hw, low)
	fit.InstallAllowed = fit.Label != contracts.FitUnsupported
	fit.Recommendations = recommendationsFor(fit.Label, kind)

	if avail := hw.Memory.AvailableBytes; avail > 0 && avail < low && fit.InstallAllowed {
		fit.RuntimeWarning = "Close other applications before running."
	}
	if others := otherLoaded(e, opts.LoadedModelNames); len(others) > 0 && hw.Memory.AvailableBytes > 0 && hw.Memory.AvailableBytes < low {
		fit.RuntimeWarning = fmt.Sprintf("Insufficient free memory while %s is loaded.", strings.Join(others, ", "))
	}

	proven := opts.Proven[e.ID] || (hasMeasured && (measured.TokPerSec > 0 || measured.PeakMemoryBytes > 0))
	if proven && fit.Label == contracts.FitUnsupported {
		fit.Label = contracts.FitHeavy
		fit.InstallAllowed = true
		fit.KnownToRun = true
		if hasMeasured && measured.UsedSwap {
			fit.Reason = "Known to run with swap."
		} else {
			fit.Reason = "Known to run on this machine."
		}
		fit.Recommendations = recommendationsFor(fit.Label, kind)
	} else if proven {
		fit.KnownToRun = true
		if hasMeasured && measured.PeakMemoryBytes > 0 {
			fit.Reason = fmt.Sprintf("Measured peak memory: %s. Previously ran successfully on this machine.", formatGiB(measured.PeakMemoryBytes))
		} else if hasMeasured && measured.UsedSwap {
			fit.Reason = "Known to run with swap."
		} else if opts.Proven[e.ID] && fit.Label == contracts.FitHeavy {
			fit.Reason = "Known to run on this machine."
		}
	}
	if hasMeasured && measured.TokPerSec > 0 {
		fit.EstTokPerSec = measured.TokPerSec
		fit.TokPerSecMeasured = true
	}
	return fit
}

func labelForHeadroom(headroom float64) contracts.FitLabel {
	switch {
	case headroom >= headroomExcellent:
		return contracts.FitExcellent
	case headroom >= headroomGood:
		return contracts.FitGood
	case headroom >= headroomTight:
		return contracts.FitTight
	case headroom >= headroomHeavy:
		return contracts.FitHeavy
	default:
		return contracts.FitUnsupported
	}
}

func reasonForLabel(label contracts.FitLabel) string {
	switch label {
	case contracts.FitExcellent:
		return "Plenty of memory headroom."
	case contracts.FitGood:
		return "Should run comfortably."
	case contracts.FitTight:
		return "Should run, but memory headroom is limited."
	case contracts.FitHeavy:
		return "May require swap or reduced context."
	default:
		return "This model is not compatible with this machine."
	}
}

func recommendationsFor(label contracts.FitLabel, kind string) []string {
	if label != contracts.FitTight && label != contracts.FitHeavy {
		return nil
	}
	out := []string{
		"Close memory-heavy apps",
		"Reduce context length",
		"Avoid running multiple large models at once",
	}
	if kind == "unified" && label == contracts.FitHeavy {
		out = append([]string{"Expect macOS to use swap"}, out...)
	}
	return out
}

func configuredContext(catalog, override int) int {
	if override > 0 {
		return override
	}
	if catalog <= 0 || catalog > runningContextTokens {
		return runningContextTokens
	}
	return catalog
}

func machineCapacity(hw contracts.HardwareInventory) (uint64, string) {
	var unified uint64
	var dedicated uint64
	for _, a := range hw.Accelerators {
		if a.UnifiedMemory > 0 && a.DedicatedVRAM == 0 {
			if a.UnifiedMemory > unified {
				unified = a.UnifiedMemory
			}
		}
		if a.DedicatedVRAM > 0 {
			dedicated += a.DedicatedVRAM
		}
	}
	// Unified memory is the same pool as system RAM. Do not add them.
	if unified > 0 {
		capacity := unified
		if hw.Memory.TotalBytes > capacity {
			capacity = hw.Memory.TotalBytes
		}
		return capacity, "unified"
	}
	if hw.Memory.TotalBytes > 0 {
		return hw.Memory.TotalBytes, "system"
	}
	if dedicated > 0 {
		return dedicated, "system"
	}
	return 0, ""
}

func gpuNote(hw contracts.HardwareInventory, runtimeLow uint64) string {
	var dedicated uint64
	offload := false
	for _, a := range hw.Accelerators {
		dedicated += a.DedicatedVRAM
		for _, b := range a.Backends {
			switch strings.ToLower(b) {
			case "cuda", "hip", "rocm", "metal", "cpu", "vulkan":
				offload = true
			}
		}
	}
	if dedicated == 0 || runtimeLow == 0 {
		return ""
	}
	if dedicated >= runtimeLow {
		return "Good GPU fit"
	}
	if offload {
		return "Partial GPU offload expected"
	}
	return ""
}

func backendSupported(e CatalogEntry, hw contracts.HardwareInventory) bool {
	if len(e.Runtime) == 0 {
		return true
	}
	for _, rt := range e.Runtime {
		switch strings.ToLower(strings.TrimSpace(rt)) {
		case "", "llamacpp", "cpu", "gguf":
			return true
		case "metal":
			if hasBackend(hw, "metal") {
				return true
			}
		case "cuda":
			if hasBackend(hw, "cuda") {
				return true
			}
		case "hip", "rocm":
			if hasBackend(hw, "hip") || hasBackend(hw, "rocm") {
				return true
			}
		}
	}
	return false
}

func hasBackend(hw contracts.HardwareInventory, name string) bool {
	for _, a := range hw.Accelerators {
		for _, b := range a.Backends {
			if strings.EqualFold(b, name) {
				return true
			}
		}
	}
	return false
}

func weightBytes(e CatalogEntry) (uint64, bool) {
	if e.SizeBytes > 0 {
		return e.SizeBytes, false
	}
	billions := parameterBillions(e.Parameters)
	if billions > 0 {
		return uint64(billions * 1e9 * quantBytesPerParam(e.Variant)), true
	}
	if e.MemoryNeededBytes > 0 {
		return e.MemoryNeededBytes, true
	}
	return 0, true
}

func quantBytesPerParam(variant string) float64 {
	v := strings.ToUpper(variant)
	switch {
	case strings.Contains(v, "Q2"):
		return 0.35
	case strings.Contains(v, "Q3"):
		return 0.40
	case strings.Contains(v, "Q4"):
		return 0.55
	case strings.Contains(v, "Q5"):
		return 0.65
	case strings.Contains(v, "Q6"):
		return 0.80
	case strings.Contains(v, "Q8"):
		return 1.05
	case strings.Contains(v, "FP16"), strings.Contains(v, "F16"):
		return 2.0
	case strings.Contains(v, "FP32"), strings.Contains(v, "F32"):
		return 4.0
	default:
		return unknownQuantBytesPerParam
	}
}

func parameterBillions(parameters string) float64 {
	s := strings.TrimSpace(strings.ToUpper(parameters))
	s = strings.TrimSuffix(s, "B")
	if s == "" || strings.ContainsAny(s, "X ") {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f <= 0 {
		return 0
	}
	return f
}

func otherLoaded(e CatalogEntry, names []string) []string {
	var out []string
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if strings.EqualFold(name, e.DisplayName) || strings.EqualFold(name, e.ID) {
			continue
		}
		out = append(out, name)
	}
	return out
}

func formatGiB(n uint64) string {
	return fmt.Sprintf("%.1f GB", float64(n)/(1024*1024*1024))
}

func runnableFit(label contracts.FitLabel) bool {
	return label != contracts.FitUnsupported && label != contracts.FitTooLarge
}
