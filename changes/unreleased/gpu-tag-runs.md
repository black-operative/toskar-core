### Added

- Replies, run traces, and benchmark samples record the backend and device that produced them, such as Vulkan on an AMD Radeon RX 7900 XTX, so performance history can tell GPU replies from CPU ones. `GET /api/v1/runtimes` lists what the installed llama.cpp build can run on (#317).
