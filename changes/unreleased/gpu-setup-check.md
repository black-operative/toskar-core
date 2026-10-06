### Added

- Diagnostics lists what this computer still needs for Toskar to use its graphics card, each with the command that fixes it here: the Vulkan loader or drivers (for this Linux distribution's package manager), permission for Toskar's user to open the card, NVIDIA's driver, a Windows card's driver, or the CPU-only llama.cpp. `GET /api/v1/diagnostics/gpu` returns the same list (#317).
