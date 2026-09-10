#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
SDK_ROOT="${ANDROID_SDK_ROOT:-${ANDROID_HOME:-}}"
NDK_ROOT="${ANDROID_NDK_HOME:-$SDK_ROOT/ndk/27.0.12077973}"
GOMOBILE_BIN="${GOMOBILE_BIN:-$(command -v gomobile || true)}"
GRADLE_BIN="${GRADLE_BIN:-}"

if [ -z "$GRADLE_BIN" ]; then
    GRADLE_BIN="$(command -v gradle || true)"
fi
if [ -z "$GRADLE_BIN" ]; then
    GRADLE_BIN="$(find "$HOME/.gradle/wrapper/dists" -path '*/gradle-8.14.3/bin/gradle' -type f 2>/dev/null | head -n 1 || true)"
fi

if [ -z "$SDK_ROOT" ] || [ ! -d "$SDK_ROOT" ]; then
    echo "Android SDK not found. Set ANDROID_SDK_ROOT or ANDROID_HOME."
    exit 1
fi
if [ ! -x "$GOMOBILE_BIN" ]; then
    echo "gomobile not found in PATH."
    echo "Install it with: go install golang.org/x/mobile/cmd/gomobile@latest"
    exit 1
fi
if [ ! -x "$GRADLE_BIN" ]; then
    echo "Gradle 8.14.3 not found. Set GRADLE_BIN to its executable."
    exit 1
fi
if [ ! -d "$NDK_ROOT" ]; then
    echo "Android NDK not found at $NDK_ROOT"
    exit 1
fi

mkdir -p "$SCRIPT_DIR/android/app/libs" "$SCRIPT_DIR/dist"

export ANDROID_HOME="$SDK_ROOT"
export ANDROID_SDK_ROOT="$SDK_ROOT"
export ANDROID_NDK_HOME="$NDK_ROOT"
export PATH="$(dirname -- "$GOMOBILE_BIN"):$PATH"

(
    cd "$SCRIPT_DIR/mobile"
    "$GOMOBILE_BIN" bind \
        -target=android/arm64 \
        -androidapi=26 \
        -javapkg=io.openflux.bridge \
        -o ../android/app/libs/openflux.aar \
        .
)

(
    cd "$SCRIPT_DIR/android"
    "$GRADLE_BIN" --no-daemon assembleDebug
)

cp "$SCRIPT_DIR/android/app/build/outputs/apk/debug/app-debug.apk" \
   "$SCRIPT_DIR/dist/OpenFlux-android-arm64-debug.apk"

echo "Built: $SCRIPT_DIR/dist/OpenFlux-android-arm64-debug.apk"
