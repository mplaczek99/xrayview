// Builds the desktop app: `wails build` runs the frontend build (wails.json
// frontend:build → syncs into desktop/dist) then compiles the Go binary to
// desktop/build/bin/xrayview. Pass-through args go to `wails build`.
import { runWails } from "./wails-cli.mjs";

runWails("build", process.argv.slice(2));
