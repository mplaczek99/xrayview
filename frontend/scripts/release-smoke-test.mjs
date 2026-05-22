import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const frontendRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const workspaceRoot = path.resolve(frontendRoot, "..");
const smokeArgs = process.argv.slice(2);
const includeBundles = smokeArgs.includes("--bundle");
const releaseBinary = path.join(
  workspaceRoot,
  "desktop-tauri",
  "target",
  "release",
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
  run("npm", ["run", "contracts:check"], workspaceRoot);
  run("npm", ["run", "backend:test"], workspaceRoot);
  run("npm", ["run", "build"], frontendRoot);

  const buildArgs = ["run", "tauri:build", "--"];
  if (!includeBundles) {
    buildArgs.push("--no-bundle");
  }
  run("npm", buildArgs, workspaceRoot);

  if (!fs.existsSync(releaseBinary)) {
    throw new Error(`Expected desktop release binary at ${releaseBinary}`);
  }

  console.log(
    includeBundles
      ? "Release smoke test passed: Tauri binary and bundles built."
      : "Release smoke test passed: Tauri binary built (skip bundles).",
  );
}

await main();
