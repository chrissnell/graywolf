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
echo "Building C (atest)..."
if [ -d "$DIREWOLF_DIR/build" ]; then
    (cd "$DIREWOLF_DIR/build" && make atest 2>/dev/null) || echo "Warning: C build failed"
fi

echo "Building Rust (demod_bench)..."
(cd "$RUST_DIR" && RUSTFLAGS="-C target-cpu=native" cargo build --release 2>/dev/null)

# cmake puts atest under build/src/ — check both locations
if [ -x "$DIREWOLF_DIR/build/src/atest" ]; then
    ATEST="$DIREWOLF_DIR/build/src/atest"
elif [ -x "$DIREWOLF_DIR/build/atest" ]; then
    ATEST="$DIREWOLF_DIR/build/atest"
else
    ATEST=""
fi

DEMOD_BENCH="$RUST_DIR/target/release/demod_bench"

# --- Benchmark C ---
echo ""
echo "=== C (atest) — $ITERATIONS iterations ==="
if [ -n "$ATEST" ]; then
    for i in $(seq 1 "$ITERATIONS"); do
        "$ATEST" -B 1200 "$WAV_FILE" 2>&1 \
            | perl -pe 's/\e\[[0-9;]*m//g' \
            | grep "packets decoded" \
            || echo "(no summary line found)"
    done
else
    echo "C atest not found (looked in $DIREWOLF_DIR/build/{src/,}atest)"
fi

# --- Benchmark Rust ---
echo ""
echo "=== Rust (demod_bench) — $ITERATIONS iterations ==="
for i in $(seq 1 "$ITERATIONS"); do
    "$DEMOD_BENCH" "$AUDIO_FILE" 2>&1 | tail -1
done
