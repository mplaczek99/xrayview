import { spawnSync } from "node:child_process";
import fs from "node:fs";
import { bundleDir, frontendRoot, prepareTauriTarget } from "./prepare-tauri-target.mjs";

const args = process.argv.slice(2);
const env = { ...process.env };

prepareTauriTarget();

if (fs.existsSync(bundleDir)) {
  fs.rmSync(bundleDir, { force: true, recursive: true });
}

if (process.platform === "linux") {
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
