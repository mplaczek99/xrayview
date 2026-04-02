fn main() {
    // Delegate to Tauri's build helper so icons, capabilities, and generated
    // metadata stay aligned with the app manifest.
    tauri_build::build()
}
