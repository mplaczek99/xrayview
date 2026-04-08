import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import {
  resolveDesktopRuntimeConfig,
  validateDesktopLaunch,
} from "./release-launch-smoke.mjs";

const frontendRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const workspaceRoot = path.resolve(frontendRoot, "..");
const smokeArgs = process.argv.slice(2);
const includeBundles = smokeArgs.includes("--bundle");
const goBuildCacheDir = path.join("/tmp", "xrayview-go-build-cache");
const goTmpDir = path.join("/tmp", "xrayview-go-tmp");
const goPathDir = path.join("/tmp", "xrayview-go-path");
const releaseBinary = path.join(
  workspaceRoot,
  "wails-prototype",
  "build",
  "bin",
  process.platform === "win32" ? "xrayview.exe" : "xrayview",
);
const sidecarBinary = path.join(
  workspaceRoot,
  "wails-prototype",
  "build",
  "bin",
  process.platform === "win32" ? "xrayview-go-backend.exe" : "xrayview-go-backend",
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
  fs.mkdirSync(goBuildCacheDir, { recursive: true });
  fs.mkdirSync(goTmpDir, { recursive: true });
  fs.mkdirSync(goPathDir, { recursive: true });
  run("npm", ["run", "go:backend:test"], workspaceRoot, {
    GOCACHE: process.env.GOCACHE ?? goBuildCacheDir,
    GOTMPDIR: process.env.GOTMPDIR ?? goTmpDir,
    GOPATH: process.env.GOPATH ?? goPathDir,
  });
  run("npm", ["run", "build"], frontendRoot);
  run("npm", ["run", "wails:build"], workspaceRoot, {
    GOCACHE: process.env.GOCACHE ?? goBuildCacheDir,
    GOTMPDIR: process.env.GOTMPDIR ?? goTmpDir,
    GOPATH: process.env.GOPATH ?? goPathDir,
  });

  if (!fs.existsSync(releaseBinary)) {
    throw new Error(`Expected desktop release binary at ${releaseBinary}`);
  }

  if (!fs.existsSync(sidecarBinary)) {
    throw new Error(`Expected Go sidecar binary at ${sidecarBinary}`);
  }

  const runtimeConfig = resolveDesktopRuntimeConfig(process.env);
  const releaseLaunchResult = await validateDesktopLaunch({
    executablePath: releaseBinary,
    label: "Wails desktop binary",
    runtimeConfig,
  });
  if (releaseLaunchResult?.skipped) {
    console.warn(`[release-smoke] skipped desktop launch validation: ${releaseLaunchResult.reason}`);
  }

  if (includeBundles) {
    console.warn(
      "[release-smoke] --bundle was requested, but the Wails desktop flow currently validates the built binary only.",
    );
  }

  console.log(
    includeBundles
      ? "Release smoke test passed; bundle verification is not implemented for the Wails shell yet."
      : "Release smoke test passed for the Wails desktop binary.",
  );
}

await main();
