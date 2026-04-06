import { spawnSync } from "node:child_process";
import fs from "node:fs";
import { bundleDir, frontendRoot, prepareTauriTarget } from "./prepare-tauri-target.mjs";
import { applyFrontendRuntimeEnv } from "./runtime-env.mjs";

const args = process.argv.slice(2);
const env = applyFrontendRuntimeEnv(process.env);

prepareTauriTarget();

// Clear previous bundles so packaging only picks up artifacts from this build.
if (fs.existsSync(bundleDir)) {
  fs.rmSync(bundleDir, { force: true, recursive: true });
}

if (process.platform === "linux") {
  // These flags make local and CI AppImage packaging more predictable on Linux.
  env.APPIMAGE_EXTRACT_AND_RUN ??= "1";
  env.NO_STRIP ??= "1";
}

const result = spawnSync("tauri", ["build", ...args], {
  cwd: frontendRoot,
  env,
  stdio: "inherit",
  shell: process.platform === "win32",
});

if (result.error) {
  throw result.error;
}

process.exit(result.status ?? 0);
