### Added

- Diagnostics shows how much memory Yggdrasil itself is using and its background tasks, with the last day as a small chart. If memory grows steadily for hours, it says so and suggests exporting diagnostics to report it.
- CI checks the web app for memory leaks by moving between every page many times, and a soak test (`YGGDRASIL_SOAK=1 go test ./tests/soak`) runs a throwaway daemon through hundreds of chats, cancellations, and event connections and fails if memory, goroutines, or open files keep growing.
