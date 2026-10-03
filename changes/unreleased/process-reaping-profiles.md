### Fixed

- Stopping an MCP tool source ends the helpers it started, such as the `node` process `npx` runs, even when the server quits on its own. They used to keep running each time a source stopped.

### Added

- Diagnostic bundles include a runtime summary and goroutine and heap profiles, so a report of growing memory can be diagnosed. They hold function names, counts, and sizes, no prompts or files.
- `YGGDRASIL_PPROF=127.0.0.1:6060` serves Go's live profiles on this computer for developers; only loopback addresses are accepted. See Troubleshooting: "Yggdrasil uses more and more memory".
