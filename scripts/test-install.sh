#!/usr/bin/env bash
# CI test of scripts/install.sh on Ubuntu with systemd (#40): install the
# .deb from a local release, check the service runs, then install-and-join
# a second computer's network with the one-line command a join token gives.
#
# Run on a disposable Linux runner: it installs a package and a service.
# Usage: ./scripts/test-install.sh   (after ONLY=linux-amd64 package-core-release.sh)
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

[[ "$(uname -s)" == "Linux" ]] || { echo "test-install.sh runs on Linux" >&2; exit 1; }
ls dist/*.deb dist/SHA256SUMS.txt >/dev/null

work="$(mktemp -d)"
cleanup() {
	if [[ -n "${http_pid:-}" ]]; then kill "$http_pid" 2>/dev/null || true; fi
	if [[ -n "${issuer_pid:-}" ]]; then kill "$issuer_pid" 2>/dev/null || true; fi
}
trap cleanup EXIT

python3 -m http.server 18090 --bind 127.0.0.1 --directory dist >"$work/http.log" 2>&1 &
http_pid=$!
for _ in $(seq 1 20); do curl -fsS -o /dev/null http://127.0.0.1:18090/SHA256SUMS.txt && break; sleep 0.5; done
# The old name on purpose: install.sh still reads the names from before the
# rename (#237); toskarctl below gets the new one.
export YGGDRASIL_RELEASE_URL=http://127.0.0.1:18090

echo "== install"
sh scripts/install.sh
systemctl is-active --quiet toskar.service || { sudo journalctl -u toskar --no-pager | tail -50; exit 1; }
# The name from before the rename is an alias (#237).
systemctl is-active --quiet yggdrasil.service
toskarctl version | head -1
# The names from before the rename still run (#237).
yggctl version | head -1
yggdrasil-daemon -version | head -1
curl -fsS http://127.0.0.1:7331/api/v1/health

echo "== installing again leaves it running"
sh scripts/install.sh | grep -q "already installed and running"

echo "== the issuer"
mkdir -p "$work/issuer"
cat >"$work/issuer/config.json" <<EOF
{"data_dir":"$work/issuer","models_dir":"$work/issuer/models","runtimes_dir":"$work/issuer/runtimes","logs_dir":"$work/issuer/logs",
 "api_host":"127.0.0.1","api_port":17431,"internal_port":17432,"discovery_enabled":true,"node_name":"issuer"}
EOF
stage="$(mktemp -d)"
dpkg-deb -x dist/toskar_*.deb "$stage"
"$stage/usr/bin/toskar" --data-dir "$work/issuer" >"$work/issuer.log" 2>&1 &
issuer_pid=$!
for _ in $(seq 1 60); do curl -fsS -o /dev/null http://127.0.0.1:17431/api/v1/health && break; sleep 0.5; done

token="$(curl -fsS -X POST http://127.0.0.1:17431/api/v1/join-tokens)"
install_command="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["install_command"])' <<<"$token")"
join_args="${install_command#* join }"
echo "== install-and-join"
# shellcheck disable=SC2086 # the join command's words
sh scripts/install.sh join $join_args | tee "$work/join.txt"
grep -q "Joined the Yggdrasil network" "$work/join.txt"

echo "== the issuer sees it"
sleep 3
TOSKAR_URL=http://127.0.0.1:17431 toskarctl network | tee "$work/network.txt"
grep -q "Paired computers" "$work/network.txt"

echo "== running the command again says it already joined"
# shellcheck disable=SC2086
if sh scripts/install.sh join $join_args >"$work/again.txt" 2>&1; then
	grep -q "already joined" "$work/again.txt"
else
	cat "$work/again.txt"
	exit 1
fi
echo "installer test passed"
