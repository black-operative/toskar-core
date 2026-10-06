### Added

- The Linux packages recommend the Vulkan loader, Mesa's Vulkan drivers, `vulkan-tools`, and `pciutils`, so apt and dnf install what an AMD or Intel GPU needs, and the service user joins the `render` and `video` groups so it can use the card. `install.sh` prints the command to install NVIDIA's driver when it finds an NVIDIA card without it, and `install.ps1` points to the card maker's driver when Windows has no Vulkan (#317).
- The Dockerfile has a `gpu` target with Vulkan and Mesa's drivers, for a container given the card with `--device /dev/dri`.
- [docs/gpu.md](docs/gpu.md) covers what each computer needs for its GPU, what the installers set up, and how to fix a model that runs on the CPU.
