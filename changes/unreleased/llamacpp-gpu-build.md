### Fixed

- On Linux and Windows, llama.cpp now installs its Vulkan build when the
  computer has a GPU with a Vulkan driver, instead of always the CPU-only
  build. Chats that took minutes on the CPU now run on the graphics card.
  Computers without a usable GPU still get the CPU build.
- A llama.cpp install that has the CPU build is swapped for the GPU build
  the next time Toskar starts, or when you install llama.cpp again from the
  Runtimes page.
- llama.cpp reports a GPU backend only when the installed build has one, so
  Toskar no longer says it can use the GPU when it cannot.
- Installing llama.cpp again now replaces the whole install. Before, a
  reinstall could leave the old build in place.
