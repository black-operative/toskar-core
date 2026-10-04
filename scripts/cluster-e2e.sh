#!/usr/bin/env bash
# Three-node Bifrost cluster E2E (Docker Compose).
# With TOSKAR_STUB_INFERENCE, also runs the two-node Team demo
# (worker pinned to B, orchestration.role + one final chat answer).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if ! command -v docker >/dev/null 2>&1; then
  echo "docker is required for cluster e2e" >&2
  exit 1
fi
if ! docker info >/dev/null 2>&1; then
  echo "docker daemon is not running; start Docker Desktop (or dockerd) and retry" >&2
  exit 1
fi

COMPOSE=(docker compose -f docker-compose.cluster.yml)

cleanup() {
  "${COMPOSE[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "==> Building cluster images"
"${COMPOSE[@]}" build

echo "==> Running 3-node cluster e2e (+ Team stub demo)"
"${COMPOSE[@]}" up --abort-on-container-exit --exit-code-from driver

echo "==> Cluster e2e succeeded"
