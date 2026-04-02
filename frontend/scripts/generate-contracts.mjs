import { spawnSync } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const frontendRoot = path.resolve(scriptDir, "..");
const workspaceRoot = path.resolve(frontendRoot, "..");
const outputPath = path.join(frontendRoot, "src", "lib", "generated", "contracts.ts");

const result = spawnSync(
  "cargo",
  [
    "run",
    "--quiet",
    "--manifest-path",
    "backend/Cargo.toml",
    "--bin",
    "generate-contracts",
    "--",
    outputPath,
  ],
  {
    cwd: workspaceRoot,
    stdio: "inherit",
    shell: process.platform === "win32",
  },
);

if (result.error) {
  throw result.error;
}

process.exit(result.status ?? 0);
