import { mkdirSync } from "node:fs";
import { spawnSync } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(scriptDir, "..", "..");
const prototypeDir = path.join(repoRoot, "wails-prototype");
const buildBinDir = path.join(prototypeDir, "build", "bin");

function run(command, args, options = {}) {
  const result = spawnSync(command, args, {
    cwd: repoRoot,
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

run("npm", ["--prefix", "frontend", "run", "wails:prototype:build"]);
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
  prototypeDir,
  "build",
  "-tags",
  detectWailsTags(),
  "-o",
  path.join(buildBinDir, binaryName("xrayview-wails-prototype")),
  ".",
]);
