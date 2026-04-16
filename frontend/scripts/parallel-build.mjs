import { spawn } from "node:child_process";
import { applyFrontendRuntimeEnv } from "./runtime-env.mjs";

function runCollected(cmd, args) {
  return new Promise((resolve) => {
    const chunks = [];
    const proc = spawn(cmd, args, {
      shell: process.platform === "win32",
    });
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
      shell: process.platform === "win32",
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
  runCollected("node_modules/.bin/tsc", ["--noEmit"]),
  runInherited("node_modules/.bin/vite", viteArgs, viteEnv),
]);

for (const { stream, data } of tscResult.chunks) {
  process[stream].write(data);
}

if (tscResult.code !== 0 || viteCode !== 0) {
  process.exit(tscResult.code || viteCode || 1);
}
