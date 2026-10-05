package training

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func fakeSpec(t *testing.T) RunSpec {
	t.Helper()
	dir := t.TempDir()
	return RunSpec{WorkDir: dir, LogPath: filepath.Join(dir, "train.log"), AdapterOut: filepath.Join(dir, "adapter.gguf"),
		Hyper: Hyper{Iters: 10, Epochs: 2}}
}

func writeScript(t *testing.T, dir, body string) string {
	t.Helper()
	p := filepath.Join(dir, "fake.sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRunScriptTurnsEventsIntoUpdates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script trainer")
	}
	spec := fakeSpec(t)
	script := writeScript(t, spec.WorkDir, `
echo "Loading pretrained model"
echo '@@ygg {"event":"stage","stage":"loading_model","detail":"Downloading base weights"}'
echo '@@ygg {"event":"download","bytes":50,"total":100}'
echo '@@ygg {"event":"stage","stage":"training","detail":"Training"}'
echo '@@ygg {"event":"progress","iter":5,"iters":10,"train_loss":1.25,"tokens_per_sec":900,"peak_memory_gb":1.6}'
echo '@@ygg {"event":"val","iter":5,"val_loss":1.5}'
echo '@@ygg {"event":"stage","stage":"exporting","detail":"Writing the adapter"}'
echo '@@ygg {"event":"done","adapter":"`+spec.AdapterOut+`","train_loss":0.5,"val_loss":0.75}'
`)
	var updates []Update
	res, err := runScript(context.Background(), spec, []string{script}, func(u Update) { updates = append(updates, u) })
	if err != nil {
		t.Fatal(err)
	}
	if res.AdapterPath != spec.AdapterOut || *res.TrainLoss != 0.5 || *res.ValLoss != 0.75 {
		t.Fatalf("result = %+v", res)
	}
	var states []string
	for _, u := range updates {
		states = append(states, string(u.State))
	}
	want := "loading_model,loading_model,training,training,training,exporting"
	if strings.Join(states, ",") != want {
		t.Fatalf("states = %v", states)
	}
	train := updates[3].Progress
	if train.Iter != 5 || *train.TrainLoss != 1.25 || train.Epoch != 1 || train.PeakMemoryGB != 1.6 {
		t.Fatalf("progress = %+v", train)
	}
	if updates[1].Progress.DownloadBytes != 50 || updates[1].Progress.DownloadTotal != 100 {
		t.Fatalf("download = %+v", updates[1].Progress)
	}
	log, _ := os.ReadFile(spec.LogPath)
	if !strings.Contains(string(log), "Loading pretrained model") || strings.Contains(string(log), "@@ygg") {
		t.Fatalf("log = %q", log)
	}
}

func TestRunScriptReportsTheScriptError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script trainer")
	}
	spec := fakeSpec(t)
	script := writeScript(t, spec.WorkDir, `
echo '@@ygg {"event":"error","message":"OSError: repository not found"}'
exit 1
`)
	_, err := runScript(context.Background(), spec, []string{script}, func(Update) {})
	if err == nil || !strings.Contains(err.Error(), "repository not found") || !strings.Contains(err.Error(), spec.LogPath) {
		t.Fatalf("err = %v", err)
	}
}

func TestRunScriptWithoutAdapterFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script trainer")
	}
	spec := fakeSpec(t)
	script := writeScript(t, spec.WorkDir, "exit 0\n")
	if _, err := runScript(context.Background(), spec, []string{script}, func(Update) {}); err == nil {
		t.Fatal("a trainer that wrote no adapter must fail")
	}
}

func TestRateTrackerWaitsForASteadyRate(t *testing.T) {
	var r rateTracker
	start := time.Unix(1000, 0)
	if r.remaining(start, 1, 100) != nil {
		t.Fatal("first report has no rate")
	}
	if r.remaining(start.Add(2*time.Second), 2, 100) != nil {
		t.Fatal("too early to trust")
	}
	got := r.remaining(start.Add(10*time.Second), 11, 100)
	if got == nil || *got != 89 {
		t.Fatalf("remaining = %v", got)
	}
}

func TestPEFTTrainsOnCUDA(t *testing.T) {
	if ok, _ := (PEFT{}).Supports(nvidiaPC(24)); !ok {
		t.Fatal("PEFT should train on a CUDA GPU")
	}
	if ok, why := (PEFT{}).Supports(mac(32)); ok || !strings.Contains(why, "NVIDIA") {
		t.Fatalf("mac: %v %s", ok, why)
	}
	// On a Mac, MLX is chosen; on an NVIDIA PC, PEFT.
	backends := []Trainer{MLX{}, PEFT{}}
	if TrainerFor(backends, mac(32)).ID() != "mlx" || TrainerFor(backends, nvidiaPC(24)).ID() != "peft" {
		t.Fatal("wrong trainer chosen")
	}
	spec := (PEFT{}).Environment()
	if spec.Name != "trainer-peft" || len(spec.InstallArgs) != 1 || spec.InstallArgs[0] != "--torch-backend=auto" {
		t.Fatalf("environment = %+v", spec)
	}
	if !strings.Contains(string(peftScript), `event="done"`) {
		t.Fatal("the PEFT script is not embedded")
	}
}

// TestPEFTScriptTrainsAnAdapter runs the PyTorch trainer for a few steps. It
// is skipped unless YGG_TEST_PEFT_PYTHON is an interpreter with
// PEFTRequirements installed. It downloads Qwen 2.5 0.5B (about 1 GB) into
// HF_HOME on first run, and uses CUDA, MPS, or the CPU.
func TestPEFTScriptTrainsAnAdapter(t *testing.T) {
	py := os.Getenv("YGG_TEST_PEFT_PYTHON")
	if py == "" {
		t.Skip("set YGG_TEST_PEFT_PYTHON to an interpreter with the PEFT packages")
	}
	dir := t.TempDir()
	data := filepath.Join(dir, "data")
	_ = os.MkdirAll(data, 0o755)
	var train, valid strings.Builder
	for i := range 24 {
		line := fmt.Sprintf(`{"messages":[{"role":"user","content":"Question %d about tires"},{"role":"assistant","content":"Ahoy! What year, make, and model is car %d?"}]}`+"\n", i, i)
		if i < 20 {
			train.WriteString(line)
		} else {
			valid.WriteString(line)
		}
	}
	_ = os.WriteFile(filepath.Join(data, "train.jsonl"), []byte(train.String()), 0o644)
	_ = os.WriteFile(filepath.Join(data, "valid.jsonl"), []byte(valid.String()), 0o644)
	var states []State
	res, err := (PEFT{}).Run(context.Background(), RunSpec{
		Python: py, Env: os.Environ(), WorkDir: filepath.Join(dir, "work"), Repo: "Qwen/Qwen2.5-0.5B-Instruct", Architecture: "qwen2",
		Hyper:   Hyper{Method: MethodLoRA, Rank: 8, Scale: 2, Layers: 4, LearningRate: 2e-4, BatchSize: 2, Iters: 10, MaxSeqLength: 256},
		DataDir: data, AdapterOut: filepath.Join(dir, "adapter.gguf"), LogPath: filepath.Join(dir, "train.log"),
	}, func(u Update) { states = append(states, u.State) })
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(res.AdapterPath)
	if err != nil || st.Size() == 0 || res.TrainLoss == nil || res.ValLoss == nil {
		t.Fatalf("result = %+v, %v", res, err)
	}
	if !slices.Contains(states, StateTraining) || !slices.Contains(states, StateExporting) {
		t.Fatalf("states = %v", states)
	}
	t.Logf("train loss %.3f, validation loss %.3f, adapter %d bytes", *res.TrainLoss, *res.ValLoss, st.Size())
}
