#!/usr/bin/env bash
# Verifies the Android cross-compile toolchain. Run by Gradle as a preBuild
# task; also runnable standalone. Exits 0 on success, non-zero with a
# pointer at the missing piece otherwise.
set -euo pipefail

fail() {
    echo "[check-android-toolchain] $1" >&2
    echo "[check-android-toolchain] See docs/wiki/android-build.md for setup." >&2
    exit 1
}

# 1. JDK 17.
if ! command -v java >/dev/null 2>&1; then
    fail "java not on PATH; install JDK 17"
fi
JAVA_VERSION=$(java -version 2>&1 | head -n1 | awk -F\" '{print $2}' | awk -F. '{print $1}')
if [[ "$JAVA_VERSION" != "17" ]]; then
    fail "JDK $JAVA_VERSION found; need JDK 17"
fi

# 2. Android NDK r27c+. ANDROID_NDK_ROOT or ANDROID_NDK_HOME must point at it.
NDK_DIR="${ANDROID_NDK_ROOT:-${ANDROID_NDK_HOME:-}}"
if [[ -z "$NDK_DIR" ]]; then
    fail "ANDROID_NDK_ROOT (or ANDROID_NDK_HOME) not set"
fi
if [[ ! -f "$NDK_DIR/source.properties" ]]; then
    fail "NDK source.properties missing at $NDK_DIR/source.properties"
fi
NDK_REV=$(awk -F= '/^Pkg.Revision/ {gsub(/ /,"",$2); print $2}' "$NDK_DIR/source.properties")
NDK_MAJOR=$(echo "$NDK_REV" | awk -F. '{print $1}')
if (( NDK_MAJOR < 27 )); then
    fail "NDK rev $NDK_REV; need r27c (27.x) or newer"
fi

# 3. rustup-managed rustc with the Android targets.
if ! command -v rustup >/dev/null 2>&1; then
    fail "rustup not on PATH; install via https://rustup.rs"
fi
for TARGET in aarch64-linux-android x86_64-linux-android; do
    if ! rustup target list --installed | grep -qx "$TARGET"; then
        fail "Rust target $TARGET not installed; run: rustup target add $TARGET"
    fi
done

# 4. cargo-ndk 4.x.
if ! command -v cargo-ndk >/dev/null 2>&1; then
    fail "cargo-ndk not on PATH; run: cargo install cargo-ndk --locked"
fi
NDK_TOOL_VER=$(cargo ndk --version 2>/dev/null | awk '{print $2}')
NDK_TOOL_MAJOR=$(echo "$NDK_TOOL_VER" | awk -F. '{print $1}')
if (( NDK_TOOL_MAJOR < 4 )); then
    fail "cargo-ndk $NDK_TOOL_VER; need 4.x or newer"
fi

# 5. Go 1.23+.
if ! command -v go >/dev/null 2>&1; then
    fail "go not on PATH; install Go 1.23+"
fi
GO_VERSION=$(go version | awk '{print $3}' | sed 's/^go//')
GO_MINOR=$(echo "$GO_VERSION" | awk -F. '{print $2}')
GO_MAJOR=$(echo "$GO_VERSION" | awk -F. '{print $1}')
if (( GO_MAJOR < 1 )) || (( GO_MAJOR == 1 && GO_MINOR < 23 )); then
    fail "Go $GO_VERSION; need 1.23+"
fi

echo "[check-android-toolchain] OK: JDK 17, NDK $NDK_REV, cargo-ndk $NDK_TOOL_VER, Go $GO_VERSION"
