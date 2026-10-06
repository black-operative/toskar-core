package app

import (
	"context"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"time"

	"github.com/yeixio/toskar-core/internal/gpusetup"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// gpuSetup is what stands between this computer's graphics card and Toskar
// using it, with the fix for each piece (#317).
func (a *App) gpuSetup(ctx context.Context) (contracts.GPUSetup, error) {
	var cards []string
	if hw, err := a.detectHardware(ctx); err == nil {
		for _, ac := range hw.Accelerators {
			if ac.Kind == "gpu" {
				cards = append(cards, ac.Model)
			}
		}
	}
	if runtime.GOOS == "linux" {
		cards = append(cards, gpusetup.PCICards(ctx, runCommand)...)
	}
	_, cpuBuild := a.accelerationFacts(ctx)
	name := ""
	if u, err := user.Current(); err == nil {
		name = u.Username
	}
	return gpusetup.Check(ctx, gpusetup.Host{
		GOOS:  runtime.GOOS,
		Cards: cards,
		Exists: func(p string) bool {
			st, err := os.Stat(p)
			return err == nil && !st.IsDir()
		},
		Glob: func(p string) []string {
			m, _ := filepath.Glob(p)
			return m
		},
		ReadFile: os.ReadFile,
		LookPath: exec.LookPath,
		Vulkaninfo: func(ctx context.Context) (string, error) {
			if _, err := exec.LookPath("vulkaninfo"); err != nil {
				return "", err
			}
			return runCommand(ctx, "vulkaninfo", "--summary")
		},
		CanOpen: func(p string) bool {
			f, err := os.OpenFile(p, os.O_RDWR, 0)
			if err != nil {
				return false
			}
			_ = f.Close()
			return true
		},
		User:     name,
		Service:  name == "yggdrasil",
		CPUBuild: cpuBuild,
	}), nil
}

// runCommand runs a command with a short deadline and returns its output.
func runCommand(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	return string(out), err
}
