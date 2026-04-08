import { spawnSync } from "node:child_process";
import { applyFrontendRuntimeEnv } from "./runtime-env.mjs";

const result = spawnSync(
  "vite",
  ["build", "--config", "vite.wails-prototype.config.ts", ...process.argv.slice(2)],
  {
    env: applyFrontendRuntimeEnv(process.env),
    stdio: "inherit",
    shell: process.platform === "win32",
  },
);

if (result.error) {
  throw result.error;
}

process.exit(result.status ?? 0);
