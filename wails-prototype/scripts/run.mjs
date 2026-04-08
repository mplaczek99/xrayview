import path from "node:path";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(scriptDir, "..", "..");
const buildScript = path.join(repoRoot, "wails-prototype", "scripts", "build.mjs");
const binaryPath = path.join(
  repoRoot,
  "wails-prototype",
  "build",
  "bin",
  process.platform === "win32" ? "xrayview-wails-prototype.exe" : "xrayview-wails-prototype",
);

function run(command, args) {
  const result = spawnSync(command, args, {
    cwd: repoRoot,
    stdio: "inherit",
    shell: process.platform === "win32",
  });

  if (result.error) {
    throw result.error;
  }

  if ((result.status ?? 0) !== 0) {
    process.exit(result.status ?? 1);
  }
}

run("node", [buildScript]);
run(binaryPath, []);
