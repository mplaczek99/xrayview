import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import {
  binariesDir,
  bundleDir,
  frontendRoot,
  releaseDir,
  prepareTauriTarget,
  tauriRoot,
} from "./prepare-tauri-target.mjs";
import { resolveGoSidecarTargetTriple } from "./prepare-go-sidecar.mjs";

const workspaceRoot = path.resolve(frontendRoot, "..");
const includeBundles = process.argv.includes("--bundle");
const tauriConfigPath = path.join(tauriRoot, "tauri.conf.json");
const capabilityPath = path.join(tauriRoot, "capabilities", "default.json");
const releaseBinary = path.join(
  releaseDir,
  process.platform === "win32" ? "xrayview-frontend.exe" : "xrayview-frontend",
);

function run(command, args, cwd) {
  const result = spawnSync(command, args, {
    cwd,
    env: { ...process.env },
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

prepareTauriTarget();

const tauriConfig = JSON.parse(fs.readFileSync(tauriConfigPath, "utf8"));
const externalBins = tauriConfig.bundle?.externalBin ?? [];
if (!externalBins.includes("binaries/xrayview-go-backend")) {
  throw new Error("Tauri bundle config must declare the Go sidecar external binary.");
}

const defaultCapability = JSON.parse(fs.readFileSync(capabilityPath, "utf8"));
if ((defaultCapability.permissions ?? []).length !== 0) {
  throw new Error("Default desktop capability should not grant broad core permissions.");
}

run("cargo", ["test", "--manifest-path", "backend/Cargo.toml"], workspaceRoot);
run("npm", ["run", "build"], frontendRoot);

const tauriArgs = ["./scripts/tauri-build.mjs", "--ci"];
if (!includeBundles) {
  tauriArgs.push("--no-bundle");
}
run("node", tauriArgs, frontendRoot);

if (!fs.existsSync(releaseBinary)) {
  throw new Error(`Expected desktop release binary at ${releaseBinary}`);
}

const targetTriple = resolveGoSidecarTargetTriple();
const sidecarFileName =
  process.platform === "win32"
    ? `xrayview-go-backend-${targetTriple}.exe`
    : `xrayview-go-backend-${targetTriple}`;
const sidecarPath = path.join(binariesDir, sidecarFileName);
if (!fs.existsSync(sidecarPath)) {
  throw new Error(`Expected Go sidecar binary at ${sidecarPath}`);
}

if (includeBundles) {
  if (!fs.existsSync(bundleDir)) {
    throw new Error(`Expected bundle artifacts under ${bundleDir}`);
  }

  const bundleEntries = fs.readdirSync(bundleDir);
  if (bundleEntries.length === 0) {
    throw new Error(`Bundle directory ${bundleDir} is empty`);
  }
}

console.log(
  includeBundles
    ? "Release smoke test passed with bundle artifacts."
    : "Release smoke test passed without bundle generation.",
);
