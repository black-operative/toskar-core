# Development

## Prerequisites

- Go 1.26.3 or newer (`go.mod`)
- Node.js 22
- pnpm 9 (the root `package.json` pins `pnpm@9.15.0`)
- Git

Linux package builds also need `nfpm` 2.41.3, as used by the release workflow. Day-to-day daemon work does not.

## Setup

```bash
git clone https://github.com/yeixio/toskar-core.git
cd toskar-core
make start
```

`make` with no target prints `make help`. `make start` installs web dependencies, writes `web/dist`, builds `bin/toskar` and `bin/toskarctl` (`bin/toskar.exe` and `bin/toskarctl.exe` on Windows), and runs the daemon with `TOSKAR_WEB_UI_DIR` set to `web/dist`. Open `http://127.0.0.1:7331`.

On Windows, `make start`, `make ui`, `make daemon`, and `make frontend` work from PowerShell, cmd, or Git Bash, with Go, Node.js, pnpm, and GNU make on `PATH` (`winget install ezwinports.make`). Without `sh` on `PATH`, make runs recipes in `cmd.exe`. The other targets use POSIX tools such as `find`, `awk`, `chmod`, and `VAR=value command`, so run them from Git Bash.

`make daemon` stamps the current git commit into both binaries. A build from this tree reports `0.1.0-dev` unless `-ldflags` sets `internal/version.Version`. `toskarctl version` and `toskar -version` print the license and the corresponding-source URL. `toskarctl completion <bash|zsh|fish>` prints the completion scripts in `cmd/devctl/completions/`, which the packages also install. Update those scripts when you add a `toskarctl` command. Release packaging sets the version as well, so a tagged build points at `tree/v<version>`.

A fork that serves a modified daemon over the network sets its own source URL at build time:

```text
-X github.com/yeixio/toskar-core/internal/version.SourceURL=<url-of-your-corresponding-source>
```

## Run

```bash
make start
```

That is the same as `make run-daemon`. It rebuilds `web/dist` and the binaries, then serves the UI from `web/dist`.

Optional data directory, after `make daemon`:

```bash
TOSKAR_WEB_UI_DIR="$PWD/web/dist" ./bin/toskar -data-dir "$PWD/.ygg-dev-data"
```

`make ui` only builds the web UI. `make frontend` does that and runs the web tests. The Vite dev server is separate and proxies `/api` and `/v1` to the daemon:

```bash
make run-web
```

That listens on `http://127.0.0.1:5173`. The daemon API stays on port 7331.

## Test

```bash
make test
make vet
```

`make test` is `go test ./...`. Every package under `internal/` runs `leakcheck.Main` from its `TestMain`: when its tests pass, it waits up to five seconds for goroutines they started to finish and fails the package if any are still running, printing their stacks. A new package with tests adds `func TestMain(m *testing.M) { leakcheck.Main(m) }` (see `internal/leakcheck`); a goroutine meant to live for the whole process is named with `leakcheck.Main(m, "pkg.longLived")`. `make ci` runs format, vet, tests, and the frontend job locally. A push to `main` publishes the README coverage badge from `go test ./... -coverprofile`. CI does not enforce a coverage percentage.

Web tests alone:

```bash
cd web && pnpm test
```

Accessibility check (axe-core in Chromium, over every page with demo data, in both themes at desktop and phone widths; build `web/dist` first):

```bash
cd scripts/screenshots && pnpm install && pnpm exec playwright install chromium && cd ../..
node scripts/screenshots/a11y.mjs
A11Y_PAGES=/chat,/settings node scripts/screenshots/a11y.mjs
```

Any WCAG 2.2 A or AA violation, best-practice violation, or page error fails it, with the element and the reason. The rules new UI follows (tokens, states, keyboard patterns, writing) are in [Design](design.md).

Leak checks (#231):

```bash
node scripts/screenshots/leaks.mjs                               # web UI, after building web/dist; also in CI
TOSKAR_SOAK=1 go test ./tests/soak -run TestSoak -v           # daemon, about 5 minutes
TOSKAR_SOAK=1 TOSKAR_SOAK_ROUNDS=1000 go test ./tests/soak -v -timeout 60m
```

The web check moves between every page many times in one browser and fails if the heap, DOM nodes, or event listeners keep growing after warm-up, or if a page throws. The soak test runs a throwaway daemon (its own data directory, free ports, the stub model) through chats, cancelled streams, stopped turns, and event subscribers, and fails if goroutines, heap, or open files keep growing.

Cluster check (Docker, stub inference, not a GPU test):

```bash
make test-cluster
```

Documentation snapshots:

```bash
python3 scripts/test_publish_docs.py
python3 scripts/publish-docs.py --destination /tmp/ygg-docs-check \
  --version 0.0.0-test --commit "$(git rev-parse HEAD)"
```

## Format and lint

Go formatting is `gofmt` via `make fmt`. CI fails if a Go file outside `web/`, `vendor/`, and `node_modules/` is not `gofmt`-clean.

`make lint` runs golangci-lint v2.14.0 (`.golangci.yml`: errcheck, govet, ineffassign, staticcheck, unused) and `pnpm lint` in `web/`. CI uses the same golangci-lint version. Install it with the upstream install script, or `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0`. `go vet ./...` still runs on its own.

The web check in CI is `pnpm lint`, `pnpm exec tsc -b --pretty false`, `pnpm test`, and `pnpm build`. ESLint covers `web/src` and `web/vite.config.ts`. Generated files in `web/wailsjs` are ignored. Hook rules are `rules-of-hooks` and `exhaustive-deps`. Warnings do not fail the job.

## CI

[`.github/workflows/ci.yml`](../.github/workflows/ci.yml) runs on pull requests and on pushes to `main`, on Ubuntu, in two jobs:

- **go:** user-guide publish checks; changelog fragment checks; `gofmt`, `go vet`, golangci-lint, `go test ./...`; then cross-compiles of `toskar` and `toskarctl` for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, and windows/amd64 (`CGO_ENABLED=0`). These share one runner, so the module and build caches are reused.
- **frontend:** web lint, typecheck, test, and build.
- **accessibility:** builds the web UI and runs `scripts/screenshots/a11y.mjs` (axe-core in Chromium) over every page, in both themes at 1440 and 390 px wide, then `scripts/screenshots/leaks.mjs` (the web leak check). New UI has to meet WCAG 2.2 AA: 4.5:1 text contrast, labeled controls, and headings in order.

To keep runs short:

- **Prose-only changes skip CI:** a change that touches only Markdown, `LICENSE`, `NOTICE`, screenshots, brand assets, or issue templates doesn't run it. The user guide JSON is still checked.
- **Superseded runs are cancelled:** a new push to a pull request cancels its older run. Runs on `main` always finish.

On `main`, a third job publishes the coverage badge.

[`.github/workflows/security.yml`](../.github/workflows/security.yml) runs `govulncheck ./...` and `pnpm audit --prod` in `web/`. The audit step does not fail the job (`|| true`). It runs every Monday, and on pushes and pull requests that change `go.mod`, `go.sum`, `web/package.json`, or `web/pnpm-lock.yaml`.

[`.github/workflows/release.yml`](../.github/workflows/release.yml) runs on tags matching `v*`. It builds packages, writes `SHA256SUMS.txt`, publishes a GitHub Release, and freezes `docs/<version>.json` from `docs/user-guide/guide.json`. Stable tags also update the Homebrew formula and the apt repository. It then tells `toskar-apps` about the release (see [the release checklist](release-checklist.md)). It does not sign binaries.

[`.github/workflows/screenshots.yml`](../.github/workflows/screenshots.yml) recaptures `docs/screenshots` from demo data, then holds those stills into `demo.mp4` and `demo.gif`. The same run writes iPhone, iPad, and Google Play phone and tablet canvases under `screenshots/appstore/`. The Release workflow runs it after each release is published and attaches the README stills and `demo.mp4` to the release as `screenshot-<name>`. toskar.ai shows the ones attached to the latest release. Start it by hand with a tag to attach them to an existing release, or without one to only capture; either way the files are also kept as a workflow artifact.

Dependabot is configured for Go modules, the web and screenshot npm trees, and GitHub Actions.

## Labels

Issue labels are defined in [`.github/labels.yml`](../.github/labels.yml). GitHub does not create them from that file. Maintainers synchronize them with `./scripts/sync-github-labels.sh`. CI does not run that script.

## Release flow

Tag `v*` → release workflow → Linux `.deb` and `.rpm` (amd64 and arm64), macOS headless archives (arm64 and amd64), Windows amd64 headless archive, `SHA256SUMS.txt` → GitHub Release.

The same job opens a Homebrew formula pull request, updates the `apt` branch, and opens a documentation snapshot pull request. The release token cannot approve those pull requests, so a required review leaves them open and does not fail the release. Signing is not part of this workflow. The checklist is [release-checklist.md](release-checklist.md).

## Changelog and shared files

A pull request does not edit `CHANGELOG.md`. It adds a fragment under [`changes/unreleased/`](../changes/unreleased/README.md), a small Markdown file with `### Added`, `### Changed`, or `### Fixed` (and so on) and one bullet per change. Two pull requests never touch the same fragment, so they cannot conflict over the changelog. `python3 scripts/changelog.py preview` shows the next release's section; the release preparation runs `python3 scripts/changelog.py release <version>`, which writes it into `CHANGELOG.md` and removes the fragments.

A new minor release also updates [`site/highlights.json`](../site/highlights.json), the few highlights toskar.ai shows as "New in 1.6". `scripts/changelog.py check` fails until its `version` matches the latest stable release in `CHANGELOG.md`, so the site never shows an older release's news.

Other files many pull requests touch merge cleanly when each change goes next to related lines rather than at the end of the file:

- **`api/openapi.yaml`:** add a path beside the other paths for the same area (all the `/api/v1/notifications/...` paths together), and a schema beside related schemas, not at the end of `components`.
- **`docs/user-guide/guide.json`:** change the section the feature belongs to, not the last section.
- **`web/src/types/api.ts` and `web/src/lib/api.ts`:** add a type or method beside the ones for the same feature.

## Conventions

This repository does not include an `AGENTS.md`. Match the package you are editing. Keep handlers thin and put behavior in `internal/`. Do not add license headers file by file. The project license is the root `LICENSE`.
