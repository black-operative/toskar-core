#!/usr/bin/env bash
# Runs the quality set (tests/quality) against a real model, on CPU.
#
#   scripts/quality-model.sh serve   start llama-server with the model, in the foreground
#   scripts/quality-model.sh run     start it, run core's cases against it, and stop it
#
# llama.cpp comes from its GitHub release LLAMA_CPP_TAG (gh must be signed
# in), and the model from QUALITY_MODEL_GGUF_URL, both kept in
# QUALITY_CACHE. The default model is Llama 3.2 1B, the smallest the
# iPhone app offers, so a release is held to what the weakest model can do.
# The iPhone app's quality run (yeixio/toskar-desktop) uses `serve`.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CACHE="${QUALITY_CACHE:-$ROOT/.quality-cache}"
TAG="${LLAMA_CPP_TAG:-b11174}"
MODEL_URL="${QUALITY_MODEL_GGUF_URL:-https://huggingface.co/bartowski/Llama-3.2-1B-Instruct-GGUF/resolve/main/Llama-3.2-1B-Instruct-Q4_K_M.gguf}"
PORT="${QUALITY_MODEL_PORT:-8089}"
CTX="${QUALITY_MODEL_CTX:-4096}"
mkdir -p "$CACHE"

llama_server() {
  local dir="$CACHE/llama.cpp-$TAG"
  local bin
  bin="$(find "$dir" -type f -name llama-server 2>/dev/null | head -n 1 || true)"
  if [[ -z "$bin" ]]; then
    rm -rf "$dir"
    mkdir -p "$dir"
    gh release download "$TAG" -R ggml-org/llama.cpp -p "*bin-ubuntu-x64.*" -D "$dir" >&2
    for archive in "$dir"/*; do
      case "$archive" in
        *.zip) unzip -q "$archive" -d "$dir" ;;
        *.tar.gz | *.tgz) tar -xzf "$archive" -C "$dir" ;;
      esac
    done
    bin="$(find "$dir" -type f -name llama-server | head -n 1)"
    chmod +x "$bin"
  fi
  echo "$bin"
}

model() {
  local file="$CACHE/$(basename "${MODEL_URL%%\?*}")"
  if [[ ! -s "$file" ]]; then
    curl -fL --retry 3 -o "$file.part" "$MODEL_URL" >&2
    mv "$file.part" "$file"
  fi
  echo "$file"
}

serve() {
  local bin gguf
  bin="$(llama_server)"
  gguf="$(model)"
  # The release archive keeps its shared libraries beside llama-server.
  export LD_LIBRARY_PATH="$(dirname "$bin")${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
  exec "$bin" -m "$gguf" -c "$CTX" --host 127.0.0.1 --port "$PORT" --jinja -np 1
}

wait_ready() {
  for _ in $(seq 1 180); do
    if curl -fs "http://127.0.0.1:$PORT/health" >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  echo "llama-server did not start" >&2
  return 1
}

case "${1:-run}" in
  serve)
    serve
    ;;
  wait)
    wait_ready
    ;;
  run)
    serve > "$CACHE/llama-server.log" 2>&1 &
    pid=$!
    trap 'kill "$pid" 2>/dev/null || true' EXIT
    wait_ready || { tail -n 50 "$CACHE/llama-server.log" >&2; exit 1; }
    cd "$ROOT"
    TOSKAR_QUALITY_MODEL_URL="http://127.0.0.1:$PORT" \
      TOSKAR_QUALITY_REPORT="${TOSKAR_QUALITY_REPORT:-$CACHE/quality-report.md}" \
      go test ./tests/quality -run TestQualitySet -count=1 -v -timeout 120m
    ;;
  *)
    echo "usage: $0 serve|wait|run" >&2
    exit 2
    ;;
esac
