#!/usr/bin/env bash
# Sync android/ + jniLibs to block.local, build APK, scp back, adb install.
set -euo pipefail
cd "$(dirname "$0")/../.."
HOST=block.local
REMOTE=build/android-poc-b
APK_REMOTE="$REMOTE/android/app/build/outputs/apk/debug/app-debug.apk"
APK_LOCAL=scratch/poc-b/app-debug.apk

echo "[1/4] rsync android/ -> $HOST"
rsync -az --delete android/ "$HOST:$REMOTE/android/"

echo "[2/4] assembleDebug on $HOST"
ssh "$HOST" "export ANDROID_HOME=\$HOME/android-sdk JAVA_HOME=/usr/lib/jvm/java-17-openjdk PATH=\$JAVA_HOME/bin:/usr/share/java/gradle/bin:\$PATH; cd \$HOME/$REMOTE/android && gradle --no-daemon assembleDebug" 2>&1 | tail -25

echo "[3/4] scp APK"
scp "$HOST:$APK_REMOTE" "$APK_LOCAL"
ls -lh "$APK_LOCAL"

echo "[4/4] adb install"
adb install -r "$APK_LOCAL"
