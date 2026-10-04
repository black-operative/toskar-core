#!/bin/sh
set -e
# Only when the package is removed: deb passes "remove", rpm passes 0. On an
# upgrade the new package's scripts restart the service.
case "$1" in
remove | 0)
  if command -v systemctl >/dev/null 2>&1; then
    systemctl disable --now toskar.service || true
  fi
  ;;
esac
