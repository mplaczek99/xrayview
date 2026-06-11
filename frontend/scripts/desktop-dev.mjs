// Hot-reload desktop dev: `wails dev` starts the Vite dev server (wails.json
// frontend:dev:watcher) and the native window, rebuilding on change.
import { runWails } from "./wails-cli.mjs";

runWails("dev", process.argv.slice(2));
