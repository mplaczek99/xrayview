import { spawn } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { applyFrontendRuntimeEnv } from "./runtime-env.mjs";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const frontendRoot = path.resolve(scriptDir, "..");
const tscCliPath = path.join(frontendRoot, "node_modules", "typescript", "bin", "tsc");
const viteCliPath = path.join(frontendRoot, "node_modules", "vite", "bin", "vite.js");

function runCollected(cmd, args) {
  return new Promise((resolve) => {
    const chunks = [];
    const proc = spawn(cmd, args);
    proc.stdout.on("data", (d) => chunks.push({ stream: "stdout", data: d }));
    proc.stderr.on("data", (d) => chunks.push({ stream: "stderr", data: d }));
    proc.on("error", (err) => {
      process.stderr.write(`Failed to start ${cmd}: ${err.message}\n`);
      resolve({ code: 1, chunks });
    });
    proc.on("close", (code) => resolve({ code: code ?? 0, chunks }));
  });
}

function runInherited(cmd, args, env) {
  return new Promise((resolve) => {
    const proc = spawn(cmd, args, {
      env,
      stdio: "inherit",
    });
    proc.on("error", (err) => {
      process.stderr.write(`Failed to start ${cmd}: ${err.message}\n`);
      resolve(1);
    });
    proc.on("close", (code) => resolve(code ?? 0));
  });
}

const viteArgs = ["build", ...process.argv.slice(2)];
const viteEnv = applyFrontendRuntimeEnv(process.env);

const [tscResult, viteCode] = await Promise.all([
  runCollected(process.execPath, [tscCliPath, "--noEmit"]),
  runInherited(process.execPath, [viteCliPath, ...viteArgs], viteEnv),
]);

for (const { stream, data } of tscResult.chunks) {
  process[stream].write(data);
}

if (tscResult.code !== 0 || viteCode !== 0) {
  process.exit(tscResult.code || viteCode || 1);
}
