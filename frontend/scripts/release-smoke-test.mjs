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
import {
  resolveDesktopRuntimeConfig,
  validateDesktopLaunch,
} from "./release-launch-smoke.mjs";

const workspaceRoot = path.resolve(frontendRoot, "..");
const smokeArgs = process.argv.slice(2);
const includeBundles = smokeArgs.includes("--bundle");
const forwardedTauriArgs = smokeArgs.filter((arg) => arg !== "--bundle");
const tauriConfigPath = path.join(tauriRoot, "tauri.conf.json");
const capabilityPath = path.join(tauriRoot, "capabilities", "default.json");
const goBuildCacheDir = path.join("/tmp", "xrayview-go-build-cache");
const goTmpDir = path.join("/tmp", "xrayview-go-tmp");
const goPathDir = path.join("/tmp", "xrayview-go-path");
const releaseBinary = path.join(
  releaseDir,
  process.platform === "win32" ? "xrayview-frontend.exe" : "xrayview-frontend",
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

function parseFlagValue(args, flagName) {
  for (let index = 0; index < args.length; index += 1) {
    const arg = args[index];
    if (arg === flagName) {
      return args[index + 1] ?? "";
    }

    if (arg.startsWith(`${flagName}=`)) {
      return arg.slice(flagName.length + 1);
    }
  }

  return null;
}

function requestedBundleKinds(args) {
  const bundleValue = parseFlagValue(args, "--bundles");
  if (bundleValue === null) {
    return null;
  }

  return bundleValue
    .split(",")
    .map((value) => value.trim().toLowerCase())
    .filter(Boolean);
}

function bundleEntriesForKind(bundleKind) {
  const kindDir = path.join(bundleDir, bundleKind);
  if (!fs.existsSync(kindDir)) {
    return [];
  }

  return fs.readdirSync(kindDir);
}

function assertBundleArtifacts(args) {
  if (!fs.existsSync(bundleDir)) {
    throw new Error(`Expected bundle artifacts under ${bundleDir}`);
  }

  const requestedKinds = requestedBundleKinds(args);
  if (requestedKinds === null || requestedKinds.includes("all")) {
    const bundleEntries = fs.readdirSync(bundleDir);
    if (bundleEntries.length === 0) {
      throw new Error(`Bundle directory ${bundleDir} is empty`);
    }
    return;
  }

  for (const bundleKind of requestedKinds) {
    const entries = bundleEntriesForKind(bundleKind);
    if (entries.length === 0) {
      throw new Error(
        `Expected ${bundleKind} bundle artifacts under ${path.join(bundleDir, bundleKind)}`,
      );
    }
  }
}

function resolveBundleLaunchTarget(args) {
  const requestedKinds = requestedBundleKinds(args);
  const appImageRequested =
    process.platform === "linux" &&
    (requestedKinds === null ||
      requestedKinds.includes("all") ||
      requestedKinds.includes("appimage"));
  if (!appImageRequested) {
    return null;
  }

  const appImageDir = path.join(bundleDir, "appimage");
  if (!fs.existsSync(appImageDir)) {
    return null;
  }

  const appImages = fs
    .readdirSync(appImageDir)
    .filter((entry) => entry.endsWith(".AppImage"))
    .sort();
  if (appImages.length === 0) {
    return null;
  }

  return path.join(appImageDir, appImages[0]);
}

async function main() {
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

  const tauriArgs = ["./scripts/tauri-build.mjs", "--ci", ...forwardedTauriArgs];
  if (!includeBundles && !forwardedTauriArgs.includes("--no-bundle")) {
    tauriArgs.push("--no-bundle");
  }
  run("node", tauriArgs, frontendRoot);

  if (!fs.existsSync(releaseBinary)) {
    throw new Error(`Expected desktop release binary at ${releaseBinary}`);
  }

  const targetTriple = resolveGoSidecarTargetTriple(forwardedTauriArgs);
  const sidecarFileName =
    process.platform === "win32"
      ? `xrayview-go-backend-${targetTriple}.exe`
      : `xrayview-go-backend-${targetTriple}`;
  const sidecarPath = path.join(binariesDir, sidecarFileName);
  if (!fs.existsSync(sidecarPath)) {
    throw new Error(`Expected Go sidecar binary at ${sidecarPath}`);
  }

  const runtimeConfig = resolveDesktopRuntimeConfig(process.env);
  const releaseLaunchResult = await validateDesktopLaunch({
    executablePath: releaseBinary,
    label: "release binary",
    runtimeConfig,
  });
  if (releaseLaunchResult?.skipped) {
    console.warn(`[release-smoke] skipped release-binary launch validation: ${releaseLaunchResult.reason}`);
  }

  if (includeBundles) {
    assertBundleArtifacts(forwardedTauriArgs);

    const bundleLaunchTarget = resolveBundleLaunchTarget(forwardedTauriArgs);
    if (bundleLaunchTarget !== null) {
      const bundleLaunchResult = await validateDesktopLaunch({
        executablePath: bundleLaunchTarget,
        label: "bundled desktop app",
        runtimeConfig,
      });
      if (bundleLaunchResult?.skipped) {
        console.warn(`[release-smoke] skipped bundled launch validation: ${bundleLaunchResult.reason}`);
      }
    }
  }

  console.log(
    includeBundles
      ? "Release smoke test passed with bundle artifacts."
      : "Release smoke test passed without bundle generation.",
  );
}

await main();
