#!/usr/bin/env bash
# Build the core release artifacts: Linux deb and rpm, macOS archives, and a Windows amd64 archive.
# Usage: VERSION=1.1.0-beta.3 ./scripts/build/package-core-release.sh
# Artifacts are not code-signed.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

VERSION="${VERSION:-0.1.0-dev}"
# Debian revision treats "-" as the package revision separator, so a
# pre-release such as 1.4.0-beta.1 is 1.4.0~beta.1. sed, not ${VERSION/-/~}:
# bash 5.2 tilde-expands that "~" to the home directory.
PKG_VERSION="$(printf '%s' "$VERSION" | sed 's/-/~/')"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
LDFLAGS="-X github.com/yeixio/toskar-core/internal/version.Version=${VERSION} -X github.com/yeixio/toskar-core/internal/version.Commit=${COMMIT} -X github.com/yeixio/toskar-core/internal/version.BuildDate=${DATE}"

if ! command -v nfpm >/dev/null 2>&1; then
  echo "nfpm is required. Install it with: go install github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.41.3" >&2
  exit 1
fi

if [[ ! -f web/dist/index.html ]]; then
  (cd web && pnpm install --frozen-lockfile && pnpm build)
fi

chmod +x packaging/linux/postinstall.sh packaging/linux/preremove.sh packaging/linux/start-after-rename.sh
rm -rf dist
mkdir -p dist

build_binaries() {
  local goos="$1" goarch="$2" dest="$3"
  mkdir -p "$dest/web"
  echo "Building ${goos}/${goarch}"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath -ldflags "$LDFLAGS" \
    -o "$dest/toskar" ./cmd/daemon
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath -ldflags "$LDFLAGS" \
    -o "$dest/toskarctl" ./cmd/devctl
  # The names from before the rename: launchd agents, scripts, and MCP
  # settings in other apps run them by path (#237).
  ln -s toskar "$dest/yggdrasil-daemon"
  ln -s toskarctl "$dest/yggctl"
  cp -R web/dist/. "$dest/web/"
  mkdir -p "$dest/completions"
  cp cmd/devctl/completions/* "$dest/completions/"
}

package_linux() {
  local goarch="$1" debarch="$2" rpmarch="$3"
  local stage="dist/stage-${debarch}"
  build_binaries linux "$goarch" "$stage"
  local cfg
  cfg="$(mktemp)"
  # Keep "~" out of the shell heredoc. On the release runner it expands to the home directory.
  {
    cat > "$cfg" <<EOF
name: toskar
arch: ${debarch}
platform: linux
EOF
    printf 'version: %s\n' "$PKG_VERSION" >> "$cfg"
    # The package was called yggdrasil before the rename (#237). It takes
    # over that package's files; rpm obsoletes it, so dnf upgrades to
    # toskar, and apt gets there through the transitional yggdrasil
    # package (package_transitional).
    # GPU support (#317): the Vulkan loader and Mesa's drivers for AMD and
    # Intel cards, vulkaninfo to check one is usable, and lspci to name it.
    # Recommended, so apt and dnf install them by default but a server can
    # leave them out. NVIDIA's driver is the user's to install.
    printf 'overrides:\n  deb:\n    replaces: ["yggdrasil (<< %s)"]\n    recommends: ["libvulkan1", "mesa-vulkan-drivers", "vulkan-tools", "pciutils"]\n  rpm:\n    replaces: ["yggdrasil < %s"]\n    provides: ["yggdrasil = %s"]\n    recommends: ["vulkan-loader", "mesa-vulkan-drivers", "vulkan-tools", "pciutils"]\ndeb:\n  breaks: ["yggdrasil (<< %s)"]\n' \
      "$PKG_VERSION" "$PKG_VERSION" "$PKG_VERSION" "$PKG_VERSION" >> "$cfg"
    cat >> "$cfg" <<EOF
maintainer: YEIXIO LLC <hello@yeix.io>
description: Local AI daemon and web UI
homepage: https://toskar.ai
license: AGPL-3.0-or-later
contents:
  - src: ${ROOT}/${stage}/toskar
    dst: /usr/bin/toskar
    file_info:
      mode: 0755
  - src: ${ROOT}/${stage}/toskarctl
    dst: /usr/bin/toskarctl
    file_info:
      mode: 0755
  # The names from before the rename (#237).
  - src: toskar
    dst: /usr/bin/yggdrasil-daemon
    type: symlink
  - src: toskarctl
    dst: /usr/bin/yggctl
    type: symlink
  - src: ${ROOT}/${stage}/web
    dst: /usr/share/yggdrasil/web
    type: tree
  - src: ${ROOT}/cmd/devctl/completions/toskarctl.bash
    dst: /usr/share/bash-completion/completions/toskarctl
  - src: ${ROOT}/cmd/devctl/completions/toskarctl.fish
    dst: /usr/share/fish/vendor_completions.d/toskarctl.fish
  # bash and fish load a command's completion by its name, so yggctl gets a
  # link to the same script, which completes both names.
  - src: toskarctl
    dst: /usr/share/bash-completion/completions/yggctl
    type: symlink
  - src: toskarctl.fish
    dst: /usr/share/fish/vendor_completions.d/yggctl.fish
    type: symlink
  # Debian and Fedora put packaged zsh completions in different fpath directories.
  - src: ${ROOT}/cmd/devctl/completions/_toskarctl
    dst: /usr/share/zsh/vendor-completions/_toskarctl
    packager: deb
  - src: ${ROOT}/cmd/devctl/completions/_toskarctl
    dst: /usr/share/zsh/site-functions/_toskarctl
    packager: rpm
  - src: ${ROOT}/packaging/linux/toskar.service
    dst: /usr/lib/systemd/system/toskar.service
scripts:
  postinstall: ${ROOT}/packaging/linux/postinstall.sh
  preremove: ${ROOT}/packaging/linux/preremove.sh
rpm:
  arch: ${rpmarch}
  scripts:
    # After the obsoleted yggdrasil package's own scripts have run.
    posttrans: ${ROOT}/packaging/linux/start-after-rename.sh
EOF
  }
  # File names keep the human version. "~" in a path expands to the runner home directory.
  nfpm package -p deb -f "$cfg" -t "dist/toskar_${VERSION}_${debarch}.deb"
  nfpm package -p rpm -f "$cfg" -t "dist/toskar-${VERSION}-1.${rpmarch}.rpm"
  rm -f "$cfg"
  rm -rf "$stage"
}

# package_transitional builds an empty yggdrasil deb that depends on toskar,
# so apt upgrade moves an install from before the rename over (#237). Its
# postinstall turns toskar.service back on after the old package's
# preremove has disabled it.
package_transitional() {
  local cfg
  cfg="$(mktemp)"
  {
    cat > "$cfg" <<EOF
name: yggdrasil
arch: all
platform: linux
EOF
    printf 'version: %s\n' "$PKG_VERSION" >> "$cfg"
    printf 'depends: ["toskar (>= %s)"]\n' "$PKG_VERSION" >> "$cfg"
    cat >> "$cfg" <<EOF
maintainer: YEIXIO LLC <hello@yeix.io>
description: Transitional package for toskar. Yggdrasil is now Toskar; this package can be removed.
homepage: https://toskar.ai
license: AGPL-3.0-or-later
section: oldlibs
contents:
  - src: ${ROOT}/packaging/linux/TRANSITIONAL.md
    dst: /usr/share/doc/yggdrasil/README
scripts:
  postinstall: ${ROOT}/packaging/linux/start-after-rename.sh
EOF
  }
  nfpm package -p deb -f "$cfg" -t "dist/yggdrasil_${VERSION}_all.deb"
  rm -f "$cfg"
}

package_darwin() {
  local goarch="$1"
  local name="toskar-${VERSION}-darwin-${goarch}-headless"
  local stage="dist/${name}"
  build_binaries darwin "$goarch" "$stage"
  tar -C dist -czf "dist/${name}.tar.gz" "$name"
  rm -rf "$stage"
}

package_windows() {
  local goarch="$1"
  local name="toskar-${VERSION}-windows-${goarch}-headless"
  local stage="dist/${name}"
  mkdir -p "$stage/web"
  echo "Building windows/${goarch}"
  CGO_ENABLED=0 GOOS=windows GOARCH="$goarch" go build -trimpath -ldflags "$LDFLAGS" \
    -o "$stage/toskar.exe" ./cmd/daemon
  CGO_ENABLED=0 GOOS=windows GOARCH="$goarch" go build -trimpath -ldflags "$LDFLAGS" \
    -o "$stage/toskarctl.exe" ./cmd/devctl
  # No links in a Windows archive: install.ps1 adds the old names (#237).
  cp -R web/dist/. "$stage/web/"
  tar -C dist -czf "dist/${name}.tar.gz" "$name"
  rm -rf "$stage"
}

# ONLY builds one target, such as linux-amd64, for the installer test in CI.
want() { [[ -z "${ONLY:-}" || "${ONLY}" == "$1" ]]; }
if want linux-amd64; then package_linux amd64 amd64 x86_64; fi
if want linux-arm64; then package_linux arm64 arm64 aarch64; fi
if want linux-amd64 || want linux-arm64; then package_transitional; fi
if want darwin-arm64; then package_darwin arm64; fi
if want darwin-amd64; then package_darwin amd64; fi
if want windows-amd64; then package_windows amd64; fi

(
  cd dist
  : > SHA256SUMS.txt
  shopt -s nullglob
  for file in *.deb *.rpm *.tar.gz; do
    if command -v sha256sum >/dev/null 2>&1; then
      hash="$(sha256sum "$file" | awk '{print $1}')"
    else
      hash="$(shasum -a 256 "$file" | awk '{print $1}')"
    fi
    printf '%s  %s\n' "$hash" "$file" >> SHA256SUMS.txt
  done
)
echo "Core packages are in dist/"
ls -lh dist
