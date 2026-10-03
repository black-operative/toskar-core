### Fixed

- Checking email no longer leaves a background task behind for each IMAP connection. A daemon that checked mail on a schedule grew without limit.
- The browser tool's idle-session cleanup stops as soon as the last browser closes, including when Yggdrasil quits, instead of up to a minute later.

### Changed

- Every Go package's tests fail if they leave goroutines running (`internal/leakcheck`), so leaks like these are caught before they ship.
