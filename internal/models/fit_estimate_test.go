package models

import (
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

func gib(n float64) uint64 {
	return uint64(n * 1024 * 1024 * 1024)
}

func apple(total, available uint64) contracts.HardwareInventory {
	return contracts.HardwareInventory{
		OS:   "darwin",
		Arch: "arm64",
		Memory: contracts.MemoryInfo{
			TotalBytes:     total,
			AvailableBytes: available,
			SwapTotalBytes: gib(8),
		},
		Accelerators: []contracts.Accelerator{{
			Vendor:        "Apple",
			Model:         "Apple M-series",
			Kind:          "gpu",
			UnifiedMemory: total,
			Backends:      []string{"metal", "cpu"},
		}},
	}
}

func qwen32(size uint64) CatalogEntry {
	return CatalogEntry{
		ID:          "qwen2.5-32b-q4",
		DisplayName: "Qwen 2.5 32B",
		Variant:     "Q4_K_M",
		Parameters:  "32B",
		SizeBytes:   size,
		Context:     32768,
		Runtime:     []string{"llamacpp"},
	}
}

func TestApple24GBQwen32IsNotUnsupported(t *testing.T) {
	fit := ClassifyFit(qwen32(gib(22.4)), apple(gib(24), gib(17)), "local", "This Mac", FitOptions{})
	if fit.Label == contracts.FitUnsupported || fit.Label == contracts.FitTooLarge {
		t.Fatalf("label %s headroom %.2f low %d", fit.Label, fit.Headroom, fit.RuntimeMemoryLowBytes)
	}
	if fit.Label != contracts.FitTight && fit.Label != contracts.FitHeavy {
		t.Fatalf("expected tight or heavy, got %s (headroom %.2f)", fit.Label, fit.Headroom)
	}
	if !fit.InstallAllowed {
		t.Fatal("install should stay available")
	}
	if fit.ContextTokens != runningContextTokens {
		t.Fatalf("context %d, want running window %d", fit.ContextTokens, runningContextTokens)
	}
	if fit.MemoryKind != "unified" {
		t.Fatalf("memory kind %q", fit.MemoryKind)
	}
}

func TestApple16GBQwen32IsHeavyOrUnsupported(t *testing.T) {
	fit := ClassifyFit(qwen32(gib(22.4)), apple(gib(16), gib(10)), "local", "This Mac", FitOptions{})
	if fit.Label != contracts.FitHeavy && fit.Label != contracts.FitUnsupported {
		t.Fatalf("expected heavy or unsupported, got %s", fit.Label)
	}
	if fit.Label == contracts.FitExcellent || fit.Label == contracts.FitGood || fit.Label == contracts.FitTight {
		t.Fatal("16 GB should not look comfortable")
	}
}

func TestApple48GBQwen32IsComfortable(t *testing.T) {
	fit := ClassifyFit(qwen32(gib(22.4)), apple(gib(48), gib(40)), "local", "Studio", FitOptions{})
	if fit.Label != contracts.FitGood && fit.Label != contracts.FitExcellent {
		t.Fatalf("expected good or excellent, got %s (headroom %.2f)", fit.Label, fit.Headroom)
	}
}

func TestLongerContextFitsWorse(t *testing.T) {
	hw := apple(gib(24), gib(18))
	model := qwen32(gib(22.4))
	short := ClassifyFit(model, hw, "local", "This Mac", FitOptions{ContextTokens: 8192})
	long := ClassifyFit(model, hw, "local", "This Mac", FitOptions{ContextTokens: 32768})
	if long.RuntimeMemoryLowBytes <= short.RuntimeMemoryLowBytes {
		t.Fatalf("32k low %d should exceed 8k low %d", long.RuntimeMemoryLowBytes, short.RuntimeMemoryLowBytes)
	}
	if fitRank(long.Label) > fitRank(short.Label) {
		t.Fatalf("32k %s should not outrank 8k %s", long.Label, short.Label)
	}
	if long.Label == short.Label && long.Headroom >= short.Headroom {
		t.Fatalf("32k headroom %.2f should be worse than 8k %.2f", long.Headroom, short.Headroom)
	}
}

func TestQ4FitsBetterThanFP16(t *testing.T) {
	hw := apple(gib(48), gib(40))
	q4 := ClassifyFit(CatalogEntry{
		ID: "q4", DisplayName: "Q4", Parameters: "32B", Variant: "Q4_K_M", Runtime: []string{"llamacpp"}, Context: 8192,
	}, hw, "local", "This Mac", FitOptions{})
	fp16 := ClassifyFit(CatalogEntry{
		ID: "fp16", DisplayName: "FP16", Parameters: "32B", Variant: "FP16", Runtime: []string{"llamacpp"}, Context: 8192,
	}, hw, "local", "This Mac", FitOptions{})
	if q4.WeightBytes >= fp16.WeightBytes {
		t.Fatalf("q4 weights %d fp16 %d", q4.WeightBytes, fp16.WeightBytes)
	}
	if fitRank(q4.Label) <= fitRank(fp16.Label) {
		t.Fatalf("q4 %s should outrank fp16 %s", q4.Label, fp16.Label)
	}
	if !q4.Approximate || !fp16.Approximate {
		t.Fatal("estimates without a file size should be marked approximate")
	}
}

func TestLowFreeMemoryStillAllowsInstall(t *testing.T) {
	// Runtime sits around 19 GB. Total 24 GB can hold it. 11 GB free cannot, right now.
	fit := ClassifyFit(qwen32(gib(16)), apple(gib(24), gib(11)), "local", "This Mac", FitOptions{})
	if !fit.InstallAllowed || fit.Label == contracts.FitUnsupported {
		t.Fatalf("label %s allowed %v", fit.Label, fit.InstallAllowed)
	}
	if !strings.Contains(fit.RuntimeWarning, "Close other applications") {
		t.Fatalf("warning %q", fit.RuntimeWarning)
	}
}

func TestMeasuredRunOverridesGenericUnsupported(t *testing.T) {
	hw := apple(gib(24), gib(16))
	model := qwen32(gib(40))
	generic := ClassifyFit(model, hw, "local", "This Mac", FitOptions{})
	if generic.Label != contracts.FitUnsupported {
		t.Fatalf("generic label %s, want unsupported so the override is meaningful", generic.Label)
	}
	measured := ClassifyFit(model, hw, "local", "This Mac", FitOptions{
		Measured: map[string]MeasuredRun{
			model.ID: {TokPerSec: 26.4, PeakMemoryBytes: gib(21.8)},
		},
	})
	if measured.Label == contracts.FitUnsupported || !measured.InstallAllowed {
		t.Fatalf("measured label %s allowed %v", measured.Label, measured.InstallAllowed)
	}
	if !measured.TokPerSecMeasured || measured.EstTokPerSec != 26.4 {
		t.Fatalf("tok/s %+v", measured.EstTokPerSec)
	}
	if !strings.Contains(measured.Reason, "Measured peak memory") {
		t.Fatalf("reason %q", measured.Reason)
	}
}

func TestDiscreteGPUOffloadIsNotUnsupported(t *testing.T) {
	hw := contracts.HardwareInventory{
		Memory: contracts.MemoryInfo{TotalBytes: gib(64), AvailableBytes: gib(40)},
		Accelerators: []contracts.Accelerator{{
			Vendor:        "AMD",
			Model:         "Radeon 7900 XTX",
			Kind:          "gpu",
			DedicatedVRAM: gib(16),
			Backends:      []string{"hip", "cpu"},
		}},
	}
	fit := ClassifyFit(qwen32(gib(22)), hw, "desktop", "Desktop", FitOptions{})
	if fit.Label == contracts.FitUnsupported || fit.MemoryKind == "unified" {
		t.Fatalf("label %s kind %s", fit.Label, fit.MemoryKind)
	}
	if fit.GPUNote != "Partial GPU offload expected" {
		t.Fatalf("gpu note %q", fit.GPUNote)
	}
	if !fit.InstallAllowed {
		t.Fatal("system RAM can hold the model")
	}
}

func TestIncompatibleBackendIsUnsupported(t *testing.T) {
	model := qwen32(gib(4))
	model.Runtime = []string{"cuda"}
	fit := ClassifyFit(model, apple(gib(128), gib(100)), "local", "This Mac", FitOptions{})
	if fit.Label != contracts.FitUnsupported || fit.InstallAllowed {
		t.Fatalf("label %s allowed %v", fit.Label, fit.InstallAllowed)
	}
}

func TestProvenLoadIsNotDowngraded(t *testing.T) {
	model := qwen32(gib(40))
	fit := ClassifyFit(model, apple(gib(24), gib(12)), "local", "This Mac", FitOptions{
		Proven: map[string]bool{model.ID: true},
	})
	if fit.Label == contracts.FitUnsupported || fit.Label == contracts.FitTooLarge {
		t.Fatalf("label %s", fit.Label)
	}
	if !fit.InstallAllowed || !fit.KnownToRun {
		t.Fatalf("allowed %v known %v", fit.InstallAllowed, fit.KnownToRun)
	}
	if !strings.Contains(fit.Reason, "Known to run") {
		t.Fatalf("reason %q", fit.Reason)
	}
}

func TestLoadedModelWarnsWithoutChangingFit(t *testing.T) {
	hw := apple(gib(24), gib(8))
	alone := ClassifyFit(qwen32(gib(16)), hw, "local", "This Mac", FitOptions{})
	busy := ClassifyFit(qwen32(gib(16)), hw, "local", "This Mac", FitOptions{
		LoadedModelNames: []string{"Qwen 14B"},
	})
	if busy.Label != alone.Label {
		t.Fatalf("loaded models changed fit from %s to %s", alone.Label, busy.Label)
	}
	if !strings.Contains(busy.RuntimeWarning, "Qwen 14B") {
		t.Fatalf("warning %q", busy.RuntimeWarning)
	}
}

func fitRank(label contracts.FitLabel) int {
	switch label {
	case contracts.FitExcellent:
		return 5
	case contracts.FitGood:
		return 4
	case contracts.FitTight:
		return 3
	case contracts.FitHeavy:
		return 2
	default:
		return 1
	}
}
