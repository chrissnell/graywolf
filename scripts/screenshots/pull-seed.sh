#!/usr/bin/env bash
# Pull a snapshot of the connected tablet's graywolf DBs to use as seed
# data for screenshot generation. Requires a DEBUGGABLE build on the
# tablet (run-as only works on debug builds) and adb connectivity.
#
# Force-stops the app for a consistent snapshot, copies the config +
# history DBs, checkpoints their WAL into the main file, then relaunches.
#
# The pulled DBs land in scratch/screenshots-seed/ (gitignored -- they
# contain the operator's real callsign, heard-station history, and
# position data, which must not be committed).
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

PKG="com.nw5w.graywolf"
SEED_DIR="scratch/screenshots-seed"
mkdir -p "$SEED_DIR"

if ! adb shell run-as "$PKG" true 2>/dev/null; then
  echo "ERROR: run-as $PKG failed -- is a DEBUG build installed and adb connected?" >&2
  exit 1
fi

echo "==> Force-stopping $PKG for a consistent snapshot"
adb shell am force-stop "$PKG"
sleep 2

echo "==> Pulling DB files"
for f in \
  graywolf.db graywolf.db-shm graywolf.db-wal \
  graywolf-history.db graywolf-history.db-shm graywolf-history.db-wal; do
  if adb exec-out run-as "$PKG" cat "files/$f" >"$SEED_DIR/$f" 2>/dev/null; then
    echo "    pulled $f ($(wc -c <"$SEED_DIR/$f") bytes)"
  else
    echo "    (skip $f -- not present)"
    rm -f "$SEED_DIR/$f"
  fi
done

echo "==> Checkpointing WAL into the main DB files"
for db in "$SEED_DIR/graywolf.db" "$SEED_DIR/graywolf-history.db"; do
  [[ -f "$db" ]] && sqlite3 "$db" "PRAGMA wal_checkpoint(TRUNCATE);" >/dev/null || true
done

echo "==> Relaunching the app"
adb shell monkey -p "$PKG" -c android.intent.category.LAUNCHER 1 >/dev/null 2>&1 || true

echo "==> Seed ready in $SEED_DIR/"
