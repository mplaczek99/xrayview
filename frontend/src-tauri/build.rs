use std::env;

const BACKEND_RUNTIME_ENV_KEY: &str = "XRAYVIEW_BACKEND_RUNTIME";
const VITE_BACKEND_RUNTIME_ENV_KEY: &str = "VITE_XRAYVIEW_BACKEND_RUNTIME";
const GO_SIDECAR_URL_ENV_KEY: &str = "XRAYVIEW_GO_BACKEND_URL";
const VITE_GO_SIDECAR_URL_ENV_KEY: &str = "VITE_XRAYVIEW_GO_BACKEND_URL";
const BUILT_BACKEND_RUNTIME_ENV_KEY: &str = "XRAYVIEW_FRONTEND_BACKEND_RUNTIME";
const BUILT_GO_SIDECAR_URL_ENV_KEY: &str = "XRAYVIEW_FRONTEND_GO_BACKEND_URL";

fn pick_env_value(plain_key: &str, vite_key: &str) -> String {
    env::var(plain_key)
        .or_else(|_| env::var(vite_key))
        .unwrap_or_default()
        .trim()
        .to_string()
}

fn main() {
    println!("cargo:rerun-if-env-changed={BACKEND_RUNTIME_ENV_KEY}");
    println!("cargo:rerun-if-env-changed={VITE_BACKEND_RUNTIME_ENV_KEY}");
    println!("cargo:rerun-if-env-changed={GO_SIDECAR_URL_ENV_KEY}");
    println!("cargo:rerun-if-env-changed={VITE_GO_SIDECAR_URL_ENV_KEY}");

    println!(
        "cargo:rustc-env={BUILT_BACKEND_RUNTIME_ENV_KEY}={}",
        pick_env_value(BACKEND_RUNTIME_ENV_KEY, VITE_BACKEND_RUNTIME_ENV_KEY)
    );
    println!(
        "cargo:rustc-env={BUILT_GO_SIDECAR_URL_ENV_KEY}={}",
        pick_env_value(GO_SIDECAR_URL_ENV_KEY, VITE_GO_SIDECAR_URL_ENV_KEY)
    );

    // Delegate to Tauri's build helper so icons, capabilities, and generated
    // metadata stay aligned with the app manifest.
    tauri_build::build()
}
