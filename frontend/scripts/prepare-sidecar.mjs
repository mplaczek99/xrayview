import { execSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const frontendRoot = path.resolve(scriptDir, "..");
const workspaceRoot = path.resolve(frontendRoot, "..");
const binariesDir = path.join(frontendRoot, "src-tauri", "binaries");
const extension = process.platform === "win32" ? ".exe" : "";
const targetTriple = execSync("rustc --print host-tuple", {
  cwd: frontendRoot,
  encoding: "utf8",
}).trim();

if (!targetTriple) {
  throw new Error("Could not determine the Rust target triple.");
}

fs.mkdirSync(binariesDir, { recursive: true });

for (const entry of fs.readdirSync(binariesDir)) {
  if (entry.startsWith("xrayview-backend-")) {
    fs.rmSync(path.join(binariesDir, entry), { force: true });
  }
}

const outputPath = path.join(binariesDir, `xrayview-backend-${targetTriple}${extension}`);

execSync(`cargo build --release --manifest-path backend-rust/Cargo.toml`, {
  cwd: workspaceRoot,
  stdio: "inherit",
});

const builtBinary = path.join(
  workspaceRoot,
  "backend-rust",
  "target",
  "release",
  `xrayview-backend-rust${extension}`,
);

fs.copyFileSync(builtBinary, outputPath);

console.log(`Prepared backend sidecar at ${outputPath}`);
