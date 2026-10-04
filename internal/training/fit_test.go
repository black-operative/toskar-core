package training

import (
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/models"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Shapes from the catalog.
var (
	qwen05 = models.TrainingInfo{BaseRepo: "Qwen/Qwen2.5-0.5B-Instruct", Architecture: "qwen2", HiddenSize: 896, Layers: 24, VocabSize: 151936, BaseBytes: 988097824}
	llama1 = models.TrainingInfo{BaseRepo: "unsloth/Llama-3.2-1B-Instruct", QuantizedRepo: "mlx-community/Llama-3.2-1B-Instruct-4bit",
		Architecture: "llama", HiddenSize: 2048, Layers: 16, VocabSize: 128256, BaseBytes: 2471645608, QuantizedBytes: 695283921}
	qwen7 = models.TrainingInfo{BaseRepo: "Qwen/Qwen2.5-7B-Instruct", QuantizedRepo: "mlx-community/Qwen2.5-7B-Instruct-4bit",
		Architecture: "qwen2", HiddenSize: 3584, Layers: 28, VocabSize: 152064, BaseBytes: 15231233024, QuantizedBytes: 4283982500}
)

func mac(memGiB uint64) contracts.HardwareInventory {
	return contracts.HardwareInventory{OS: "darwin", Arch: "arm64",
		Memory:       contracts.MemoryInfo{TotalBytes: memGiB << 30},
		Disk:         contracts.DiskInfo{AvailableBytes: 500 << 30},
		Accelerators: []contracts.Accelerator{{Kind: "gpu", Backends: []string{"metal"}}}}
}

func TestMemoryEstimateMatchesMeasuredRuns(t *testing.T) {
	// Spike runs on an M5 Pro: batch 4, 16 layers, rank 8, ~60-token examples.
	st := DatasetStats{Usable: 20, P95Tokens: 60, Tokens: 1100}
	h := Hyper{Method: MethodLoRA, Rank: 8, Layers: 16, BatchSize: 4, MaxSeqLength: 2048}
	cases := []struct {
		info     models.TrainingInfo
		measured float64 // GiB
	}{{qwen05, 1.65}, {llama1, 4.17}}
	// End-to-end run through the daemon: rank 16, instructions prepended.
	e2e := Hyper{Method: MethodLoRA, Rank: 16, Layers: 16, BatchSize: 4, MaxSeqLength: 256}
	if got := float64(memoryNeeded(llama1, e2e, DatasetStats{Usable: 40, P95Tokens: 130})) / (1 << 30); got < 5.21*0.9 {
		t.Errorf("daemon run: estimated %.2f GiB, measured 5.21", got)
	}
	for _, c := range cases {
		got := float64(memoryNeeded(c.info, h, st)) / (1 << 30)
		// Never under by more than 10%; a generous overestimate is acceptable.
		if got < c.measured*0.9 || got > c.measured*1.6 {
			t.Errorf("%s: estimated %.2f GiB, measured %.2f", c.info.BaseRepo, got, c.measured)
		}
	}
	if p := loraParams(qwen05, h); p < 2.8e6 || p > 3.1e6 {
		t.Errorf("qwen 0.5B adapter params = %d, mlx reported 2.933M", p)
	}
}

func TestPresetsScaleWithEffort(t *testing.T) {
	st := DatasetStats{Usable: 100, P95Tokens: 300, Tokens: 20000}
	q := PresetSettings(PresetQuick, qwen7, st)
	b := PresetSettings(PresetBalanced, qwen7, st)
	hq := PresetSettings(PresetQuality, qwen7, st)
	if q.Rank >= b.Rank || b.Rank >= hq.Rank || q.Iters >= b.Iters || b.Iters >= hq.Iters {
		t.Fatalf("quick %+v balanced %+v quality %+v", q, b, hq)
	}
	if q.Method != MethodQLoRA || b.Method != MethodLoRA {
		t.Fatalf("quick should take the small 4-bit download for a 7B model: %s / %s", q.Method, b.Method)
	}
	if hq.Layers != qwen7.Layers {
		t.Fatalf("quality trains all %d layers, got %d", qwen7.Layers, hq.Layers)
	}
	if b.MaxSeqLength != 384 {
		t.Fatalf("max seq = %d", b.MaxSeqLength)
	}
	// Ten examples still see each one several times.
	small := PresetSettings(PresetBalanced, qwen7, DatasetStats{Usable: 10, P95Tokens: 80})
	if small.Iters < 20 {
		t.Fatalf("iters for 10 examples = %d", small.Iters)
	}
}

func TestApplyAdvancedOverridesOnlySetFields(t *testing.T) {
	base := PresetSettings(PresetBalanced, qwen05, DatasetStats{Usable: 40, P95Tokens: 100})
	got := ApplyAdvanced(base, &Hyper{Rank: 64, Epochs: 1}, 40)
	if got.Rank != 64 || got.Layers != base.Layers || got.Iters >= base.Iters {
		t.Fatalf("got %+v from %+v", got, base)
	}
	if ApplyAdvanced(base, nil, 40) != base {
		t.Fatal("nil advanced changed settings")
	}
}

func TestEstimateFitFallsBackToCheaperSettings(t *testing.T) {
	st := DatasetStats{Usable: 100, P95Tokens: 512, Tokens: 40000}
	h := PresetSettings(PresetBalanced, qwen7, st)

	roomy := EstimateFit(FitInput{NodeName: "studio", Local: true, Hardware: mac(64), Info: qwen7, Hyper: h, Stats: st, Trainer: MLX{}})
	if roomy.Label != FitComfortable || roomy.Hyper.Method != MethodLoRA || !roomy.Eligible {
		t.Fatalf("64 GB: %+v", roomy)
	}
	if roomy.DownloadBytes != qwen7.BaseBytes+trainerEnvBytes || roomy.DurationSec <= 0 {
		t.Fatalf("download %d duration %d", roomy.DownloadBytes, roomy.DurationSec)
	}

	laptop := EstimateFit(FitInput{NodeName: "laptop", Local: true, Hardware: mac(16), Info: qwen7, Hyper: h, Stats: st, Trainer: MLX{},
		Cached: func(string) bool { return true }, EnvInstalled: true})
	if !laptop.Eligible || laptop.Hyper.Method != MethodQLoRA || len(laptop.Notes) == 0 {
		t.Fatalf("16 GB: %+v", laptop)
	}
	if laptop.DownloadBytes != 0 {
		t.Fatalf("cached weights and env still counted %d bytes", laptop.DownloadBytes)
	}

	tiny := EstimateFit(FitInput{NodeName: "air", Hardware: mac(4), Info: qwen7, Hyper: h, Stats: st, Trainer: MLX{}})
	if tiny.Eligible || tiny.Label != FitTooLarge || !strings.Contains(tiny.Reason, "Needs about") {
		t.Fatalf("4 GB: %+v", tiny)
	}

	pinned := EstimateFit(FitInput{NodeName: "laptop", Hardware: mac(16), Info: qwen7, Hyper: h, Stats: st, Trainer: MLX{}, Pinned: true})
	if pinned.Hyper.Method != MethodLoRA || pinned.Eligible {
		t.Fatalf("pinned advanced settings changed or fit: %+v", pinned)
	}
}

func nvidiaPC(vramGiB uint64) contracts.HardwareInventory {
	return contracts.HardwareInventory{OS: "linux", Arch: "amd64", Memory: contracts.MemoryInfo{TotalBytes: 64 << 30},
		Disk:         contracts.DiskInfo{AvailableBytes: 500 << 30},
		Accelerators: []contracts.Accelerator{{Kind: "gpu", DedicatedVRAM: vramGiB << 30, Backends: []string{"cuda"}}}}
}

func TestNVIDIAComputersTrainWithPEFT(t *testing.T) {
	backends := []Trainer{MLX{}, PEFT{}}
	st := DatasetStats{Usable: 100, P95Tokens: 512, Tokens: 40000}
	h := PresetSettings(PresetBalanced, qwen7, st)

	big := EstimateFit(FitInput{NodeName: "gpu-box", Hardware: nvidiaPC(48), Info: qwen7, Hyper: h, Stats: st, Trainer: TrainerFor(backends, nvidiaPC(48))})
	if !big.Eligible || big.Backend != "peft" || big.Hyper.Method != MethodLoRA {
		t.Fatalf("48 GB GPU: %+v", big)
	}
	// PyTorch downloads the original weights and its CUDA environment.
	if big.DownloadBytes != qwen7.BaseBytes+(PEFT{}).EnvBytes() {
		t.Fatalf("download = %d", big.DownloadBytes)
	}

	// A smaller GPU falls back to QLoRA, which still loads the original
	// weights (bitsandbytes quantizes them), not the MLX 4-bit copy.
	small := EstimateFit(FitInput{NodeName: "gaming-pc", Hardware: nvidiaPC(12), Info: qwen7, Hyper: h, Stats: st, Trainer: PEFT{}, EnvInstalled: true})
	if small.Hyper.Method != MethodQLoRA || small.DownloadBytes != qwen7.BaseBytes {
		t.Fatalf("12 GB GPU: %+v", small)
	}
	if repo, _ := trainingWeights(PEFT{}, qwen7, MethodQLoRA); repo != qwen7.BaseRepo {
		t.Fatalf("PEFT QLoRA repo = %s", repo)
	}
	if repo, _ := trainingWeights(MLX{}, qwen7, MethodQLoRA); repo != qwen7.QuantizedRepo {
		t.Fatalf("MLX QLoRA repo = %s", repo)
	}
}

func TestEstimateFitUnsupportedHardware(t *testing.T) {
	cpuOnly := contracts.HardwareInventory{OS: "linux", Arch: "amd64", Memory: contracts.MemoryInfo{TotalBytes: 64 << 30}}
	backends := []Trainer{MLX{}, PEFT{}}
	fit := EstimateFit(FitInput{NodeName: "server", Hardware: cpuOnly, Info: qwen05, Hyper: Hyper{Rank: 8, Layers: 8, BatchSize: 4}, Trainer: TrainerFor(backends, cpuOnly)})
	if fit.Eligible || fit.Label != FitUnsupported || !strings.Contains(fit.Reason, "NVIDIA GPU with CUDA") {
		t.Fatalf("no GPU: %+v", fit)
	}
}

// noQLoRA is a trainer that cannot train QLoRA on any hardware.
type noQLoRA struct{ MLX }

func (noQLoRA) Weights(info models.TrainingInfo, _ Method) (string, uint64) {
	return info.BaseRepo, info.BaseBytes
}
func (noQLoRA) CanQLoRA(contracts.HardwareInventory, models.TrainingInfo) bool { return false }

func TestQLoRAIsReplacedWhereItCannotRun(t *testing.T) {
	h := Hyper{Method: MethodQLoRA, Rank: 8, Layers: 8, BatchSize: 4, MaxSeqLength: 512}
	fit := EstimateFit(FitInput{NodeName: "x", Hardware: mac(64), Info: qwen7, Hyper: h, Trainer: noQLoRA{}})
	if fit.Hyper.Method != MethodLoRA || !strings.Contains(strings.Join(fit.Notes, " "), "QLoRA is not available") {
		t.Fatalf("fit = %+v", fit)
	}
}

func TestEstimateFitChecksDisk(t *testing.T) {
	hw := mac(64)
	hw.Disk.AvailableBytes = 1 << 30
	fit := EstimateFit(FitInput{NodeName: "full", Hardware: hw, Info: qwen7, Hyper: Hyper{Method: MethodLoRA, Rank: 8, Layers: 8, BatchSize: 4}, Trainer: MLX{}})
	if fit.Eligible || !strings.Contains(fit.Reason, "disk space") {
		t.Fatalf("fit = %+v", fit)
	}
}

func TestPickNode(t *testing.T) {
	fits := []NodeFit{
		{NodeName: "remote-big", Eligible: true, MemoryAvailable: 96, MemoryNeeded: 10},
		{NodeName: "local", Local: true, Eligible: true, MemoryAvailable: 90, MemoryNeeded: 10},
		{NodeName: "gpu", Eligible: false, Reason: "NVIDIA later."},
	}
	got, err := PickNode(fits)
	if err != nil || got.NodeName != "local" {
		t.Fatalf("similar headroom should stay local: %+v %v", got, err)
	}
	fits[0].MemoryAvailable = 200
	if got, _ = PickNode(fits); got.NodeName != "remote-big" {
		t.Fatalf("much more headroom should win: %+v", got)
	}
	_, err = PickNode([]NodeFit{{NodeName: "gpu", Reason: "NVIDIA later."}})
	if err == nil || !strings.Contains(err.Error(), "gpu: NVIDIA later.") {
		t.Fatalf("err = %v", err)
	}
}
