import path from "node:path";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { applyFrontendRuntimeEnv } from "../../frontend/scripts/runtime-env.mjs";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(scriptDir, "..", "..");
const buildScript = path.join(repoRoot, "desktop", "scripts", "build.mjs");
const binaryPath = path.join(
  repoRoot,
  "desktop",
  "build",
  "bin",
  process.platform === "win32" ? "xrayview.exe" : "xrayview",
);
const launchEnv = applyFrontendRuntimeEnv(process.env);

function run(command, args, env = launchEnv) {
  const result = spawnSync(command, args, {
    cwd: repoRoot,
    env,
    stdio: "inherit",
    shell: process.platform === "win32",
  });

  if (result.error) {
    throw result.error;
  }

  if ((result.status ?? 0) !== 0) {
    process.exit(result.status ?? 1);
  }
}

run("node", [buildScript]);
run(binaryPath, []);
