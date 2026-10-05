### Added

- Running models report where they run, read from llama.cpp's own log when it loads them: on the GPU, partly, or on the CPU, with the device, the layers on the GPU, the GPU memory, and the reason when a model isn't fully on the GPU. `GET /api/v1/models/running` has it in `acceleration`, and `GET /api/v1/health` sums it up in `acceleration` without changing `status` (#317).

### Fixed

- A running model's speed is filled in from its latest replies, and its device is the one it runs on rather than the first graphics card found.
