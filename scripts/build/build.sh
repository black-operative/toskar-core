#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

VERSION="${VERSION:-0.1.0-dev}"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
LDFLAGS="-X github.com/yeixio/toskar-core/internal/version.Version=${VERSION} -X github.com/yeixio/toskar-core/internal/version.Commit=${COMMIT} -X github.com/yeixio/toskar-core/internal/version.BuildDate=${DATE}"

mkdir -p bin dist
echo "Building frontend..."
(cd web && pnpm install --frozen-lockfile && pnpm build)

echo "Building daemon..."
GOOS="${GOOS:-$(go env GOOS)}" GOARCH="${GOARCH:-$(go env GOARCH)}" \
  go build -ldflags "$LDFLAGS" -o "bin/toskar" ./cmd/daemon

go build -ldflags "$LDFLAGS" -o "bin/toskarctl" ./cmd/devctl

echo "Artifacts in bin/ (version=${VERSION} commit=${COMMIT})"
