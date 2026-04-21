#!/usr/bin/env bash
#
# gen_prev_release_db.sh — generate a configstore fixture DB from the
# previous release of graywolf, for use by TestMigrateFromPriorRelease.
#
# The "previous release" is detected dynamically from the newest `v*`
# tag reachable from HEAD. If HEAD itself carries a v* tag (i.e. you
# are sitting on a release commit), the search falls back to the
# parent commit so you get the version before the current one. This
# means the generator stays correct across releases without any edits.
#
# The output file is intentionally generic (prev_release.db). The
# fixture is NOT committed to git — it is regenerated on demand by CI
# and optionally by developers running the migration test locally.
# See graywolf/pkg/configstore/testdata/README.md.
#
# Usage:
#   ./scripts/testdata/gen_prev_release_db.sh [OUTPUT_PATH]
#
# Defaults to graywolf/pkg/configstore/testdata/prev_release.db
# relative to the repo root.
#
# Requirements:
#   - git worktree support (full clone; shallow clones without tags
#     will fail the tag lookup — see fetch-depth: 0 in .github/workflows)
#   - go, make, curl, jq installed
#   - Working network for the first-time module fetch of the previous
#     release's Go dependencies.
#
# Idempotent: re-running cleans up the previous worktree + temp config
# dir and rebuilds from scratch.

set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
DEFAULT_OUT="${REPO_ROOT}/graywolf/pkg/configstore/testdata/prev_release.db"
OUT_PATH="${1:-$DEFAULT_OUT}"
LISTEN_ADDR="127.0.0.1:${PORT:-38080}"

log() { printf '[gen_prev_release_db] %s\n' "$*" >&2; }
die() { log "ERROR: $*"; exit 1; }

# --- 0. Discover the previous release tag ------------------------------

# If HEAD itself is tagged v*, search starts from HEAD^ so we skip past
# the current release. Otherwise HEAD is fine — git describe walks back
# from there to the nearest v* tag.
HEAD_V_TAG="$(git -C "$REPO_ROOT" tag --points-at HEAD | grep -E '^v[0-9]' | head -n1 || true)"
if [[ -n "$HEAD_V_TAG" ]]; then
  SEARCH_START="HEAD^"
else
  SEARCH_START="HEAD"
fi

if ! TAG="$(git -C "$REPO_ROOT" describe --tags --abbrev=0 --match 'v[0-9]*' "$SEARCH_START" 2>/dev/null)"; then
  die "no previous v* tag found reachable from ${SEARCH_START}. Is this a shallow clone? Ensure fetch-depth: 0 in CI."
fi
log "detected previous release tag: $TAG"

WORKTREE="/tmp/graywolf-prev-release-${TAG}"
CFG_DIR="$(mktemp -d -t graywolf-prev-release-cfg.XXXXXX)"
BIN_PATH="${WORKTREE}/graywolf/bin/graywolf"
GRAYWOLF_PID=""

cleanup() {
  local code=$?
  if [[ -n "${GRAYWOLF_PID:-}" ]] && kill -0 "$GRAYWOLF_PID" 2>/dev/null; then
    log "stopping graywolf pid=$GRAYWOLF_PID"
    kill -TERM "$GRAYWOLF_PID" 2>/dev/null || true
    for _ in $(seq 1 20); do
      sleep 0.5
      kill -0 "$GRAYWOLF_PID" 2>/dev/null || break
    done
    kill -KILL "$GRAYWOLF_PID" 2>/dev/null || true
  fi
  rm -rf "$CFG_DIR"
  if [[ $code -eq 0 ]]; then
    git -C "$REPO_ROOT" worktree remove --force "$WORKTREE" 2>/dev/null || true
  else
    log "keeping worktree at $WORKTREE for debugging (cleanup with: git worktree remove --force $WORKTREE)"
  fi
}
trap cleanup EXIT

# --- 1. Worktree -------------------------------------------------------

if [[ -d "$WORKTREE" ]]; then
  log "removing stale worktree $WORKTREE"
  git -C "$REPO_ROOT" worktree remove --force "$WORKTREE" 2>/dev/null || rm -rf "$WORKTREE"
fi
log "creating worktree at $WORKTREE from tag $TAG"
git -C "$REPO_ROOT" worktree add "$WORKTREE" "$TAG"

# --- 2. Build graywolf -------------------------------------------------

log "building graywolf $TAG"
(cd "$WORKTREE" && make build)
if [[ ! -x "$BIN_PATH" ]]; then
  # Older tags may use a different make target; fall back to direct go build.
  log "make build did not produce $BIN_PATH; falling back to go build"
  (cd "$WORKTREE/graywolf" && go build -o bin/graywolf ./cmd/graywolf)
fi

# --- 3. Run graywolf against throwaway config dir ---------------------

mkdir -p "$CFG_DIR"
DB_PATH="$CFG_DIR/graywolf.db"
log "launching graywolf with DB $DB_PATH listening on $LISTEN_ADDR"
GRAYWOLF_DISABLE_AUTH=1 \
"$BIN_PATH" \
  --db "$DB_PATH" \
  --listen "$LISTEN_ADDR" \
  --log-level warn \
  >"$CFG_DIR/graywolf.log" 2>&1 &
GRAYWOLF_PID=$!

# Wait for health endpoint.
for i in $(seq 1 60); do
  if curl -sf "http://$LISTEN_ADDR/api/health" >/dev/null 2>&1; then
    log "graywolf is up (pid=$GRAYWOLF_PID)"
    break
  fi
  sleep 0.5
  if ! kill -0 "$GRAYWOLF_PID" 2>/dev/null; then
    log "graywolf exited early; log:"
    cat "$CFG_DIR/graywolf.log" >&2
    exit 1
  fi
done
if ! kill -0 "$GRAYWOLF_PID" 2>/dev/null; then
  log "graywolf not running after 30s; log:"
  cat "$CFG_DIR/graywolf.log" >&2
  exit 1
fi

# --- 4. Seed representative config via REST API -----------------------

API="http://$LISTEN_ADDR/api"

log "seeding audio device"
DEV_ID=$(curl -sf -X POST -H 'Content-Type: application/json' "$API/audio-devices" -d '{
  "name": "seed-mic",
  "direction": "input",
  "source_type": "flac",
  "device_path": "/tmp/seed.flac",
  "sample_rate": 44100,
  "channels": 1,
  "format": "s16le"
}' | jq -r '.id')
log "created audio device id=$DEV_ID"

# 3 channels
for n in 1 2 3; do
  curl -sf -X POST -H 'Content-Type: application/json' "$API/channels" -d "{
    \"name\": \"channel-$n\",
    \"input_device_id\": $DEV_ID,
    \"modem_type\": \"afsk\",
    \"bit_rate\": 1200,
    \"mark_freq\": 1200,
    \"space_freq\": 2200,
    \"profile\": \"A\",
    \"num_slicers\": 1,
    \"fix_bits\": \"none\"
  }" >/dev/null
done
log "created 3 channels"

# 2 KISS interfaces (both tcp-server, one in modem mode, one in tnc mode).
for pair in '1 modem tcp-server-1 127.0.0.1:8001' '2 tnc tcp-server-2 127.0.0.1:8002'; do
  set -- $pair
  ch="$1"; mode="$2"; name="$3"; addr="$4"
  curl -sf -X POST -H 'Content-Type: application/json' "$API/kiss" -d "{
    \"name\": \"$name\",
    \"type\": \"tcp\",
    \"listen_addr\": \"$addr\",
    \"channel\": $ch,
    \"broadcast\": true,
    \"enabled\": true,
    \"mode\": \"$mode\"
  }" >/dev/null
done
log "created 2 KISS interfaces"

# 3 beacons
for b in 1 2 3; do
  curl -sf -X POST -H 'Content-Type: application/json' "$API/beacons" -d "{
    \"type\": \"position\",
    \"channel\": $b,
    \"callsign\": \"N0CALL-$b\",
    \"destination\": \"APGRWO\",
    \"path\": \"WIDE1-1\",
    \"latitude\": 40.0,
    \"longitude\": -105.0,
    \"alt_ft\": 5280,
    \"symbol_table\": \"/\",
    \"symbol\": \">\",
    \"compress\": true,
    \"every_seconds\": 1800,
    \"slot_seconds\": -1,
    \"enabled\": true
  }" >/dev/null
done
log "created 3 beacons"

# 2 digipeater rules (both same-channel repeat on channel 1, distinct aliases).
for alias in WIDE TRACE; do
  curl -sf -X POST -H 'Content-Type: application/json' "$API/digipeater/rules" -d "{
    \"from_channel\": 1,
    \"to_channel\": 1,
    \"alias\": \"$alias\",
    \"alias_type\": \"widen\",
    \"max_hops\": 2,
    \"action\": \"repeat\",
    \"priority\": 100,
    \"enabled\": true
  }" >/dev/null
done
log "created 2 digipeater rules"

# 1 igate config (singleton PUT). software_version pinned to the tag so
# the fixture is self-documenting about which release produced it.
curl -sf -X PUT -H 'Content-Type: application/json' "$API/igate" -d "{
  \"enabled\": true,
  \"server\": \"rotate.aprs2.net\",
  \"port\": 14580,
  \"callsign\": \"N0CALL\",
  \"passcode\": \"-1\",
  \"gate_rf_to_is\": true,
  \"gate_is_to_rf\": false,
  \"rf_channel\": 1,
  \"max_msg_hops\": 2,
  \"software_name\": \"graywolf\",
  \"software_version\": \"${TAG#v}\",
  \"tx_channel\": 1
}" >/dev/null
log "upserted igate config"

# --- 5. Shutdown, copy, done ------------------------------------------

log "sending SIGTERM and waiting for clean exit"
kill -TERM "$GRAYWOLF_PID"
for _ in $(seq 1 60); do
  if ! kill -0 "$GRAYWOLF_PID" 2>/dev/null; then break; fi
  sleep 0.5
done
GRAYWOLF_PID=""  # suppress cleanup kill

mkdir -p "$(dirname "$OUT_PATH")"
cp "$DB_PATH" "$OUT_PATH"
log "wrote fixture from $TAG to $OUT_PATH ($(wc -c < "$OUT_PATH") bytes)"
