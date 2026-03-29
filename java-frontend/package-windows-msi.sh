#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$SCRIPT_DIR"
TARGET_DIR="$PROJECT_DIR/target"
APP_NAME="XRayView"
APP_VENDOR="Michael Placzek"
APP_DESCRIPTION="Desktop image visualization tool with a bundled Go backend"
INSTALLER_DIR="$TARGET_DIR/windows-installer"
APP_INPUT_DIR="$TARGET_DIR/jpackage-input"
ICON_FILE="$TARGET_DIR/xrayview-placeholder.ico"
BACKEND_BINARY="${1:-}"
APP_VERSION="${2:-}"
BACKEND_NAME=""

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

BACKEND_NAME="$(basename "$BACKEND_BINARY")"

if [ -z "$APP_VERSION" ]; then
    printf 'Pass the numeric app version as the second argument, for example 0.1.0.\n' >&2
    exit 1
fi

rm -rf "$INSTALLER_DIR" "$APP_INPUT_DIR"
mkdir -p "$INSTALLER_DIR" "$APP_INPUT_DIR/lib" "$APP_INPUT_DIR/backend"

PROJECT_JAR="$(basename "$TARGET_DIR"/xrayview-java-frontend-*.jar)"
cp "$TARGET_DIR/$PROJECT_JAR" "$APP_INPUT_DIR/"
cp "$TARGET_DIR"/lib/*.jar "$APP_INPUT_DIR/lib/"
cp "$BACKEND_BINARY" "$APP_INPUT_DIR/backend/$BACKEND_NAME"

cat <<'EOF' | base64 --decode > "$ICON_FILE"
AAABAAEAEBAAAAEAIABoBAAAFgAAACgAAAAQAAAAIAAAAAEAIAAAAAAAAAQAAAAAAAAAAAAAAAAAAAAAAAD6+PX/PCgV/zwoFf88KBX/PCgV/zwoFf88KBX/PCgV/zwoFf88KBX/PCgV/zwoFf88KBX/PCgV/zwoFf/6+PX/PCgV//r49f+HXi7/h14u/4deLv+HXi7/h14u/4deLv+HXi7/h14u/4deLv+HXi7/h14u/4deLv/6+PX/PCgV/zwoFf+HXi7/+vj1/4deLv+HXi7/h14u/4deLv+HXi7/h14u/4deLv+HXi7/h14u/4deLv/6+PX/h14u/zwoFf88KBX/h14u/4deLv/6+PX/h14u/4deLv+HXi7/h14u/4deLv+HXi7/h14u/4deLv/6+PX/h14u/4deLv88KBX/PCgV/4deLv+HXi7/h14u//r49f+HXi7/h14u/4deLv+HXi7/h14u/4deLv/6+PX/h14u/4deLv+HXi7/PCgV/zwoFf+HXi7/h14u/4deLv+HXi7/+vj1/4deLv+HXi7/h14u/4deLv/6+PX/h14u/4deLv+HXi7/h14u/zwoFf88KBX/h14u/4deLv+HXi7/h14u/4deLv/6+PX/h14u/4deLv/6+PX/h14u/4deLv+HXi7/h14u/4deLv88KBX/PCgV/4deLv+HXi7/h14u/4deLv+HXi7/h14u//r49f/6+PX/h14u/4deLv+HXi7/h14u/4deLv+HXi7/PCgV/zwoFf+HXi7/h14u/4deLv+HXi7/h14u/4deLv/6+PX/+vj1/4deLv+HXi7/h14u/4deLv+HXi7/h14u/zwoFf88KBX/h14u/4deLv+HXi7/h14u/4deLv/6+PX/h14u/4deLv/6+PX/h14u/4deLv+HXi7/h14u/4deLv88KBX/PCgV/4deLv+HXi7/h14u/4deLv/6+PX/h14u/4deLv+HXi7/h14u//r49f+HXi7/h14u/4deLv+HXi7/PCgV/zwoFf+HXi7/h14u/4deLv/6+PX/h14u/4deLv+HXi7/h14u/4deLv+HXi7/+vj1/4deLv+HXi7/h14u/zwoFf88KBX/h14u/4deLv/6+PX/h14u/4deLv+HXi7/h14u/4deLv+HXi7/h14u/4deLv/6+PX/h14u/4deLv88KBX/PCgV/4deLv/6+PX/h14u/4deLv+HXi7/h14u/4deLv+HXi7/h14u/4deLv+HXi7/h14u//r49f+HXi7/PCgV/zwoFf/6+PX/h14u/4deLv+HXi7/h14u/4deLv+HXi7/h14u/4deLv+HXi7/h14u/4deLv+HXi7/+vj1/zwoFf/6+PX/PCgV/zwoFf88KBX/PCgV/zwoFf88KBX/PCgV/zwoFf88KBX/PCgV/zwoFf88KBX/PCgV/zwoFf/6+PX/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA==
EOF

jpackage \
    --type msi \
    --dest "$INSTALLER_DIR" \
    --input "$APP_INPUT_DIR" \
    --name "$APP_NAME" \
    --vendor "$APP_VENDOR" \
    --description "$APP_DESCRIPTION" \
    --app-version "$APP_VERSION" \
    --icon "$ICON_FILE" \
    --main-jar "$PROJECT_JAR" \
    --main-class "com.xrayview.frontend.XRayViewLauncher" \
    --java-options "--enable-native-access=ALL-UNNAMED"

printf 'Created MSI in %s\n' "$INSTALLER_DIR"
