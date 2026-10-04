package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/yeixio/yggdrasil-core/internal/app"
	"github.com/yeixio/yggdrasil-core/internal/config"
	"github.com/yeixio/yggdrasil-core/internal/ocr"
	"github.com/yeixio/yggdrasil-core/internal/pyenv"
	"github.com/yeixio/yggdrasil-core/internal/training"
	"github.com/yeixio/yggdrasil-core/internal/version"
)

func main() {
	dataDir := flag.String("data-dir", "", "application data directory")
	showVersion := flag.Bool("version", false, "print version, license, and corresponding source")
	pythonEnvs := flag.Bool("python-envs", false, "print the Python environments to bundle with a sandboxed app, as JSON")
	flag.Parse()

	if *showVersion {
		fmt.Print(version.CurrentOffer().Text())
		os.Exit(0)
	}
	if *pythonEnvs {
		printPythonEnvs()
		os.Exit(0)
	}

	if *dataDir == "" {
		*dataDir = config.DefaultDataDir()
	}

	logger, logCloser, err := setupLogger(*dataDir)
	if err != nil {
		slog.Error("failed to open log file", "error", err)
		logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}
	if logCloser != nil {
		defer logCloser.Close()
	}

	application, err := app.New(app.Options{DataDir: *dataDir, Logger: logger})
	if err != nil {
		logger.Error("failed to initialize", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := application.Start(ctx); err != nil {
		logger.Error("daemon exited with error", "error", err)
		os.Exit(1)
	}
}

func setupLogger(dataDir string) (*slog.Logger, io.Closer, error) {
	logsDir := filepath.Join(dataDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return nil, nil, err
	}
	logPath := filepath.Join(logsDir, "daemon.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, err
	}
	w := io.MultiWriter(os.Stdout, f)
	logger := slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: slog.LevelInfo}))
	return logger, f, nil
}

// printPythonEnvs describes each managed Python environment for packagers.
// A sandboxed app (the Mac App Store build) cannot install them, so it ships
// them signed in a "python" folder beside the daemon: python/<name>/ with
// the interpreter, the packages, and a ".toskar-requirements" file whose
// content is "marker" exactly (see docs/runtimes.md).
func printPythonEnvs() {
	type env struct {
		Name         string   `json:"name"`
		Use          string   `json:"use"`
		Python       string   `json:"python"`
		Requirements []string `json:"requirements"`
		InstallArgs  []string `json:"install_args,omitempty"`
		NoDeps       bool     `json:"no_deps,omitempty"`
		Marker       string   `json:"marker"`
	}
	var out []env
	for _, e := range []struct {
		use  string
		spec pyenv.Spec
	}{
		{"Training on Apple Silicon (MLX)", training.MLX{}.Environment()},
		{"Training on NVIDIA GPUs (PyTorch)", training.PEFT{}.Environment()},
		{"Text recognition for scanned PDFs", ocr.Spec()},
	} {
		out = append(out, env{Name: e.spec.Name, Use: e.use, Python: pyenv.PythonVersion, Requirements: e.spec.Requirements,
			InstallArgs: e.spec.InstallArgs, NoDeps: e.spec.Pinned, Marker: pyenv.RequirementsKey(e.spec)})
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}
