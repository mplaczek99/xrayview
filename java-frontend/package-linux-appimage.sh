#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$SCRIPT_DIR"
TARGET_DIR="$PROJECT_DIR/target"
APP_NAME="XRayView"
APP_DESCRIPTION="Desktop image visualization tool with a bundled Go backend"
APP_BUNDLE_DIR="$TARGET_DIR/app-image/$APP_NAME"
APPDIR_DIR="$TARGET_DIR/appimage/$APP_NAME.AppDir"
BACKEND_BINARY="${1:-}"
OUTPUT_APPIMAGE="${2:-}"
APP_VERSION="${3:-}"
APPIMAGETOOL_BIN="${4:-appimagetool}"

if [ -z "$BACKEND_BINARY" ]; then
    printf 'Pass the built Go backend binary path as the first argument.\n' >&2
    exit 1
fi

if [ -z "$OUTPUT_APPIMAGE" ]; then
    printf 'Pass the output AppImage path as the second argument.\n' >&2
    exit 1
fi

if [ -x "$APPIMAGETOOL_BIN" ]; then
    APPIMAGETOOL_PATH="$APPIMAGETOOL_BIN"
elif command -v "$APPIMAGETOOL_BIN" >/dev/null 2>&1; then
    APPIMAGETOOL_PATH="$(command -v "$APPIMAGETOOL_BIN")"
else
    printf 'appimagetool is required but %s was not found.\n' "$APPIMAGETOOL_BIN" >&2
    exit 1
fi

OUTPUT_DIR="$(dirname "$OUTPUT_APPIMAGE")"
mkdir -p "$OUTPUT_DIR"

bash "$PROJECT_DIR/package-app-image.sh" "$BACKEND_BINARY"

if [ ! -d "$APP_BUNDLE_DIR" ]; then
    printf 'Expected jpackage app-image at %s\n' "$APP_BUNDLE_DIR" >&2
    exit 1
fi

if [ ! -f "$APP_BUNDLE_DIR/lib/$APP_NAME.png" ]; then
    printf 'Expected jpackage icon at %s/lib/%s.png\n' "$APP_BUNDLE_DIR" "$APP_NAME" >&2
    exit 1
fi

rm -rf "$APPDIR_DIR"
mkdir -p "$APPDIR_DIR/usr"
cp -a "$APP_BUNDLE_DIR/." "$APPDIR_DIR/usr/"
cp "$APP_BUNDLE_DIR/lib/$APP_NAME.png" "$APPDIR_DIR/$APP_NAME.png"
ln -sfn "$APP_NAME.png" "$APPDIR_DIR/.DirIcon"

cat > "$APPDIR_DIR/AppRun" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

HERE="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec "$HERE/usr/bin/XRayView" "$@"
EOF
chmod +x "$APPDIR_DIR/AppRun"

cat > "$APPDIR_DIR/$APP_NAME.desktop" <<EOF
[Desktop Entry]
Type=Application
Name=$APP_NAME
Comment=$APP_DESCRIPTION
Exec=AppRun
Icon=$APP_NAME
Categories=Graphics;
Terminal=false
EOF

APPIMAGETOOL_CMD=("$APPIMAGETOOL_PATH")
if [[ "$APPIMAGETOOL_PATH" == *.AppImage ]]; then
    APPIMAGETOOL_CMD+=("--appimage-extract-and-run")
fi

APPIMAGETOOL_ENV=(ARCH=x86_64)
if [ -n "$APP_VERSION" ]; then
    APPIMAGETOOL_ENV+=(VERSION="$APP_VERSION")
fi

rm -f "$OUTPUT_APPIMAGE"
env "${APPIMAGETOOL_ENV[@]}" "${APPIMAGETOOL_CMD[@]}" -n "$APPDIR_DIR" "$OUTPUT_APPIMAGE"

printf 'Created AppImage at %s\n' "$OUTPUT_APPIMAGE"
