# Packaging

The base packaging target is a local `jpackage` app-image on the current OS.

- It produces a self-contained runnable app without adding installer, signing, or platform-specific packaging work yet.
- This step stages a separately built Go backend binary into the app-image at `lib/app/backend/` using its platform-specific filename.
- Linux releases wrap that app-image in a single `.AppImage` executable with `appimagetool`.

Requirements:

- JDK with `jpackage`
- Maven
- Go

Build the backend:

```bash
go build -o java-frontend/target/xrayview ./cmd/xrayview
```

Build the Java frontend:

```bash
mvn -f java-frontend/pom.xml package
```

Build the app-image:

```bash
bash java-frontend/package-app-image.sh java-frontend/target/xrayview
```

Run the packaged app:

```bash
./java-frontend/target/app-image/XRayView/bin/XRayView
```

Build the Linux `.AppImage`:

```bash
curl -fsSL -o appimagetool-x86_64.AppImage \
  https://github.com/AppImage/appimagetool/releases/download/continuous/appimagetool-x86_64.AppImage
chmod +x appimagetool-x86_64.AppImage
bash java-frontend/package-linux-appimage.sh \
  java-frontend/target/xrayview \
  java-frontend/target/release/XRayView.AppImage \
  0.1.0 \
  ./appimagetool-x86_64.AppImage
```
