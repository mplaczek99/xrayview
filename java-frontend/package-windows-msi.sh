#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$SCRIPT_DIR"
TARGET_DIR="$PROJECT_DIR/target"
APP_NAME="XRayView"
INSTALLER_DIR="$TARGET_DIR/windows-installer"
APP_INPUT_DIR="$TARGET_DIR/jpackage-input"
BACKEND_BINARY="${1:-}"
APP_VERSION="${2:-}"

if ! command -v jpackage >/dev/null 2>&1; then
    printf 'jpackage is required but was not found on PATH.\n' >&2
    exit 1
fi

if ! command -v candle.exe >/dev/null 2>&1; then
    printf 'WiX Toolset is required for MSI packaging but candle.exe was not found on PATH.\n' >&2
    exit 1
fi

if ! compgen -G "$TARGET_DIR/xrayview-java-frontend-*.jar" >/dev/null; then
    printf 'Build the Java frontend first with: mvn -f java-frontend/pom.xml package\n' >&2
    exit 1
fi

if [ ! -d "$TARGET_DIR/lib" ]; then
    printf 'Expected runtime dependencies in %s/lib after Maven packaging.\n' "$TARGET_DIR" >&2
    exit 1
fi

if [ -z "$BACKEND_BINARY" ]; then
    printf 'Pass the built Go backend binary path as the first argument.\n' >&2
    exit 1
fi

if [ ! -f "$BACKEND_BINARY" ] || [ ! -x "$BACKEND_BINARY" ]; then
    printf 'Expected an executable Go backend binary at %s\n' "$BACKEND_BINARY" >&2
    exit 1
fi

if [ -z "$APP_VERSION" ]; then
    printf 'Pass the numeric app version as the second argument, for example 0.1.0.\n' >&2
    exit 1
fi

rm -rf "$INSTALLER_DIR" "$APP_INPUT_DIR"
mkdir -p "$INSTALLER_DIR" "$APP_INPUT_DIR/lib" "$APP_INPUT_DIR/backend"

PROJECT_JAR="$(basename "$TARGET_DIR"/xrayview-java-frontend-*.jar)"
cp "$TARGET_DIR/$PROJECT_JAR" "$APP_INPUT_DIR/"
cp "$TARGET_DIR"/lib/*.jar "$APP_INPUT_DIR/lib/"
cp "$BACKEND_BINARY" "$APP_INPUT_DIR/backend/xrayview"

jpackage \
    --type msi \
    --dest "$INSTALLER_DIR" \
    --input "$APP_INPUT_DIR" \
    --name "$APP_NAME" \
    --app-version "$APP_VERSION" \
    --main-jar "$PROJECT_JAR" \
    --main-class "com.xrayview.frontend.XRayViewLauncher" \
    --java-options "--enable-native-access=javafx.graphics" \
    --java-options "--sun-misc-unsafe-memory-access=allow"

printf 'Created MSI in %s\n' "$INSTALLER_DIR"
