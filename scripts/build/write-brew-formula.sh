#!/usr/bin/env bash
# Write Formula/toskar.rb from the macOS archives in dist/SHA256SUMS.txt.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
VERSION="${VERSION:?VERSION is required}"
TAG="v${VERSION#v}"
SUMS="${ROOT}/dist/SHA256SUMS.txt"
TMPL="${ROOT}/packaging/homebrew/toskar.rb.tmpl"
OUT="${ROOT}/Formula/toskar.rb"

arm_file="yggdrasil-${VERSION}-darwin-arm64-headless.tar.gz"
amd_file="yggdrasil-${VERSION}-darwin-amd64-headless.tar.gz"
arm_sha="$(awk -v f="$arm_file" '{ name=$2; sub(/^\.\//, "", name); if (name == f) print $1 }' "$SUMS")"
amd_sha="$(awk -v f="$amd_file" '{ name=$2; sub(/^\.\//, "", name); if (name == f) print $1 }' "$SUMS")"
if [[ -z "$arm_sha" || -z "$amd_sha" ]]; then
  echo "Missing macOS checksums in $SUMS" >&2
  exit 1
fi

mkdir -p "${ROOT}/Formula"
sed \
  -e "s/VERSION/${VERSION}/g" \
  -e "s/TAG/${TAG}/g" \
  -e "s/ARM64_SHA/${arm_sha}/g" \
  -e "s/AMD64_SHA/${amd_sha}/g" \
  "$TMPL" > "$OUT"
echo "Wrote $OUT"
