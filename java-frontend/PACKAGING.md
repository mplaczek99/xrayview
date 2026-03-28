# Packaging

The first packaging target is a `jpackage` app-image.

- It produces a self-contained runnable app without adding installer, signing, or platform-specific packaging work yet.
- This step packages only the Java frontend. The Go backend is not bundled yet, so the app-image is intended to run from the repository checkout for now.

Requirements:

- JDK with `jpackage`
- Maven

Build the app-image:

```bash
bash java-frontend/package-app-image.sh
```

Run the packaged app:

```bash
./java-frontend/target/app-image/XRayView/bin/XRayView
```
