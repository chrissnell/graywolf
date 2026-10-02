export JAVA_HOME=/opt/homebrew/opt/openjdk@17
export PATH="$JAVA_HOME/bin:$PATH"

export ANDROID_HOME=/Users/zwolanj/Library/Android/sdk
export ANDROID_NDK_HOME="$ANDROID_HOME/ndk/30.0.15729638"
export ANDROID_NDK_ROOT="$ANDROID_NDK_HOME"

./gradlew :app:assembleDebug