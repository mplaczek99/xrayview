# Packaging

The first packaging target is a local `jpackage` app-image on the current OS.

- It produces a self-contained runnable app without adding installer, signing, or platform-specific packaging work yet.
- This step packages only the Java frontend. The Go backend is still built separately and passed in at runtime with `XRAYVIEW_BACKEND_PATH`.

Requirements:

- JDK with `jpackage`
- Maven
- Go

Build the backend:

```bash
go build -o /tmp/xrayview ./cmd/xrayview
```

Build the Java frontend:

```bash
mvn -f java-frontend/pom.xml package
```

Build the app-image:

```bash
bash java-frontend/package-app-image.sh
```

Run the packaged app:

```bash
XRAYVIEW_BACKEND_PATH=/tmp/xrayview ./java-frontend/target/app-image/XRayView/bin/XRayView
```
