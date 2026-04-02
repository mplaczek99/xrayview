import { spawnSync } from "node:child_process";
import { frontendRoot, prepareTauriTarget } from "./prepare-tauri-target.mjs";

const args = process.argv.slice(2);

// Normalize the target directory before dev boot so stale renamed artifacts do
// not confuse Tauri when it locates the frontend binary.
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
