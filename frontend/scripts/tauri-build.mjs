import { spawnSync } from "node:child_process";
import { existsSync, mkdtempSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const env = { ...process.env };
const args = [...process.argv.slice(2)];
let tempConfigDir;

if (process.platform === "linux") {
  env.NO_STRIP = env.NO_STRIP || "1";
  env.RUST_MIN_STACK = env.RUST_MIN_STACK || "16777216";
}

if (process.platform === "linux" && shouldBundleAppImage(args)) {
  tempConfigDir = mkdtempSync(join(tmpdir(), "xrayview-tauri-build-"));
  const appImageConfigPath = join(tempConfigDir, "appimage-libs.json");
  writeFileSync(
    appImageConfigPath,
    JSON.stringify(
      {
        bundle: {
          linux: {
            appimage: {
              files: Object.fromEntries(
                [
                  "libEGL.so.1",
                  "libGLdispatch.so.0",
                  "libGLX.so.0",
                  "libX11-xcb.so.1",
                  "libgbm.so.1",
                  "libdrm.so.2",
                  "libharfbuzz.so.0",
                ].map((soname) => [`usr/lib/${soname}`, resolveSharedLibrary(soname)]),
              ),
            },
          },
        },
      },
      null,
      2,
    ),
  );
  args.push("--config", appImageConfigPath);
}

const result = spawnSync("tauri", ["build", ...args], {
  cwd: process.cwd(),
  env,
  stdio: "inherit",
  shell: process.platform === "win32",
});

if (tempConfigDir) {
  rmSync(tempConfigDir, { recursive: true, force: true });
}

if (result.error) {
  throw result.error;
}

process.exit(result.status ?? 1);

function resolveSharedLibrary(soname) {
  const candidates = [
    join("/usr/lib", soname),
    join("/usr/lib/x86_64-linux-gnu", soname),
    join("/lib/x86_64-linux-gnu", soname),
  ];

  const path = candidates.find((candidate) => existsSync(candidate));
  if (!path) {
    throw new Error(`Unable to resolve ${soname}`);
  }

  return realpathSync(path);
}

function shouldBundleAppImage(args) {
  if (args.includes("--no-bundle")) {
    return false;
  }

  const bundleValues = [];

  for (let index = 0; index < args.length; index += 1) {
    const arg = args[index];

    if (arg === "--bundles" || arg === "-b") {
      bundleValues.push(args[index + 1] ?? "");
    } else if (arg.startsWith("--bundles=")) {
      bundleValues.push(arg.slice("--bundles=".length));
    }
  }

  if (!bundleValues.length) {
    return true;
  }

  return bundleValues.some((value) => value.split(/[,\s]+/).includes("appimage"));
}
