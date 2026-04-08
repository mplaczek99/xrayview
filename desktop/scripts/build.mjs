import { mkdirSync } from "node:fs";
import { spawnSync } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { applyFrontendRuntimeEnv } from "../../frontend/scripts/runtime-env.mjs";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(scriptDir, "..", "..");
const desktopDir = path.join(repoRoot, "desktop");
const buildBinDir = path.join(desktopDir, "build", "bin");
const defaultGoPath = process.env.HOME
  ? path.join(process.env.HOME, "go")
  : undefined;
const buildEnv = {
  ...applyFrontendRuntimeEnv(process.env),
  GOCACHE: process.env.GOCACHE ?? path.join("/tmp", "xrayview-go-build-cache"),
  GOMODCACHE:
    process.env.GOMODCACHE ?? (defaultGoPath ? path.join(defaultGoPath, "pkg", "mod") : undefined),
  GOTMPDIR: process.env.GOTMPDIR ?? path.join("/tmp", "xrayview-go-tmp"),
};

function run(command, args, options = {}) {
  const result = spawnSync(command, args, {
    cwd: repoRoot,
    env: buildEnv,
    stdio: "inherit",
    shell: process.platform === "win32",
    ...options,
  });

  if (result.error) {
    throw result.error;
  }

  if ((result.status ?? 0) !== 0) {
    process.exit(result.status ?? 1);
  }
}

function detectWailsTags() {
  const tags = ["desktop", "production"];
  if (process.platform === "linux") {
    const probe = spawnSync("pkg-config", ["--exists", "webkit2gtk-4.1"], {
      cwd: repoRoot,
      shell: false,
    });
    if ((probe.status ?? 1) === 0) {
      tags.push("webkit2_41");
    }
  }

  return tags.join(",");
}

function binaryName(baseName) {
  return process.platform === "win32" ? `${baseName}.exe` : baseName;
}

mkdirSync(buildBinDir, { recursive: true });
mkdirSync(buildEnv.GOCACHE, { recursive: true });
if (buildEnv.GOMODCACHE) {
  mkdirSync(buildEnv.GOMODCACHE, { recursive: true });
}
mkdirSync(buildEnv.GOTMPDIR, { recursive: true });

run("npm", ["--prefix", "frontend", "run", "wails:build"]);
run("go", [
  "-C",
  path.join(repoRoot, "go-backend"),
  "build",
  "-o",
  path.join(buildBinDir, binaryName("xrayview-go-backend")),
  "./cmd/xrayviewd",
]);
run("go", [
  "-C",
  desktopDir,
  "build",
  "-tags",
  detectWailsTags(),
  "-o",
  path.join(buildBinDir, binaryName("xrayview")),
  ".",
]);
