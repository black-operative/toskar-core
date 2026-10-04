#!/usr/bin/env bash
# Build a headless (daemon + web UI) package for GOOS/GOARCH.
# Usage: VERSION=0.1.0 GOOS=linux GOARCH=amd64 ./scripts/build/package-headless.sh
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

VERSION="${VERSION:-0.1.0-dev}"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
GOOS="${GOOS:-$(go env GOOS)}"
GOARCH="${GOARCH:-$(go env GOARCH)}"
EXT=""
if [[ "${GOOS}" == "windows" ]]; then
  EXT=".exe"
fi
LDFLAGS="-X github.com/yeixio/yggdrasil-core/internal/version.Version=${VERSION} -X github.com/yeixio/yggdrasil-core/internal/version.Commit=${COMMIT} -X github.com/yeixio/yggdrasil-core/internal/version.BuildDate=${DATE}"

NAME="toskar-${VERSION}-${GOOS}-${GOARCH}-headless"
OUT_DIR="dist/${NAME}"
rm -rf "${OUT_DIR}"
mkdir -p "${OUT_DIR}/web" bin dist

if [[ ! -f web/dist/index.html ]]; then
  echo "Building frontend..."
  (cd web && pnpm install --frozen-lockfile && pnpm build)
fi

echo "Building headless daemon (${GOOS}/${GOARCH})..."
export CGO_ENABLED="${CGO_ENABLED:-0}"
GOOS="${GOOS}" GOARCH="${GOARCH}" go build -trimpath -ldflags "${LDFLAGS}" \
  -o "${OUT_DIR}/toskar${EXT}" ./cmd/daemon
GOOS="${GOOS}" GOARCH="${GOARCH}" go build -trimpath -ldflags "${LDFLAGS}" \
  -o "${OUT_DIR}/toskarctl${EXT}" ./cmd/devctl
# The names from before the rename, for setups that run them by path (#237).
if [[ "${GOOS}" != "windows" ]]; then
  ln -s toskar "${OUT_DIR}/yggdrasil-daemon"
  ln -s toskarctl "${OUT_DIR}/yggctl"
fi

cp -R web/dist/. "${OUT_DIR}/web/"

if [[ "${GOOS}" != "windows" ]]; then
  mkdir -p "${OUT_DIR}/completions"
  cp cmd/devctl/completions/* "${OUT_DIR}/completions/"
fi

# Linux desktop integration assets (hicolor + .desktop)
if [[ "${GOOS}" == "linux" ]]; then
  mkdir -p "${OUT_DIR}/share/applications"
  cp packaging/linux/yggdrasil.desktop "${OUT_DIR}/share/applications/"
  if [[ -d assets/brand/generated/linux ]]; then
    mkdir -p "${OUT_DIR}/share/icons/hicolor"
    cp -R assets/brand/generated/linux/. "${OUT_DIR}/share/icons/hicolor/"
  fi
fi

cat > "${OUT_DIR}/README.txt" <<EOF
Yggdrasil ${VERSION} — headless / web-only package (${GOOS}/${GOARCH})

This build runs the control-plane daemon and serves the web UI. There is no
native desktop window; open a browser after starting the daemon.

Start:
  ./toskar${EXT}

Then open:
  http://127.0.0.1:7331

Optional:
  TOSKAR_WEB_UI_DIR=./web ./toskar${EXT}
  TOSKAR_API_HOST=0.0.0.0 ./toskar${EXT}   # LAN bind (explicit)

Shell completion for toskarctl (bash, zsh, fish):
  completions/   # or print one with: ./toskarctl completion <bash|zsh|fish>

yggdrasil-daemon and yggctl, the names from before the rename, still work.

Linux icon theme (if included):
  share/icons/hicolor/<size>/apps/yggdrasil.png
  share/applications/yggdrasil.desktop   # Icon=yggdrasil

Commit: ${COMMIT}
Built:  ${DATE}
EOF

rm -f "dist/${NAME}.tar.gz" "dist/${NAME}.zip"
if [[ "${GOOS}" == "windows" ]]; then
  if command -v zip >/dev/null 2>&1; then
    (cd dist && zip -r "${NAME}.zip" "${NAME}")
  else
    powershell.exe -NoProfile -Command \
      "Compress-Archive -Path 'dist/${NAME}' -DestinationPath 'dist/${NAME}.zip' -Force"
  fi
  echo "Created dist/${NAME}.zip"
else
  tar -C dist -czf "dist/${NAME}.tar.gz" "${NAME}"
  echo "Created dist/${NAME}.tar.gz"
fi
