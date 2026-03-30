import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const frontendRoot = path.resolve(scriptDir, "..");
const targetDir = path.join(frontendRoot, "src-tauri", "target");
const releaseDir = path.join(frontendRoot, "src-tauri", "target", "release");
const bundleDir = path.join(releaseDir, "bundle");
const args = process.argv.slice(2);
const env = { ...process.env };

function removeStaleTargetArtifacts(dir) {
  if (!fs.existsSync(dir)) {
    return;
  }

  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const entryPath = path.join(dir, entry.name);

    if (
      entry.name.includes("xrayview-frontend-app") ||
      entry.name.includes("xrayview_frontend_app")
    ) {
      fs.rmSync(entryPath, { force: true, recursive: true });
      continue;
    }

    if (entry.isDirectory()) {
      removeStaleTargetArtifacts(entryPath);
    }
  }
}

removeStaleTargetArtifacts(targetDir);

if (fs.existsSync(bundleDir)) {
  fs.rmSync(bundleDir, { force: true, recursive: true });
}

if (process.platform === "linux") {
  env.APPIMAGE_EXTRACT_AND_RUN ??= "1";
  env.NO_STRIP ??= "1";
}

const tauriCommand = process.platform === "win32" ? "tauri.cmd" : "tauri";
const result = spawnSync(tauriCommand, ["build", ...args], {
  cwd: frontendRoot,
  env,
  stdio: "inherit",
});

if (result.error) {
  throw result.error;
}

process.exit(result.status ?? 0);
