import { spawnSync } from "node:child_process";
import { frontendRoot, prepareTauriTarget } from "./prepare-tauri-target.mjs";

const args = process.argv.slice(2);

prepareTauriTarget();

const result = spawnSync("tauri", ["dev", ...args], {
  cwd: frontendRoot,
  env: { ...process.env },
  stdio: "inherit",
  shell: process.platform === "win32",
});

if (result.error) {
  throw result.error;
}

process.exit(result.status ?? 0);
