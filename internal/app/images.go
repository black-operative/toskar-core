package app

import (
	"context"
	"path/filepath"
	"sync"

	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/internal/imagegen"
)

// newImageSetup keeps stable-diffusion.cpp with the runtimes and image
// models with the models, and sizes its recommendation to this computer.
func (a *App) newImageSetup(cfg config.Config) *imagegen.Setup {
	return a.newSDSetup(cfg, "images", nil)
}

// newVideoSetup is video generation's setup: the same stable-diffusion.cpp,
// with video models in their own folder (Gungnir §27).
func (a *App) newVideoSetup(cfg config.Config) *imagegen.Setup {
	return a.newSDSetup(cfg, "video", imagegen.VideoCatalog)
}

func (a *App) newSDSetup(cfg config.Config, folder string, catalog func() []imagegen.Model) *imagegen.Setup {
	memory := sync.OnceValue(func() int64 {
		inv, err := a.hw.Detect(context.Background())
		if err != nil {
			return 0
		}
		return int64(inv.Memory.TotalBytes)
	})
	return &imagegen.Setup{
		ProgramDir: filepath.Join(cfg.RuntimesDir, "sdcpp"),
		ModelsDir:  filepath.Join(cfg.ModelsDir, folder),
		Catalog:    catalog,
		Memory:     memory,
		FreeBytes: func() (int64, error) {
			inv, err := a.hw.Detect(context.Background())
			if err != nil {
				return 0, err
			}
			return int64(inv.Disk.AvailableBytes), nil
		},
		Sandboxed: a.python.Sandboxed,
		// "Can you make images?" and setup offers read the inventory.
		Changed: a.invalidateCapabilities,
	}
}
