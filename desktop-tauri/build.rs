// Tauri's build step — generates the invoke handlers + capability glue from
// tauri.conf.json and the macros in src/. If you ever see "command not found"
// at runtime, suspect this step didn't pick up a new #[tauri::command].
fn main() {
    tauri_build::build()
}
