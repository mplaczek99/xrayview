import { mkdirSync } from "node:fs";
import { spawnSync } from "node:child_process";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { applyFrontendRuntimeEnv } from "../../frontend/scripts/runtime-env.mjs";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(scriptDir, "..", "..");
const desktopDir = path.join(repoRoot, "desktop");
const buildBinDir = path.join(desktopDir, "build", "bin");
const homeDir = process.env.HOME ?? process.env.USERPROFILE;
const tempDir = process.env.RUNNER_TEMP ?? os.tmpdir();
const defaultGoPath = homeDir ? path.join(homeDir, "go") : undefined;
const buildEnv = {
  ...applyFrontendRuntimeEnv(process.env),
  GOCACHE: process.env.GOCACHE ?? path.join(tempDir, "xrayview-go-build-cache"),
  GOMODCACHE:
    process.env.GOMODCACHE ?? (defaultGoPath ? path.join(defaultGoPath, "pkg", "mod") : undefined),
  GOTMPDIR: process.env.GOTMPDIR ?? path.join(tempDir, "xrayview-go-tmp"),
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

function hasPkgConfigPackage(name) {
  const probe = spawnSync("pkg-config", ["--exists", name], {
    cwd: repoRoot,
    shell: false,
  });
  return (probe.status ?? 1) === 0;
}

function ensureLinuxDesktopPrereqs() {
  if (process.platform !== "linux") {
    return;
  }

  const hasGtk3 = hasPkgConfigPackage("gtk+-3.0");
  const hasWebkit41 = hasPkgConfigPackage("webkit2gtk-4.1");
  const hasWebkit40 = hasPkgConfigPackage("webkit2gtk-4.0");

  if (hasGtk3 && (hasWebkit41 || hasWebkit40)) {
    return;
  }

  const missingPackages = [];
  if (!hasGtk3) {
    missingPackages.push("gtk+-3.0");
  }
  if (!hasWebkit41 && !hasWebkit40) {
    missingPackages.push("webkit2gtk-4.1 or webkit2gtk-4.0");
  }

  process.stderr.write(
    [
      "Missing Linux desktop build prerequisites.",
      `Install ${missingPackages.join(" and ")} before running npm run wails:build.`,
      "On Debian/Ubuntu this is typically provided by libgtk-3-dev plus either libwebkit2gtk-4.1-dev or libwebkit2gtk-4.0-dev.",
    ].join("\n") + "\n",
  );
  process.exit(1);
}

function detectWailsTags() {
  const tags = ["desktop", "production"];
  if (process.platform === "linux") {
    if (hasPkgConfigPackage("webkit2gtk-4.1")) {
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
  path.join(repoRoot, "backend"),
  "build",
  "-o",
  path.join(buildBinDir, binaryName("xrayview-backend")),
  "./cmd/xrayviewd",
]);
ensureLinuxDesktopPrereqs();
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
