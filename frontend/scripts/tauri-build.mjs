import { spawnSync } from "node:child_process";
import fs from "node:fs";
import { prepareLinuxAppImageRuntime } from "./appimage-runtime.mjs";
import { bundleDir, frontendRoot, prepareTauriTarget } from "./prepare-tauri-target.mjs";
import { prepareGoSidecarBinary } from "./prepare-go-sidecar.mjs";
import { applyFrontendRuntimeEnv } from "./runtime-env.mjs";

const args = process.argv.slice(2);
const env = applyFrontendRuntimeEnv(process.env);

function parseFlagValue(argv, flagName) {
  for (let index = 0; index < argv.length; index += 1) {
    const arg = argv[index];
    if (arg === flagName) {
      return argv[index + 1] ?? "";
    }

    if (arg.startsWith(`${flagName}=`)) {
      return arg.slice(flagName.length + 1);
    }
  }

  return null;
}

function shouldPrepareLinuxAppImageRuntime(argv) {
  if (process.platform !== "linux") {
    return false;
  }

  if (argv.includes("--no-bundle")) {
    return false;
  }

  const bundleValue = parseFlagValue(argv, "--bundles");
  if (bundleValue === null) {
    return true;
  }

  return bundleValue
    .split(",")
    .map((value) => value.trim().toLowerCase())
    .some((value) => value === "all" || value === "appimage");
}

prepareTauriTarget();
prepareGoSidecarBinary(args);

// Clear previous bundles so packaging only picks up artifacts from this build.
if (fs.existsSync(bundleDir)) {
  fs.rmSync(bundleDir, { force: true, recursive: true });
}

if (process.platform === "linux") {
  // These flags make local and CI AppImage packaging more predictable on Linux.
  env.APPIMAGE_EXTRACT_AND_RUN ??= "1";
  env.NO_STRIP ??= "1";

  if (!env.LDAI_RUNTIME_FILE && shouldPrepareLinuxAppImageRuntime(args)) {
    const runtimeFile = prepareLinuxAppImageRuntime();
    if (runtimeFile) {
      env.LDAI_RUNTIME_FILE = runtimeFile;
    }
  }
}

const result = spawnSync("tauri", ["build", ...args], {
  cwd: frontendRoot,
  env,
  stdio: "inherit",
  shell: process.platform === "win32",
});

if (result.error) {
  throw result.error;
}

process.exit(result.status ?? 0);
