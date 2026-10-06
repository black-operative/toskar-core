# Compatibility

This matrix is what the repository implements and what its tests actually exercise. It is not a certification. "Supported" means this tree contains a working code path and project CI or a release build covers that path. Combinations without a recorded run on that hardware are Untested or Experimental.

Submit a correction with the [hardware compatibility](../.github/ISSUE_TEMPLATE/hardware_compatibility.yml) issue form. Those reports are how this table should get more precise.

| Machine | Detection | Runtime support | Inference tested | Multi-node tested | Notes |
| --- | --- | --- | --- | --- | --- |
| macOS / Apple Silicon | Experimental | Experimental | Untested | Untested | `internal/hardware/darwin.go` reports CPU, memory, disk, and an Apple GPU with Metal. llama.cpp install selects `bin-macos-arm64`. Release CI cross-compiles darwin/arm64 from Linux. CI does not run the daemon on Apple Silicon. |
| macOS / Intel | Experimental | Experimental | Untested | Untested | Same Darwin detector. llama.cpp install selects `bin-macos-x64`. Release archives include darwin/amd64. No Intel Mac run is recorded in CI. |
| Windows / NVIDIA | Experimental | Experimental | Untested | Untested | `internal/hardware/windows.go` classifies a GPU name that contains "nvidia" and lists CUDA and Vulkan as backends. The llama.cpp installer selects `bin-win-vulkan-x64` when NVIDIA's driver provides Vulkan, and `bin-win-cpu-x64` otherwise. Live figures come from `nvidia-smi`. No Windows GPU run is recorded in CI. |
| Windows / AMD | Experimental | Experimental | Untested | Untested | Name heuristic for AMD or Radeon. The installer selects `bin-win-vulkan-x64` when the card's driver provides Vulkan. Live busy % and memory come from Windows' GPU counters; temperature and power are not available. |
| Windows / Intel | Experimental | Experimental | Untested | Untested | Name heuristic for Intel graphics. As for AMD. |
| Linux / NVIDIA | Experimental | Experimental | Untested | Untested | `nvidia-smi` is used when it is on `PATH`. Unit tests run on Ubuntu in CI and do not require a GPU. llama.cpp install selects `bin-ubuntu-vulkan-x64` when the driver provides Vulkan, and `bin-ubuntu-x64` or `bin-ubuntu-arm64` otherwise. |
| Linux / AMD | Experimental | Experimental | Tested | Untested | Cards come from `/sys/class/drm` (named by `lspci` when installed), with their video memory. The installer selects `bin-ubuntu-vulkan-x64`. The weekly quality runs use a Radeon RX 7900 XTX through Vulkan in Docker (#296, #317): all layers on the GPU, and the quality set passes with Qwen 2.5 7B, 14B, and 32B. `rocminfo` on `PATH` adds `rocm` to the reported backends; no ROCm run is recorded. |
| Linux / Intel | Experimental | Experimental | Untested | Untested | Cards come from `/sys/class/drm`, counted only when no other accelerator was found. Backends reported are Vulkan and CPU. |

## Training, knowledge, and text recognition

These features run their own programs, so they have their own status. "Manual run" means a run recorded in a pull request, not in CI.

| Feature | macOS / Apple Silicon | Linux / NVIDIA | Windows / NVIDIA | Other | Notes |
| --- | --- | --- | --- | --- | --- |
| Training (MLX) | Manual run | Not supported | Not supported | Not supported | Apple M5 Pro, end to end through evaluation and deployment (#48) |
| Training (PyTorch PEFT) | Not used (MLX is chosen) | Untested | Untested | Not supported | The trainer ran on Apple MPS against llama.cpp. The CUDA path, QLoRA with bitsandbytes, and uv's CUDA build selection have not run on NVIDIA hardware. |
| Text recognition (scanned PDFs) | Manual run | Untested | Untested | Untested | RapidOCR in a pinned Python environment, with headless OpenCV so Linux servers need no graphics libraries |
| Meaning search (embedding model) | Manual run | Untested | Untested | Untested | Nomic Embed Text v1.5 in llama-server |
| GGUF export | Manual run | Untested | Untested | Untested | `llama-export-lora` from the llama.cpp install |
| Mac App Store build | Simulated | | | | `TOSKAR_SANDBOXED=1`; bundled Python environments are not yet packaged in the store build |

## How to read the columns

**Detection.** The daemon can fill a hardware inventory on that OS. GPU rows depend on external commands (`nvidia-smi`, `lspci`) or on macOS system information. A missing command degrades the inventory. It does not fail the whole detect call.

**Runtime support.** The llama.cpp installer has a filename it looks for, or the external OpenAI adapter can point at a server you already run. Experimental means that path is in the source and has not been treated as a tested matrix entry.

**Inference tested.** No automated job in this repository generates tokens on these GPUs. A community report can move a cell once it includes the model, quantization, and result.

**Multi-node tested.** Pairing and Team placement have Go tests and a Docker compose file that uses stub inference. That is not a test of two physical computers. The steps people can run by hand are in [two-machine-team-demo.md](two-machine-team-demo.md).

## Sending a report

Open a [Hardware](../.github/ISSUE_TEMPLATE/hardware_compatibility.yml) issue and classify the machine as Works, Works with limitations, Does not work, or Not sure. Include the OS version, CPU, GPU, RAM, Toskar version, runtime, and models. Tokens per second are optional. Results move around with model size, context, and other programs using the machine.
