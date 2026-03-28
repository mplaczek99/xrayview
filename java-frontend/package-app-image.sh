#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$SCRIPT_DIR"
TARGET_DIR="$PROJECT_DIR/target"
APP_NAME="XRayView"
APP_IMAGE_DIR="$TARGET_DIR/app-image"
APP_INPUT_DIR="$APP_IMAGE_DIR/input"

rm -rf "$APP_IMAGE_DIR"
mkdir -p "$APP_INPUT_DIR"

mvn -f "$PROJECT_DIR/pom.xml" package dependency:copy-dependencies \
    -DincludeScope=runtime \
    -DoutputDirectory="$APP_INPUT_DIR"

PROJECT_JAR="$(basename "$TARGET_DIR"/xrayview-java-frontend-*.jar)"
cp "$TARGET_DIR/$PROJECT_JAR" "$APP_INPUT_DIR/"

jpackage \
    --type app-image \
    --dest "$APP_IMAGE_DIR" \
    --input "$APP_INPUT_DIR" \
    --name "$APP_NAME" \
    --main-jar "$PROJECT_JAR" \
    --main-class "com.xrayview.frontend.XRayViewApp" \
    --java-options "--enable-native-access=javafx.graphics" \
    --java-options "--sun-misc-unsafe-memory-access=allow"
