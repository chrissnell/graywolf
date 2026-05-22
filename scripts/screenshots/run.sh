#!/usr/bin/env bash
# Orchestrates Android Play Store screenshot capture:
#   1. refresh web/dist so screenshots reflect the current UI
#   2. build a local graywolf binary (SPA embedded; no Rust modem needed)
#   3. launch graywolf in -demo mode (self-seeding; no tablet DB required)
#   4. run the Playwright harness (shoot.mjs) in Android mode
#   5. tear graywolf down
#
# Run via `make android-screenshots` from the repo root.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

PORT="${ANDROID_SS_PORT:-8088}"
WORK_DIR="scratch/ss-work"
BIN="scratch/gw-screenshots"
OUT="${GW_SCREENSHOT_OUT:-$WORK_DIR/shots}"

echo "==> Building SPA bundle (vite)"
( cd web && npx vite build >/dev/null )

echo "==> Building graywolf binary"
GOWORK=off go build -o "$BIN" ./cmd/graywolf

echo "==> Preparing work dir $WORK_DIR"
rm -rf "$WORK_DIR"
mkdir -p "$WORK_DIR/tiles"

echo "==> Launching graywolf on 127.0.0.1:$PORT"
GRAYWOLF_PLATFORM=desktop "$BIN" -demo \
  -config "$WORK_DIR/graywolf.db" \
  -history-db "$WORK_DIR/graywolf-history.db" \
  -tile-cache-dir "$WORK_DIR/tiles" \
  -http "127.0.0.1:$PORT" \
  -modem "" >"$WORK_DIR/gw.log" 2>&1 &
GW_PID=$!
# Always kill graywolf on exit, even if the harness fails.
trap 'kill "$GW_PID" 2>/dev/null || true' EXIT

echo "==> Waiting for HTTP listener"
for i in $(seq 1 30); do
  if curl -sf -o /dev/null "http://127.0.0.1:$PORT/api/auth/setup"; then
    break
  fi
  if ! kill -0 "$GW_PID" 2>/dev/null; then
    echo "ERROR: graywolf exited early; see $WORK_DIR/gw.log" >&2
    tail -20 "$WORK_DIR/gw.log" >&2
    exit 1
  fi
  sleep 1
done

echo "==> Capturing screenshots"
GW_SCREENSHOT_BASE="http://127.0.0.1:$PORT" \
GW_SCREENSHOT_OUT="$OUT" \
  node scripts/screenshots/shoot.mjs

echo "==> Done. Screenshots in $OUT/"
