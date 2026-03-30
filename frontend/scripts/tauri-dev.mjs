import { spawnSync } from "node:child_process";
import { frontendRoot, prepareTauriTarget } from "./prepare-tauri-target.mjs";

const args = process.argv.slice(2);

prepareTauriTarget();

const tauriCommand = process.platform === "win32" ? "tauri.cmd" : "tauri";
const result = spawnSync(tauriCommand, ["dev", ...args], {
  cwd: frontendRoot,
  env: { ...process.env },
  stdio: "inherit",
});

if (result.error) {
  throw result.error;
}

process.exit(result.status ?? 0);
