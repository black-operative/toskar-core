#!/usr/bin/env bash
# Build an unsigned apt repository from the deb files in dist/.
# The result is a directory CI publishes as the apt branch.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

if ! command -v dpkg-scanpackages >/dev/null 2>&1; then
  echo "dpkg-scanpackages is required (apt/dpkg-dev)." >&2
  exit 1
fi
if ! command -v apt-ftparchive >/dev/null 2>&1; then
  echo "apt-ftparchive is required (apt-utils)." >&2
  exit 1
fi

OUT="${1:-dist/apt}"
rm -rf "$OUT"
mkdir -p "$OUT/pool/main"
cp dist/*.deb "$OUT/pool/main/"

for arch in amd64 arm64; do
  dest="dists/stable/main/binary-${arch}"
  mkdir -p "$OUT/$dest"
  (
    cd "$OUT"
    dpkg-scanpackages -a "$arch" pool/main /dev/null > "$dest/Packages"
    gzip -9c "$dest/Packages" > "$dest/Packages.gz"
  )
done

# Origin and Label keep the names from before the rename (#237): apt refuses
# to update from a repository whose Origin or Label changed until each person
# accepts it with --allow-releaseinfo-change.
cat > "$OUT/dists/stable/Release" <<EOF
Origin: Yggdrasil
Label: Yggdrasil
Suite: stable
Codename: stable
Architectures: amd64 arm64
Components: main
Description: Toskar core packages
EOF
apt-ftparchive release "$OUT/dists/stable" >> "$OUT/dists/stable/Release"
echo "Apt repository: $OUT"
