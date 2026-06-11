// Starts the Vite dev server for `wails dev` hot reload. Wails proxies the URL
// declared in wails.json (frontend:dev:serverUrl). Host/port must stay in sync
// with that value.
import { spawnSync } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";

const desktopRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const workspaceRoot = path.resolve(desktopRoot, "..");

const result = spawnSync(
  "npm",
  ["run", "dev", "--", "--host", "localhost", "--port", "5173", "--strictPort"],
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
