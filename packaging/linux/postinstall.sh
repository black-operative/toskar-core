#!/bin/sh
set -e
if ! getent passwd yggdrasil >/dev/null 2>&1; then
  useradd --system --home-dir /var/lib/yggdrasil --create-home --shell /usr/sbin/nologin yggdrasil
fi
# The service opens the GPU's render device (/dev/dri/renderD*), which the
# render group (video on older systems) may use (#317). Only groups this
# system has.
for group in render video; do
  if getent group "$group" >/dev/null 2>&1; then
    usermod -a -G "$group" yggdrasil || true
  fi
done
if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload || true
  systemctl enable toskar.service || true
  systemctl restart toskar.service || true
fi
