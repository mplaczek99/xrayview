import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));

export const frontendRoot = path.resolve(scriptDir, "..");
export const tauriRoot = path.join(frontendRoot, "src-tauri");
export const targetDir = path.join(tauriRoot, "target");
export const releaseDir = path.join(targetDir, "release");
export const bundleDir = path.join(releaseDir, "bundle");
export const binariesDir = path.join(tauriRoot, "binaries");

function removeRenamedBinaryArtifacts(dir) {
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
      removeRenamedBinaryArtifacts(entryPath);
    }
  }
}

function removeLegacySidecars() {
  if (!fs.existsSync(binariesDir)) {
    return;
  }

  for (const entry of fs.readdirSync(binariesDir, { withFileTypes: true })) {
    if (
      !entry.isFile() ||
      (!entry.name.startsWith("xrayview-backend-") &&
        !entry.name.startsWith("xrayview-go-backend-"))
    ) {
      continue;
    }

    fs.rmSync(path.join(binariesDir, entry.name), { force: true });
  }
}

function rootOutputFiles(dir) {
  if (!fs.existsSync(dir)) {
    return [];
  }

  const files = [];
  const stack = [dir];
  while (stack.length > 0) {
    const currentDir = stack.pop();
    if (!currentDir) {
      continue;
    }

    for (const entry of fs.readdirSync(currentDir, { withFileTypes: true })) {
      const entryPath = path.join(currentDir, entry.name);
      if (entry.isDirectory()) {
        stack.push(entryPath);
        continue;
      }
      if (entry.name === "root-output") {
        files.push(entryPath);
      }
    }
  }

  return files;
}

function targetHasForeignWorkspaceArtifacts() {
  // Cargo leaves root-output breadcrumbs when a shared target dir is being
  // redirected; if those point elsewhere, reset to a clean app-local target.
  for (const rootOutputPath of rootOutputFiles(targetDir)) {
    const outputPath = fs.readFileSync(rootOutputPath, "utf8").trim();
    if (outputPath === "") {
      continue;
    }
    if (!outputPath.startsWith(targetDir)) {
      return true;
    }
  }

  return false;
}

export function prepareTauriTarget() {
  removeRenamedBinaryArtifacts(targetDir);
  removeLegacySidecars();

  if (targetHasForeignWorkspaceArtifacts()) {
    fs.rmSync(targetDir, { force: true, recursive: true });
  }
}
