#!/bin/sh
# Runs after the package that replaced yggdrasil has been set up: the deb
# transitional yggdrasil package's postinstall, and the rpm's posttrans.
# The yggdrasil package from before the rename disables yggdrasil.service
# when it is upgraded or removed, which by then is toskar.service's alias,
# so this turns the service back on (#237).
set -e
if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload || true
  systemctl enable toskar.service || true
  systemctl restart toskar.service || true
fi
