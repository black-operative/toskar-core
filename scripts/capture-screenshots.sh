#!/usr/bin/env bash
# Build the web UI, serve demo data, and capture the README screenshots.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
created_workspaces=()
cleanup_workspaces() {
  if ((${#created_workspaces[@]})); then
    rm -f "${created_workspaces[@]}"
  fi
}
trap cleanup_workspaces EXIT

install_pnpm() {
  local dir="$1"
  local major
  major="$(cd /tmp && pnpm --version | cut -d. -f1)"
  if [[ "$major" -ge 10 && ! -f "$dir/pnpm-workspace.yaml" ]]; then
    printf '%s\n' 'packages:' '  - "."' 'allowBuilds:' '  esbuild: true' '  playwright: true' > "$dir/pnpm-workspace.yaml"
    created_workspaces+=("$dir/pnpm-workspace.yaml")
  fi
  (cd "$dir" && pnpm install)
}

if [[ ! -f web/dist/index.html ]]; then
  install_pnpm web
  (cd web && pnpm build)
fi

install_pnpm scripts/screenshots
# A self-hosted runner has Chromium's system libraries and no sudo.
if [[ "$(uname -s)" == "Linux" && "${RUNNER_ENVIRONMENT:-}" != "self-hosted" ]]; then
  (cd scripts/screenshots && pnpm exec playwright install --with-deps chromium)
else
  (cd scripts/screenshots && pnpm exec playwright install chromium)
fi

if [[ "${STORE_ONLY:-}" == "1" ]]; then
  export SCREENSHOT_FORMS="${SCREENSHOT_FORMS:-apple-iphone,apple-ipad,apple-mac}"
fi

node scripts/screenshots/capture.mjs

if [[ "${STORE_ONLY:-}" != "1" ]] && command -v ffmpeg >/dev/null 2>&1; then
  hold="${DEMO_HOLD_SEC:-3.2}"
  concat="$(mktemp)"
  python3 - "$ROOT" "$hold" "$concat" <<'PY'
import json, os, sys
root, hold, list_path = sys.argv[1:4]
cfg = json.load(open(os.path.join(root, "screenshots/config.json")))
paths = [os.path.join(root, "docs/screenshots", screen["filename"]) for screen in cfg["screens"]]
missing = [path for path in paths if not os.path.isfile(path)]
if missing:
    raise SystemExit("missing screenshot: " + ", ".join(missing))
lines = [f"file '{path}'\nduration {hold}\n" for path in paths]
lines.append(f"file '{paths[-1]}'\n")
open(list_path, "w").write("".join(lines))
PY
  ffmpeg -y -f concat -safe 0 -i "$concat" \
    -r 25 -an -c:v libx264 -pix_fmt yuv420p -movflags +faststart \
    docs/screenshots/demo.mp4
  ffmpeg -y -i docs/screenshots/demo.mp4 \
    -vf "fps=10,scale=960:-1:flags=lanczos,split[s0][s1];[s0]palettegen[p];[s1][p]paletteuse" \
    docs/screenshots/demo.gif
  rm -f "$concat" docs/screenshots/demo.webm
fi

if command -v magick >/dev/null 2>&1 || command -v convert >/dev/null 2>&1; then
  chmod +x scripts/process-screenshots.sh scripts/validate-screenshots.sh
  ./scripts/process-screenshots.sh
  ./scripts/validate-screenshots.sh
fi

echo "README screenshots: docs/screenshots"
mkdir -p docs/user-guide/images
for name in 07-automations.png 08-automations-create.png; do
  if [[ -f "docs/screenshots/$name" ]]; then
    cp "docs/screenshots/$name" docs/user-guide/images/
  fi
done
if [[ "${STORE_ONLY:-}" == "1" ]]; then
  echo "App Store screenshots: screenshots/appstore"
fi
if [[ -f docs/screenshots/demo.mp4 ]]; then
  echo "Demo video: docs/screenshots/demo.mp4"
fi
