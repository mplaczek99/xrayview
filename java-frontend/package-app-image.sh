#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$SCRIPT_DIR"
TARGET_DIR="$PROJECT_DIR/target"
APP_NAME="XRayView"
APP_IMAGE_DIR="$TARGET_DIR/app-image"
APP_INPUT_DIR="$TARGET_DIR/jpackage-input"

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

rm -rf "$APP_IMAGE_DIR" "$APP_INPUT_DIR"
mkdir -p "$APP_INPUT_DIR/lib"

PROJECT_JAR="$(basename "$TARGET_DIR"/xrayview-java-frontend-*.jar)"
cp "$TARGET_DIR/$PROJECT_JAR" "$APP_INPUT_DIR/"
cp "$TARGET_DIR"/lib/*.jar "$APP_INPUT_DIR/lib/"

jpackage \
    --type app-image \
    --dest "$APP_IMAGE_DIR" \
    --input "$APP_INPUT_DIR" \
    --name "$APP_NAME" \
    --main-jar "$PROJECT_JAR" \
    --main-class "com.xrayview.frontend.XRayViewLauncher" \
    --java-options "--enable-native-access=javafx.graphics" \
    --java-options "--sun-misc-unsafe-memory-access=allow"

printf 'Created app image at %s/%s\n' "$APP_IMAGE_DIR" "$APP_NAME"
printf 'Launch it with XRAYVIEW_BACKEND_PATH set to your built Go CLI binary.\n'
