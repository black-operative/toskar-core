# Runtimes

A runtime adapter starts and stops a model and reports whether it is installed. Adapters register in-process. The interface is `pkg/pluginapi.Runtime`.

## llamacpp

| | |
| --- | --- |
| Id | `llamacpp` |
| Display name | llama.cpp (llama-server) |
| Model format | GGUF |
| Install | `POST /api/v1/runtimes/llamacpp/install`, or the web UI |

Install looks up recent GitHub releases of `ggml-org/llama.cpp` and downloads one archive whose name matches this host:

| Host | Asset name contains |
| --- | --- |
| macOS arm64 | `bin-macos-arm64` |
| macOS amd64 | `bin-macos-x64` |
| Linux amd64 | `bin-ubuntu-vulkan-x64` with a usable GPU, `bin-ubuntu-x64` otherwise |
| Linux arm64 | `bin-ubuntu-arm64` |
| Windows amd64 | `bin-win-vulkan-x64` with a usable GPU, `bin-win-cpu-x64` otherwise |
| anything else | the GOOS-GOARCH pair, which usually fails the lookup |

The binary is stored under the data directory `runtimes/llamacpp/` unless a macOS build finds a `llama-server` placed beside the daemon. A usable GPU is a Vulkan loader that finds a graphics card (`vulkaninfo`, or the loader, a GPU driver, and a detected card). A CPU install on a computer where the Vulkan build would run is replaced at the next start with no model running. Capability reporting lists `cpu` and the GPU backends of the installed build (`metal` on macOS, `vulkan` when the Vulkan build is installed). CUDA and ROCm are names the hardware inventory can attach to a GPU it sees; they are not a separate installer path. Whether a running model is actually on the GPU is read from llama-server's log; see [GPU acceleration](gpu.md).

On a Mac App Store build, the sandbox cannot `fork` a binary downloaded into the container. Those builds are expected to ship a signed `llama-server` next to the daemon. That packaging is outside this repository.

## Python environments in sandboxed builds

Training (MLX on Apple Silicon, PyTorch on NVIDIA GPUs) and text recognition for scanned PDFs run in Python environments. The daemon normally installs them under `runtimes/python` on first use: it downloads `uv`, a private Python, and pinned packages. A sandboxed app cannot run any of that.

The daemon knows it is sandboxed when macOS sets `APP_SANDBOX_CONTAINER_ID`, or when its home folder or data folder is inside an app container (`~/Library/Containers/<bundle id>/Data`). The app's daemon doesn't always see the variable, so the folder check covers it. `TOSKAR_SANDBOXED=1` simulates this for testing. When sandboxed:

- **No downloads:** it never downloads or installs a Python environment.
- **Bundled environments are used:** a current environment found in a `python` folder beside the daemon executable is used instead.
- **Training without a bundle:** the trainer is reported as unavailable on this computer, with the reason, in `GET /training/backends`, the training plan, and the capabilities a paired computer sees. Norn chooses an eligible paired computer running Toskar Core instead, and a paired computer that is sandboxed refuses runs it cannot do.
- **Read aloud without a bundle:** speech is reported as unavailable, and the app reads answers with the device's own voices instead.
- **Scanned PDFs without a bundle:** a scanned PDF fails with "text recognition is not included in this copy of Toskar". PDFs with a text layer are read as usual.

To bundle an environment, run `toskar -python-envs`. It prints each environment as JSON: `name`, `python` version, `requirements`, `install_args`, `no_deps`, and `marker`. For each one you want to ship:

1. Create `python/<name>/` beside the daemon, with a virtual environment of that Python version, for example `uv venv --python 3.12 python/<name>`.
2. Install the `requirements` with the `install_args`, and with `--no-deps` when `no_deps` is true, for example `uv pip install --python python/<name>/bin/python …`.
3. Write `marker` exactly, byte for byte, to `python/<name>/.yggdrasil-requirements`. The daemon uses the environment only when the marker matches what this daemon version expects, so a bundle built for another version is ignored.
4. Sign every executable and native library in the folder with the app.

## external-openai

| | |
| --- | --- |
| Id | `external-openai` |
| Display name | External OpenAI-compatible |

This adapter does not download a runtime. It connects to an OpenAI-compatible server you set up: `external_openai_url` in the configuration, or Settings → External server (advanced mode), with an optional API key kept in `secrets/external-openai.key`.

- **Models:** the server's models (`GET /v1/models`) appear in the model list as `ext:<name>`, marked external, for a chat to choose. The Models page lists them separately; they can't be installed, started, or removed here.
- **Chats:** a chat that chooses one sends its messages to `POST /v1/chat/completions` with the model's name and the key, streams the reply, and records it in What left this computer.
- **Never automatic:** Auto, fallback, and the default model never pick an external model. A profile that turns web search off, and a chat using memories or knowledge marked This computer only, refuse it with `EXTERNAL_OFFLINE_PROFILE` or `EXTERNAL_LOCAL_ONLY`.
- **Errors:** a refused key or an unreachable server is `EXTERNAL_FAILED`, with the server's reason.

See [privacy.md](privacy.md).

## Adding an adapter

Implement the runtime interface, register it where the app constructs the runtime registry, and cover detection plus start/stop with tests. Open a runtime request or a pull request using the forms in `.github/`. Licensing of the upstream runtime should be compatible with shipping and downloading it. Code contributions are covered by the [Toskar Contributor License Agreement](../CLA.md).
