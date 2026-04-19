import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(scriptDir, "..", "..");
const buildRoot = path.join(repoRoot, "desktop", "build");
const outputDir = resolveOutputDir(process.argv.slice(2));
const shellBinary = process.platform === "win32" ? "xrayview.exe" : "xrayview";
const sidecarBinary = process.platform === "win32" ? "xrayview-backend.exe" : "xrayview-backend";

stageReleasePayload(outputDir);

function resolveOutputDir(args) {
  for (let index = 0; index < args.length; index += 1) {
    if (args[index] === "--output") {
      const value = args[index + 1];
      if (!value) {
        break;
      }

      return path.resolve(value);
    }
  }

  throw new Error("usage: node desktop/scripts/stage-release-payload.mjs --output <directory>");
}

function stageReleasePayload(targetDir) {
  const binDir = path.join(buildRoot, "bin");
  const frontendDistDir = path.join(buildRoot, "frontend", "dist");
  const sources = [
    {
      source: path.join(binDir, shellBinary),
      target: path.join(targetDir, "bin", shellBinary),
    },
    {
      source: path.join(binDir, sidecarBinary),
      target: path.join(targetDir, "bin", sidecarBinary),
    },
    {
      source: frontendDistDir,
      target: path.join(targetDir, "frontend", "dist"),
    },
  ];

  for (const entry of sources) {
    assertExists(entry.source);
  }

  fs.rmSync(targetDir, { recursive: true, force: true });
  fs.mkdirSync(targetDir, { recursive: true });

  for (const entry of sources) {
    fs.mkdirSync(path.dirname(entry.target), { recursive: true });
    fs.cpSync(entry.source, entry.target, { recursive: true });
  }
}

function assertExists(targetPath) {
  if (!fs.existsSync(targetPath)) {
    throw new Error(
      `missing build output at ${targetPath}; run npm run release:smoke before staging release payloads`,
    );
  }
}
