// Shared helpers for the desktop (Wails) npm scripts: locate the repo's desktop
// module and the `wails` CLI. The CLI is installed via `go install` and may live
// in GOPATH/bin rather than on PATH, so fall back to that location.
import { spawnSync } from "node:child_process";
import { existsSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

export const frontendRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
export const workspaceRoot = path.resolve(frontendRoot, "..");
export const desktopRoot = path.join(workspaceRoot, "desktop");

export function resolveWails() {
  const lookup = process.platform === "win32" ? "where" : "which";
  const onPath = spawnSync(lookup, ["wails"], { encoding: "utf8" });
  if ((onPath.status ?? 1) === 0) {
    return "wails";
  }

  const gopath = spawnSync("go", ["env", "GOPATH"], { encoding: "utf8" });
  if ((gopath.status ?? 1) === 0 && gopath.stdout.trim()) {
    const binary = process.platform === "win32" ? "wails.exe" : "wails";
    const candidate = path.join(gopath.stdout.trim(), "bin", binary);
    if (existsSync(candidate)) {
      return candidate;
    }
  }

  throw new Error(
    "wails CLI not found. Install it: go install github.com/wailsapp/wails/v2/cmd/wails@latest",
  );
}

export function runWails(subcommand, extraArgs) {
  const result = spawnSync(resolveWails(), [subcommand, "-tags", "webkit2_41", ...extraArgs], {
    cwd: desktopRoot,
    env: { ...process.env, GOCACHE: path.join(desktopRoot, ".gocache") },
    stdio: "inherit",
    shell: process.platform === "win32",
  });
  if (result.error) {
    throw result.error;
  }
  process.exit(result.status ?? 1);
}
