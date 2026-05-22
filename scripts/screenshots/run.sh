#!/usr/bin/env bash
# Orchestrates Android Play Store screenshot capture:
#   1. refresh web/dist so screenshots reflect the current UI
#   2. build a local graywolf binary (SPA embedded; no Rust modem needed)
#   3. stage a copy of the seed DBs (originals stay pristine)
#   4. launch graywolf against the seed on a test port
#   5. run the Playwright harness (shoot.mjs) in Android mode
#   6. tear graywolf down
#
# Seed DBs come from a real tablet snapshot; refresh them with
# scripts/screenshots/pull-seed.sh while the tablet is on adb.
#
# Run via `make android-screenshots` from the repo root.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

PORT="${ANDROID_SS_PORT:-8088}"
SEED_DIR="scratch/screenshots-seed"
WORK_DIR="scratch/ss-work"
BIN="scratch/gw-screenshots"
OUT="${GW_SCREENSHOT_OUT:-$WORK_DIR/shots}"

if [[ ! -f "$SEED_DIR/graywolf.db" ]]; then
  echo "ERROR: no seed DB at $SEED_DIR/graywolf.db" >&2
  echo "Run 'make android-screenshots-seed' with the tablet on adb first." >&2
  exit 1
fi

echo "==> Building SPA bundle (vite)"
( cd web && npx vite build >/dev/null )

echo "==> Building graywolf binary"
GOWORK=off go build -o "$BIN" ./cmd/graywolf

echo "==> Staging seed DBs into $WORK_DIR"
rm -rf "$WORK_DIR"
mkdir -p "$WORK_DIR/tiles"
cp "$SEED_DIR/graywolf.db" "$WORK_DIR/graywolf.db"
cp "$SEED_DIR/graywolf-history.db" "$WORK_DIR/graywolf-history.db"

echo "==> Launching graywolf on 127.0.0.1:$PORT"
GRAYWOLF_PLATFORM=desktop "$BIN" \
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
