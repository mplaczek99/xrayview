// Builds the real frontend (repo-root `npm run build` → frontend/dist) and syncs
// the output into desktop/dist, which main.go embeds via //go:embed all:dist.
// go:embed cannot reach outside the desktop module, hence the copy. Invoked by
// wails.json (frontend:build) and indirectly by `npm run desktop:build`.
import { spawnSync } from "node:child_process";
import { cpSync, mkdirSync, rmSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const desktopRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const workspaceRoot = path.resolve(desktopRoot, "..");
const frontendRoot = path.join(workspaceRoot, "frontend");
const distSrc = path.join(frontendRoot, "dist");
const distDest = path.join(desktopRoot, "dist");

const result = spawnSync("npm", ["run", "build"], {
  cwd: frontendRoot,
  stdio: "inherit",
  shell: process.platform === "win32",
});
if (result.error) {
  throw result.error;
}
if ((result.status ?? 1) !== 0) {
  process.exit(result.status ?? 1);
}

rmSync(distDest, { recursive: true, force: true });
mkdirSync(distDest, { recursive: true });
cpSync(distSrc, distDest, { recursive: true });
console.log(`Synced ${distSrc} -> ${distDest}`);
