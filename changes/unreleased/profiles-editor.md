### Changed

- The profile editor opens across the whole page and is split into four
  tabs: Models, Tools, Knowledge and memory, and Strategy and computers. It
  was one long column inside a half-width card. Models shows the roles in
  use, with Show all roles for the rest; Tools uses switches, with per-tool
  permissions folded below them.

### Fixed

- Saving a profile without changing its tools no longer turns tools on. The
  editor showed every tool a profile didn't list as Always allow, though
  the daemon treats them as off, so saving gave the profile every tool,
  including the terminal and git push. It also kept tools from connected
  services that the editor doesn't list, which saving used to drop.
