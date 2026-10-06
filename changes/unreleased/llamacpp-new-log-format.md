### Fixed

- Running models say whether they're on the GPU again with current llama.cpp builds, which log the devices and offloaded layers only at trace verbosity and in a new format. Toskar now asks llama-server for that verbosity and reads the new format (#317).
