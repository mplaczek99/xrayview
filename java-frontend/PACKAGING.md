# Packaging

The base packaging target is a local `jpackage` app-image on the current OS.

- It produces a self-contained runnable app image locally; release workflows turn that app image into the final Linux and Windows release assets.
- This step stages a separately built Go backend binary into the app-image at `lib/app/backend/xrayview-backend[.exe]` so the packaged app exposes the frontend launcher instead of the raw CLI name.
- Linux releases wrap that app-image in a single `.AppImage` executable with `appimagetool`.
- Windows releases publish a portable `.zip` archive containing the packaged `XRayView/` app folder so users can unzip and run `XRayView.exe` without an installer.

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
  0.1.2 \
  ./appimagetool-x86_64.AppImage
```

## Windows portable release

Tagged releases build the Windows app-image and then zip the `XRayView/` folder for distribution.

- Extract the zip on Windows.
- Run `XRayView.exe` from the extracted `XRayView/` folder.
