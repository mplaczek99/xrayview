import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { binariesDir, frontendRoot } from "./prepare-tauri-target.mjs";

const SIDECAR_BASE_NAME = "xrayview-go-backend";
const workspaceRoot = path.resolve(frontendRoot, "..");
const goBackendRoot = path.join(workspaceRoot, "go-backend");
const thisFilePath = fileURLToPath(import.meta.url);
const goBuildCacheDir = path.join("/tmp", "xrayview-go-build-cache");
const goTmpDir = path.join("/tmp", "xrayview-go-tmp");

function parseTargetArg(args) {
  for (let index = 0; index < args.length; index += 1) {
    const arg = args[index];
    if (arg === "--target") {
      return args[index + 1] ?? null;
    }
    if (arg.startsWith("--target=")) {
      return arg.slice("--target=".length);
    }
  }

  return null;
}

function defaultTargetTriple() {
  if (process.platform === "darwin") {
    if (process.arch === "arm64") {
      return "aarch64-apple-darwin";
    }
    if (process.arch === "x64") {
      return "x86_64-apple-darwin";
    }
  }

  if (process.platform === "linux") {
    if (process.arch === "arm64") {
      return "aarch64-unknown-linux-gnu";
    }
    if (process.arch === "arm") {
      return "armv7-unknown-linux-gnueabihf";
    }
    if (process.arch === "x64") {
      return "x86_64-unknown-linux-gnu";
    }
  }

  if (process.platform === "win32") {
    if (process.arch === "arm64") {
      return "aarch64-pc-windows-msvc";
    }
    if (process.arch === "ia32") {
      return "i686-pc-windows-msvc";
    }
    if (process.arch === "x64") {
      return "x86_64-pc-windows-msvc";
    }
  }

  throw new Error(
    `Unsupported host platform/arch for Go sidecar build: ${process.platform}/${process.arch}`,
  );
}

function goTargetForTriple(targetTriple) {
  switch (targetTriple) {
    case "x86_64-unknown-linux-gnu":
    case "x86_64-unknown-linux-musl":
      return { goos: "linux", goarch: "amd64" };
    case "aarch64-unknown-linux-gnu":
    case "aarch64-unknown-linux-musl":
      return { goos: "linux", goarch: "arm64" };
    case "armv7-unknown-linux-gnueabihf":
      return { goos: "linux", goarch: "arm", goarm: "7" };
    case "x86_64-apple-darwin":
      return { goos: "darwin", goarch: "amd64" };
    case "aarch64-apple-darwin":
      return { goos: "darwin", goarch: "arm64" };
    case "x86_64-pc-windows-msvc":
      return { goos: "windows", goarch: "amd64" };
    case "aarch64-pc-windows-msvc":
      return { goos: "windows", goarch: "arm64" };
    case "i686-pc-windows-msvc":
      return { goos: "windows", goarch: "386" };
    default:
      throw new Error(`Unsupported Rust target triple for Go sidecar build: ${targetTriple}`);
  }
}

function sidecarExecutableName(targetTriple) {
  return targetTriple.includes("windows")
    ? `${SIDECAR_BASE_NAME}-${targetTriple}.exe`
    : `${SIDECAR_BASE_NAME}-${targetTriple}`;
}

export function resolveGoSidecarTargetTriple(args = process.argv.slice(2)) {
  return parseTargetArg(args) ?? defaultTargetTriple();
}

export function prepareGoSidecarBinary(args = process.argv.slice(2)) {
  const targetTriple = resolveGoSidecarTargetTriple(args);
  const target = goTargetForTriple(targetTriple);
  const outputPath = path.join(binariesDir, sidecarExecutableName(targetTriple));
  const goEnv = {
    ...process.env,
    GOCACHE: process.env.GOCACHE ?? goBuildCacheDir,
    GOOS: target.goos,
    GOARCH: target.goarch,
    GOTMPDIR: process.env.GOTMPDIR ?? goTmpDir,
  };

  if (target.goarm) {
    goEnv.GOARM = target.goarm;
  }

  fs.mkdirSync(binariesDir, { recursive: true });
  fs.mkdirSync(goEnv.GOCACHE, { recursive: true });
  fs.mkdirSync(goEnv.GOTMPDIR, { recursive: true });

  const goArgs = ["build", "-o", outputPath];
  if (target.goos === "windows") {
    goArgs.push("-ldflags", "-H=windowsgui");
  }
  goArgs.push("./cmd/xrayviewd");

  const result = spawnSync("go", goArgs, {
    cwd: goBackendRoot,
    env: goEnv,
    stdio: "inherit",
    shell: process.platform === "win32",
  });

  if (result.error) {
    throw result.error;
  }

  if ((result.status ?? 1) !== 0) {
    process.exit(result.status ?? 1);
  }

  return {
    targetTriple,
    outputPath,
  };
}

if (process.argv[1] && path.resolve(process.argv[1]) === thisFilePath) {
  const { outputPath, targetTriple } = prepareGoSidecarBinary();
  console.log(`Prepared Go sidecar ${targetTriple} at ${outputPath}`);
}
