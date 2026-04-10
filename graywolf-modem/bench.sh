#!/bin/bash

# Usage: ./bench.sh <audio-file> [iterations]
AUDIO_FILE="${1:?Usage: bench.sh <audio-file> [iterations]}"
ITERATIONS="${2:-5}"

# Resolve directories to absolute paths
RUST_DIR="$(cd "$(dirname "$0")" && pwd)"
DIREWOLF_DIR="${DIREWOLF_DIR:-$RUST_DIR/../direwolf}"

# --- Locate / convert WAV for C atest ---
if [[ "$AUDIO_FILE" == *.flac ]]; then
    WAV_FILE="${AUDIO_FILE%.flac}.wav"
    if [ ! -f "$WAV_FILE" ]; then
        command -v ffmpeg >/dev/null 2>&1 || { echo "ffmpeg required for FLAC→WAV conversion"; exit 1; }
        echo "Converting $AUDIO_FILE → $WAV_FILE"
        ffmpeg -y -i "$AUDIO_FILE" -acodec pcm_s16le -map_metadata -1 -fflags +bitexact "$WAV_FILE" 2>/dev/null
    fi
else
    WAV_FILE="$AUDIO_FILE"
fi

# --- Build both ---
echo "Building Direwolf (atest)..."
if [ -d "$DIREWOLF_DIR/build" ]; then
    (cd "$DIREWOLF_DIR/build" && make atest 2>/dev/null) || echo "Warning: C build failed"
fi

echo "Building Graywolf (demod_bench)..."
(cd "$RUST_DIR" && RUSTFLAGS="-C target-cpu=native" cargo build --release --bin demod-bench 2>/dev/null)

# Resolve cargo target directory (handles workspace vs package layouts)
TARGET_DIR="$(cd "$RUST_DIR" && cargo metadata --format-version 1 --no-deps 2>/dev/null \
    | perl -ne 'print $1 if /"target_directory":"([^"]+)"/')"
TARGET_DIR="${TARGET_DIR:-$RUST_DIR/target}"

# cmake puts atest under build/src/ — check both locations, then fall back to PATH
if [ -x "$DIREWOLF_DIR/build/src/atest" ]; then
    ATEST="$DIREWOLF_DIR/build/src/atest"
elif [ -x "$DIREWOLF_DIR/build/atest" ]; then
    ATEST="$DIREWOLF_DIR/build/atest"
elif command -v atest >/dev/null 2>&1; then
    ATEST="$(command -v atest)"
else
    ATEST=""
fi

DEMOD_BENCH="$TARGET_DIR/release/demod-bench"

# --- Benchmark C ---
echo ""
echo "=== Direwolf (atest) — $ITERATIONS iterations ==="
if [ -n "$ATEST" ]; then
    for _ in $(seq 1 "$ITERATIONS"); do
        "$ATEST" -B 1200 "$WAV_FILE" 2>&1 \
            | perl -pe 's/\e\[[0-9;]*m//g' \
            | grep "packets decoded" \
            || echo "(no summary line found)"
    done
else
    echo "Direwolf atest not found (looked in $DIREWOLF_DIR/build/{src/,}atest)"
fi

# --- Benchmark Rust ---
echo ""
echo "=== Graywolf (demod_bench) — $ITERATIONS iterations ==="
for _ in $(seq 1 "$ITERATIONS"); do
    "$DEMOD_BENCH" "$AUDIO_FILE" 2>&1 | tail -1
done
