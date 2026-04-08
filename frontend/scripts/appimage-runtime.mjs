import { spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { targetDir } from "./prepare-tauri-target.mjs";

function appImageArchitecture() {
  switch (process.arch) {
    case "x64":
      return "x86_64";
    case "arm64":
      return "aarch64";
    case "ia32":
      return "i686";
    default:
      throw new Error(`Unsupported Linux AppImage architecture: ${process.arch}`);
  }
}

function tauriToolCacheDir() {
  const xdgCacheHome = process.env.XDG_CACHE_HOME?.trim();
  if (xdgCacheHome) {
    return path.join(xdgCacheHome, "tauri");
  }

  return path.join(os.homedir(), ".cache", "tauri");
}

function resolveCachedToolAppImage() {
  const cacheDir = tauriToolCacheDir();
  const appImageArch = appImageArchitecture();
  const candidatePaths = [
    path.join(cacheDir, "linuxdeploy-plugin-appimage.AppImage"),
    path.join(cacheDir, `linuxdeploy-${appImageArch}.AppImage`),
    path.join(cacheDir, `appimagetool-${appImageArch}.AppImage`),
  ];

  for (const candidatePath of candidatePaths) {
    if (fs.existsSync(candidatePath)) {
      return candidatePath;
    }
  }

  return null;
}

function resolveAppImageOffset(appImagePath) {
  const result = spawnSync(
    "/usr/bin/bash",
    [
      "-lc",
      'APPIMAGE_EXTRACT_AND_RUN="${APPIMAGE_EXTRACT_AND_RUN:-1}" "$1" --appimage-offset',
      "_",
      appImagePath,
    ],
    {
      encoding: "utf8",
      env: {
        ...process.env,
        APPIMAGE_EXTRACT_AND_RUN: process.env.APPIMAGE_EXTRACT_AND_RUN ?? "1",
      },
    },
  );

  if (result.error && (result.status ?? 1) !== 0) {
    throw result.error;
  }

  if ((result.status ?? 1) !== 0) {
    throw new Error(
      `Failed to resolve AppImage runtime offset from ${appImagePath}: ${result.stderr ?? ""}`,
    );
  }

  const rawOffset = result.stdout.trim();
  const offset = Number.parseInt(rawOffset, 10);

  if (!Number.isFinite(offset) || offset <= 0) {
    throw new Error(
      `Failed to resolve AppImage runtime offset from ${appImagePath}: ${rawOffset}`,
    );
  }

  return offset;
}

function copyLeadingBytes(sourcePath, destinationPath, byteCount) {
  const buffer = Buffer.allocUnsafe(64 * 1024);
  const sourceFd = fs.openSync(sourcePath, "r");
  const destinationFd = fs.openSync(destinationPath, "w", 0o755);

  try {
    let bytesRemaining = byteCount;
    let readOffset = 0;
    while (bytesRemaining > 0) {
      const bytesToRead = Math.min(buffer.length, bytesRemaining);
      const bytesRead = fs.readSync(
        sourceFd,
        buffer,
        0,
        bytesToRead,
        readOffset,
      );

      if (bytesRead === 0) {
        throw new Error(
          `Unexpected end of file while extracting AppImage runtime from ${sourcePath}`,
        );
      }

      fs.writeSync(destinationFd, buffer, 0, bytesRead);
      bytesRemaining -= bytesRead;
      readOffset += bytesRead;
    }
  } finally {
    fs.closeSync(sourceFd);
    fs.closeSync(destinationFd);
  }
}

export function prepareLinuxAppImageRuntime() {
  if (process.platform !== "linux") {
    return null;
  }

  const sourcePath = resolveCachedToolAppImage();
  if (!sourcePath) {
    return null;
  }

  const runtimeOffset = resolveAppImageOffset(sourcePath);
  const runtimeDir = path.join(targetDir, "appimage-runtime");
  const runtimePath = path.join(runtimeDir, `runtime-${appImageArchitecture()}`);
  fs.mkdirSync(runtimeDir, { recursive: true });

  if (fs.existsSync(runtimePath)) {
    const existing = fs.statSync(runtimePath);
    const source = fs.statSync(sourcePath);
    if (existing.size === runtimeOffset && existing.mtimeMs >= source.mtimeMs) {
      return runtimePath;
    }
  }

  copyLeadingBytes(sourcePath, runtimePath, runtimeOffset);
  fs.chmodSync(runtimePath, 0o755);
  return runtimePath;
}
