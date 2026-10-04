package training

import (
	"fmt"
	"math"
	"strings"

	"github.com/yeixio/toskar-core/internal/models"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// PresetSettings fills Hyper for a preset before fit adjustments. Iterations
// follow the number of examples, with a floor so a small dataset still sees
// each example several times.
func PresetSettings(p Preset, info models.TrainingInfo, st DatasetStats) Hyper {
	h := Hyper{Scale: 20, BatchSize: 4, Seed: 7, LearningRate: 1e-5}
	minIters := 0
	switch p {
	case PresetQuick:
		h.Epochs, h.Rank, h.Layers, h.LearningRate, minIters = 3, 8, 8, 2e-5, 30
	case PresetQuality:
		h.Epochs, h.Rank, h.Layers, minIters = 6, 32, -1, 100
	default:
		h.Epochs, h.Rank, h.Layers, minIters = 4, 16, 16, 60
	}
	if h.Layers > info.Layers || h.Layers < 0 {
		h.Layers = info.Layers
	}
	h.MaxSeqLength = seqLength(st.P95Tokens)
	h.Method = MethodLoRA
	// Quick favours the smaller download once the full weights pass 3 GB.
	if p == PresetQuick && info.QuantizedRepo != "" && info.BaseBytes > 3<<30 {
		h.Method = MethodQLoRA
	}
	h.Iters = iterations(h, st.Usable, minIters)
	return h
}

func iterations(h Hyper, examples, minIters int) int {
	trainN := examples - max(1, examples/10)
	if trainN < 1 {
		trainN = max(examples, 1)
	}
	batch := min(h.BatchSize, trainN)
	if batch < 1 {
		batch = 1
	}
	iters := int(math.Ceil(float64(h.Epochs*trainN) / float64(batch)))
	// Small datasets get at least minIters, but never more than 12 passes.
	floor := min(minIters, int(math.Ceil(12*float64(trainN)/float64(batch))))
	return max(iters, floor, 1)
}

func seqLength(p95 int) int {
	n := int(float64(p95)*1.15) + 32
	n = (n + 63) / 64 * 64
	return min(max(n, 256), 2048)
}

// ApplyAdvanced overlays the user's Advanced settings on a preset.
func ApplyAdvanced(h Hyper, adv *Hyper, examples int) Hyper {
	if adv == nil {
		return h
	}
	if adv.Method != "" {
		h.Method = adv.Method
	}
	if adv.Rank > 0 {
		h.Rank = adv.Rank
	}
	if adv.Scale > 0 {
		h.Scale = adv.Scale
	}
	if adv.Layers != 0 {
		h.Layers = adv.Layers
	}
	if adv.LearningRate > 0 {
		h.LearningRate = adv.LearningRate
	}
	if adv.BatchSize > 0 {
		h.BatchSize = adv.BatchSize
	}
	if adv.MaxSeqLength > 0 {
		h.MaxSeqLength = adv.MaxSeqLength
	}
	if adv.GradCheckpoint {
		h.GradCheckpoint = true
	}
	if adv.Epochs > 0 {
		h.Epochs = adv.Epochs
		h.Iters = iterations(h, examples, 0)
	}
	if adv.Iters > 0 {
		h.Iters = adv.Iters
	}
	return h
}

// FitLabel grades a training fit.
type FitLabel string

const (
	FitComfortable FitLabel = "comfortable"
	FitTight       FitLabel = "tight"
	FitTooLarge    FitLabel = "too_large"
	FitUnsupported FitLabel = "unsupported"
)

// NodeFit is the training estimate for one computer. It is separate from the
// inference fit: training holds optimizer state and activations the chat
// runtime never needs.
type NodeFit struct {
	NodeID   string   `json:"node_id"`
	NodeName string   `json:"node_name"`
	Local    bool     `json:"local"`
	Backend  string   `json:"backend,omitempty"`
	Label    FitLabel `json:"label"`
	Eligible bool     `json:"eligible"`
	Reason   string   `json:"reason"`
	Hyper    Hyper    `json:"hyper"`
	// MemoryNeeded and MemoryAvailable are bytes of accelerator memory.
	MemoryNeeded    uint64 `json:"memory_needed_bytes"`
	MemoryAvailable uint64 `json:"memory_available_bytes"`
	// DownloadBytes counts base weights and the trainer environment still to fetch.
	DownloadBytes    uint64 `json:"download_bytes"`
	StorageNeeded    uint64 `json:"storage_needed_bytes"`
	StorageAvailable uint64 `json:"storage_available_bytes"`
	// DurationSec is a rough estimate of the training time after downloads.
	DurationSec int      `json:"duration_sec"`
	Notes       []string `json:"notes,omitempty"`
}

// FitInput is everything an estimate needs about one computer.
type FitInput struct {
	NodeID, NodeName string
	Local            bool
	Hardware         contracts.HardwareInventory
	Info             models.TrainingInfo
	Hyper            Hyper
	Stats            DatasetStats
	Trainer          Trainer
	// Cached reports whether the weights for a repo are already downloaded.
	Cached func(repo string) bool
	// EnvInstalled is true when the trainer environment exists.
	EnvInstalled bool
	// Pinned keeps the method and batch the user chose in Advanced mode.
	Pinned bool
	// Unavailable, when set, is why the trainer cannot run on this computer
	// even though the hardware suits it.
	Unavailable string
}

// Approximate size of the trainer environment download.
const trainerEnvBytes = 450 << 20

// EstimateFit grades training on one computer. When the preset does not fit,
// it tries the cheaper settings a person would: QLoRA, then a batch of one
// with gradient checkpointing, then fewer layers.
func EstimateFit(in FitInput) NodeFit {
	fit := NodeFit{NodeID: in.NodeID, NodeName: in.NodeName, Local: in.Local, Hyper: in.Hyper}
	if in.Trainer == nil {
		fit.Label, fit.Reason = FitUnsupported, "No trainer is available for this computer."
		return fit
	}
	fit.Backend = in.Trainer.ID()
	if ok, why := in.Trainer.Supports(in.Hardware); !ok {
		fit.Label, fit.Reason = FitUnsupported, why
		return fit
	}
	if in.Unavailable != "" {
		fit.Label, fit.Reason = FitUnsupported, in.Unavailable
		return fit
	}
	capacity := trainingCapacity(in.Hardware)
	fit.MemoryAvailable = capacity
	qlora := canQLoRA(in.Trainer, in.Hardware, in.Info)
	if in.Hyper.Method == MethodQLoRA && !qlora {
		in.Hyper.Method = MethodLoRA
		fit.Notes = append(fit.Notes, "Uses LoRA, because QLoRA is not available on this computer.")
	}

	candidates := []Hyper{in.Hyper}
	if !in.Pinned {
		h := in.Hyper
		if h.Method == MethodLoRA && qlora {
			h.Method = MethodQLoRA
			candidates = append(candidates, h)
		}
		h.BatchSize, h.GradCheckpoint = 1, true
		h.Iters = in.Hyper.Iters * max(in.Hyper.BatchSize, 1)
		candidates = append(candidates, h)
		if h.Layers > 8 || h.Layers < 0 {
			h.Layers = 8
			candidates = append(candidates, h)
		}
	}
	chosen := candidates[0]
	need := memoryNeeded(in.Info, chosen, in.Stats)
	for _, h := range candidates {
		n := memoryNeeded(in.Info, h, in.Stats)
		chosen, need = h, n
		if capacity == 0 || float64(n) <= 0.9*float64(capacity) {
			break
		}
	}
	fit.Hyper = chosen
	fit.MemoryNeeded = need
	if chosen.Method != in.Hyper.Method {
		fit.Notes = append(fit.Notes, "Uses QLoRA (4-bit base weights) so training fits in memory.")
	}
	if chosen.GradCheckpoint && !in.Hyper.GradCheckpoint {
		fit.Notes = append(fit.Notes, "Trains one example at a time with gradient checkpointing to save memory. This is slower.")
	}
	if chosen.Layers != in.Hyper.Layers {
		fit.Notes = append(fit.Notes, fmt.Sprintf("Trains the last %d layers instead of %d to save memory.", chosen.Layers, in.Hyper.Layers))
	}

	repo, weights := trainingWeights(in.Trainer, in.Info, chosen.Method)
	if in.Cached == nil || !in.Cached(repo) {
		fit.DownloadBytes += weights
	}
	if !in.EnvInstalled {
		fit.DownloadBytes += envBytes(in.Trainer)
	}
	adapter := loraParams(in.Info, chosen) * 4
	fit.StorageNeeded = fit.DownloadBytes + adapter + uint64(in.Stats.Tokens*8)
	fit.StorageAvailable = in.Hardware.Disk.AvailableBytes
	fit.DurationSec = estimateDuration(in.Info, chosen, in.Stats)

	switch {
	case capacity == 0:
		fit.Label, fit.Reason = FitTight, "Toskar could not read this computer's memory. Training may fail."
	case float64(need) <= 0.7*float64(capacity):
		fit.Label, fit.Reason = FitComfortable, "Fits in memory with room to spare."
	case float64(need) <= 0.95*float64(capacity):
		fit.Label, fit.Reason = FitTight, "Fits, with little memory to spare. Close other apps while it trains."
	default:
		fit.Label = FitTooLarge
		fit.Reason = fmt.Sprintf("Needs about %s for training. This computer can give training about %s.", gib(need), gib(capacity))
	}
	if fit.StorageAvailable > 0 && fit.StorageNeeded > fit.StorageAvailable {
		fit.Label = FitTooLarge
		fit.Reason = fmt.Sprintf("Needs %s of free disk space. %s is free.", gib(fit.StorageNeeded), gib(fit.StorageAvailable))
	}
	fit.Eligible = fit.Label == FitComfortable || fit.Label == FitTight
	return fit
}

// trainingCapacity is the memory training may use. Apple Silicon shares
// memory with the system; MLX's working-set limit is about three quarters.
func trainingCapacity(hw contracts.HardwareInventory) uint64 {
	var vram uint64
	unified := false
	for _, a := range hw.Accelerators {
		for _, b := range a.Backends {
			if strings.EqualFold(b, "metal") {
				unified = true
			}
		}
		if a.UnifiedMemory > 0 {
			unified = true
		}
		if a.DedicatedVRAM > vram {
			vram = a.DedicatedVRAM
		}
	}
	if unified || (hw.OS == "darwin" && hw.Arch == "arm64") {
		return hw.Memory.TotalBytes / 4 * 3
	}
	return vram
}

// loraParams approximates adapter parameters across q, k, v, o and the three
// MLP projections of each trained layer.
func loraParams(info models.TrainingInfo, h Hyper) uint64 {
	layers := h.Layers
	if layers <= 0 || layers > info.Layers {
		layers = info.Layers
	}
	return uint64(float64(layers) * float64(h.Rank) * 25.7 * float64(info.HiddenSize))
}

// memoryNeeded is weights + adapter with optimizer state + activations +
// logits + runtime overhead, with a 20% margin. It was calibrated against MLX
// runs of Qwen 2.5 0.5B and Llama 3.2 1B on Apple Silicon.
func memoryNeeded(info models.TrainingInfo, h Hyper, st DatasetStats) uint64 {
	weights := float64(info.BaseBytes)
	if h.Method == MethodQLoRA && info.QuantizedBytes > 0 {
		weights = float64(info.QuantizedBytes)
	}
	layers := h.Layers
	if layers <= 0 || layers > info.Layers {
		layers = info.Layers
	}
	seq := float64(min(max(st.P95Tokens, 64), max(h.MaxSeqLength, 64)))
	batch := float64(max(h.BatchSize, 1))
	adapter := float64(loraParams(info, h)) * 16
	act := batch * seq * float64(info.HiddenSize) * float64(layers) * 34
	if h.GradCheckpoint {
		act /= 6
	}
	logits := batch * seq * float64(info.VocabSize) * 6
	overhead := 600.0 * (1 << 20)
	return uint64((weights + adapter + act + logits + overhead) * 1.2)
}

// estimateDuration assumes about 1,100 training tokens per second for a
// one-billion-parameter model on Apple Silicon, scaled by size. QLoRA runs
// slower. The live job replaces this with measured throughput.
func estimateDuration(info models.TrainingInfo, h Hyper, st DatasetStats) int {
	paramsB := float64(info.BaseBytes) / 2e9
	if paramsB <= 0 {
		return 0
	}
	tps := 1100 / paramsB
	if h.Method == MethodQLoRA {
		tps *= 0.75
	}
	if h.GradCheckpoint {
		tps *= 0.7
	}
	avg := 64.0
	if st.Usable > 0 {
		avg = float64(st.Tokens) / float64(st.Usable)
	}
	tokens := float64(h.Iters*max(h.BatchSize, 1)) * avg
	return int(tokens/tps) + 45
}

func gib(n uint64) string { return fmt.Sprintf("%.1f GB", float64(n)/(1<<30)) }

// PickNode chooses where to train. Norn's rule for training: an eligible
// computer with the most memory headroom, preferring this computer on a tie.
func PickNode(fits []NodeFit) (NodeFit, error) {
	var best *NodeFit
	headroom := func(f NodeFit) float64 {
		if f.MemoryNeeded == 0 {
			return 0
		}
		return float64(f.MemoryAvailable) / float64(f.MemoryNeeded)
	}
	for i := range fits {
		f := &fits[i]
		if !f.Eligible {
			continue
		}
		if best == nil {
			best = f
			continue
		}
		hb, hf := headroom(*best), headroom(*f)
		if hf > hb*1.1 || (hf >= hb*0.9 && f.Local && !best.Local) {
			best = f
		}
	}
	if best == nil {
		reasons := make([]string, 0, len(fits))
		for _, f := range fits {
			reasons = append(reasons, f.NodeName+": "+f.Reason)
		}
		return NodeFit{}, fmt.Errorf("no computer can train this model. %s", strings.Join(reasons, " "))
	}
	return *best, nil
}
