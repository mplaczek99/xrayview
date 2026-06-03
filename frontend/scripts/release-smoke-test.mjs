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
const bundleRoot = path.join(workspaceRoot, "desktop-tauri", "target", "release", "bundle");

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

function findFilesByExtension(root, extension) {
  if (!fs.existsSync(root)) {
    return [];
  }

  const matches = [];
  const entries = fs.readdirSync(root, { withFileTypes: true });
  for (const entry of entries) {
    const entryPath = path.join(root, entry.name);
    if (entry.isDirectory()) {
      matches.push(...findFilesByExtension(entryPath, extension));
    } else if (entry.isFile() && entry.name.endsWith(extension)) {
      matches.push(entryPath);
    }
  }
  return matches;
}

async function main() {
  run("npm", ["run", "lint"], workspaceRoot);
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

  if (includeBundles && process.platform === "linux") {
    const appImages = findFilesByExtension(bundleRoot, ".AppImage");
    if (appImages.length === 0) {
      throw new Error(`Expected Linux AppImage bundle under ${bundleRoot}`);
    }
  }

  console.log(
    includeBundles
      ? "Release smoke test passed: Tauri binary and bundles built."
      : "Release smoke test passed: Tauri binary built (skip bundles).",
  );
}

await main();
