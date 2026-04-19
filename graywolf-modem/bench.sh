#!/bin/bash
#
# WA8LMF AFSK benchmark: runs the real graywolf-modem binary through its IPC
# pipeline against Direwolf atest -P AD+ and reports both counts side by side.
# This exercises the production path (flac_fast → DevicePipeline → ensemble
# demodulator → IPC ReceivedFrame), not just the DSP library, so the numbers
# match what an operator sees at runtime.
#
# Usage: ./bench.sh <audio-file> [iterations] [ensemble]
#   audio-file  FLAC (preferred) or WAV. FLAC feeds both binaries; WAV-only
#               inputs are passed straight to Direwolf and converted for us.
#   iterations  Number of head-to-head runs to perform (default 3).
#   ensemble    Pass-through to graywolf-modem: "" (default = triple),
#               "single", "dual", "triple".

set -e

AUDIO_FILE="${1:?Usage: bench.sh <audio-file> [iterations] [ensemble]}"
ITERATIONS="${2:-3}"
ENSEMBLE="${3:-}"

RUST_DIR="$(cd "$(dirname "$0")" && pwd)"
DIREWOLF_DIR="${DIREWOLF_DIR:-$RUST_DIR/../direwolf}"

# --- Resolve FLAC / WAV inputs ---
# graywolf-modem's flac_fast source reads .flac directly at max speed.
# Direwolf's atest wants .wav, so convert if needed.
if [[ "$AUDIO_FILE" == *.flac ]]; then
    FLAC_FILE="$AUDIO_FILE"
    WAV_FILE="${AUDIO_FILE%.flac}.wav"
    if [ ! -f "$WAV_FILE" ]; then
        command -v ffmpeg >/dev/null 2>&1 || { echo "ffmpeg required for FLAC→WAV conversion"; exit 1; }
        echo "Converting $AUDIO_FILE → $WAV_FILE"
        ffmpeg -y -i "$AUDIO_FILE" -acodec pcm_s16le -map_metadata -1 -fflags +bitexact "$WAV_FILE" 2>/dev/null
    fi
else
    WAV_FILE="$AUDIO_FILE"
    FLAC_FILE="${AUDIO_FILE%.wav}.flac"
    if [ ! -f "$FLAC_FILE" ]; then
        echo "NOTE: $AUDIO_FILE is WAV only; graywolf-modem's flac_fast source needs .flac"
        echo "      Will use the WAV through a temporary conversion for the bench."
        FLAC_FILE="$(mktemp -t bench.XXXXX.flac)"
        ffmpeg -y -i "$AUDIO_FILE" -c:a flac "$FLAC_FILE" 2>/dev/null
    fi
fi

# --- Build both sides ---
echo "Building Direwolf (atest)..."
if [ -d "$DIREWOLF_DIR/build" ]; then
    (cd "$DIREWOLF_DIR/build" && make atest 2>/dev/null) || echo "Warning: Direwolf build failed"
fi

echo "Building Graywolf (graywolf-modem + demod-ipc-bench)..."
(cd "$RUST_DIR" && RUSTFLAGS="-C target-cpu=native" \
    cargo build --release --bin graywolf-modem --bin demod-ipc-bench 2>/dev/null)

# --- Find the binaries ---
TARGET_DIR="$(cd "$RUST_DIR" && cargo metadata --format-version 1 --no-deps 2>/dev/null \
    | perl -ne 'print $1 if /"target_directory":"([^"]+)"/')"
TARGET_DIR="${TARGET_DIR:-$RUST_DIR/target}"

if [ -x "$DIREWOLF_DIR/build/src/atest" ]; then
    ATEST="$DIREWOLF_DIR/build/src/atest"
elif [ -x "$DIREWOLF_DIR/build/atest" ]; then
    ATEST="$DIREWOLF_DIR/build/atest"
elif command -v atest >/dev/null 2>&1; then
    ATEST="$(command -v atest)"
else
    ATEST=""
fi

IPC_BENCH="$TARGET_DIR/release/demod-ipc-bench"

# Prefer GNU timeout on macOS (from coreutils: brew install coreutils).
if command -v gtimeout >/dev/null 2>&1; then
    TIMEOUT="gtimeout --foreground 300"
elif command -v timeout >/dev/null 2>&1; then
    TIMEOUT="timeout --foreground 300"
else
    TIMEOUT=""
fi

# --- Direwolf AD+ (proven best mode, no bit-flipping) ---
echo ""
echo "=== Direwolf -P AD+ ($ITERATIONS iterations) ==="
if [ -n "$ATEST" ]; then
    for _ in $(seq 1 "$ITERATIONS"); do
        LC_ALL=C "$ATEST" -B 1200 -P AD+ "$WAV_FILE" 2>&1 \
            | LC_ALL=C grep -a "packets decoded" \
            | LC_ALL=C sed 's/\x1b\[[0-9;]*m//g' \
            || echo "(no summary line found)"
    done
else
    echo "Direwolf atest not found (looked in $DIREWOLF_DIR/build/{src/,}atest)"
fi

# --- graywolf-modem via IPC (production path, triple-demod default) ---
echo ""
echo "=== graywolf-modem via IPC — ensemble=${ENSEMBLE:-triple (default)} — $ITERATIONS iterations ==="
for _ in $(seq 1 "$ITERATIONS"); do
    if [ -n "$ENSEMBLE" ]; then
        $TIMEOUT "$IPC_BENCH" "$FLAC_FILE" "$ENSEMBLE" 2>/dev/null | tail -1
    else
        $TIMEOUT "$IPC_BENCH" "$FLAC_FILE" 2>/dev/null | tail -1
    fi
done

# --- Cleanup temporary FLAC conversion if we made one ---
if [[ "$FLAC_FILE" == /tmp/bench.*.flac ]]; then
    rm -f "$FLAC_FILE"
fi
