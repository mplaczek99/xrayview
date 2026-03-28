# Packaging

The first packaging target is a local `jpackage` app-image on the current OS.

- It produces a self-contained runnable app without adding installer, signing, or platform-specific packaging work yet.
- This step stages a separately built Go backend binary into the app-image at `lib/app/backend/xrayview`.

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
