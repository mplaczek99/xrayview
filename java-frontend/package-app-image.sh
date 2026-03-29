#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$SCRIPT_DIR"
TARGET_DIR="$PROJECT_DIR/target"
APP_NAME="XRayView"
APP_IMAGE_DIR="$TARGET_DIR/app-image"
APP_INPUT_DIR="$TARGET_DIR/jpackage-input"
BACKEND_BINARY="${1:-}"

if ! command -v jpackage >/dev/null 2>&1; then
    printf 'jpackage is required but was not found on PATH.\n' >&2
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

rm -rf "$APP_IMAGE_DIR" "$APP_INPUT_DIR"
mkdir -p "$APP_INPUT_DIR/lib" "$APP_INPUT_DIR/backend"

PROJECT_JAR="$(basename "$TARGET_DIR"/xrayview-java-frontend-*.jar)"
cp "$TARGET_DIR/$PROJECT_JAR" "$APP_INPUT_DIR/"
cp "$TARGET_DIR"/lib/*.jar "$APP_INPUT_DIR/lib/"
cp "$BACKEND_BINARY" "$APP_INPUT_DIR/backend/xrayview"
chmod +x "$APP_INPUT_DIR/backend/xrayview"

jpackage \
    --type app-image \
    --dest "$APP_IMAGE_DIR" \
    --input "$APP_INPUT_DIR" \
    --name "$APP_NAME" \
    --main-jar "$PROJECT_JAR" \
    --main-class "com.xrayview.frontend.XRayViewLauncher" \
    --java-options "--enable-native-access=ALL-UNNAMED"

printf 'Created app image at %s/%s\n' "$APP_IMAGE_DIR" "$APP_NAME"
printf 'Bundled backend copied to lib/app/backend/xrayview inside the app image.\n'
