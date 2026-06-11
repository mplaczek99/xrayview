import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const frontendRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const workspaceRoot = path.resolve(frontendRoot, "..");
const releaseBinary = path.join(
  workspaceRoot,
  "desktop",
  "build",
  "bin",
  process.platform === "win32" ? "xrayview.exe" : "xrayview",
);

function run(command, args, cwd, envOverrides = {}) {
  const result = spawnSync(command, args, {
    cwd,
    env: { ...process.env, ...envOverrides },
    stdio: "inherit",
    shell: process.platform === "win32",
  });

  if (result.error) {
    throw result.error;
  }

  if ((result.status ?? 1) !== 0) {
    process.exit(result.status ?? 1);
  }
}

async function main() {
  run("npm", ["run", "lint"], workspaceRoot);
  run("npm", ["run", "contracts:check"], workspaceRoot);
  run("npm", ["run", "backend:test"], workspaceRoot);
  run("npm", ["run", "build"], frontendRoot);

  // desktop:build runs `wails build` (frontend build + sync + Go compile).
  run("npm", ["run", "desktop:build"], workspaceRoot);

  if (!fs.existsSync(releaseBinary)) {
    throw new Error(`Expected desktop release binary at ${releaseBinary}`);
  }

  console.log("Release smoke test passed: Wails desktop binary built.");
}

await main();
