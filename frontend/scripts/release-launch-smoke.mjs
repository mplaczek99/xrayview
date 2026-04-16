import { spawn, spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { applyFrontendRuntimeEnv } from "./runtime-env.mjs";

const DEFAULT_DESKTOP_RUNTIME = "desktop";
const DEFAULT_BACKEND_BASE_URL = "http://127.0.0.1:38181";
const EXPECTED_SERVICE_NAME = "xrayview-backend";
const EXPECTED_TRANSPORT_KIND = "local-http-json";
const LAUNCH_TIMEOUT_MS = 20_000;
const SHUTDOWN_TIMEOUT_MS = 8_000;
const PROBE_TIMEOUT_MS = 400;
const PROBE_INTERVAL_MS = 200;
const SUPPORTED_BACKEND_RUNTIMES = new Set([
  "mock",
  "desktop",
]);
const DESKTOP_RUNTIMES_REQUIRING_SIDECAR = new Set(["desktop"]);

function pickEnvValue(env, plainKeys, viteKeys) {
  const keys = [
    ...(Array.isArray(plainKeys) ? plainKeys : [plainKeys]),
    ...(Array.isArray(viteKeys) ? viteKeys : [viteKeys]),
  ];

  for (const key of keys) {
    const value = env[key];
    if (typeof value === "string" && value.trim() !== "") {
      return value.trim();
    }
  }

  return "";
}

function isLoopbackHostname(hostname) {
  return (
    hostname === "localhost" ||
    hostname === "127.0.0.1" ||
    hostname === "[::1]" ||
    hostname === "::1"
  );
}

function hasDisplay(env) {
  return Boolean(env.DISPLAY || env.WAYLAND_DISPLAY);
}

function commandExists(command) {
  const probe = spawnSync(process.platform === "win32" ? "where" : "which", [command], {
    stdio: "ignore",
    shell: process.platform === "win32",
  });
  return (probe.status ?? 1) === 0;
}

function normalizeSidecarBaseUrl(rawValue) {
  if (!rawValue) {
    return DEFAULT_BACKEND_BASE_URL;
  }

  const parsed = new URL(rawValue);
  if (parsed.protocol !== "http:") {
    throw new Error(
      "XRAYVIEW_BACKEND_URL must be an absolute loopback http URL " +
        `(unsupported protocol: ${parsed.protocol})`,
    );
  }

  if (!isLoopbackHostname(parsed.hostname)) {
    throw new Error(
      "XRAYVIEW_BACKEND_URL must be an absolute loopback http URL " +
        `(host must be localhost, 127.0.0.1, or [::1]: ${parsed.hostname})`,
    );
  }

  if (
    (parsed.pathname && parsed.pathname !== "/") ||
    parsed.search ||
    parsed.hash ||
    parsed.username ||
    parsed.password
  ) {
    throw new Error(
      "XRAYVIEW_BACKEND_URL must be an absolute loopback http URL " +
        "(URL must not include a path, query, hash, or credentials)",
    );
  }

  return rawValue.replace(/\/+$/, "");
}

export function resolveDesktopRuntimeConfig(env = process.env) {
  const normalizedEnv = applyFrontendRuntimeEnv(env);
  const rawMode = pickEnvValue(
    normalizedEnv,
    "XRAYVIEW_BACKEND_RUNTIME",
    "VITE_XRAYVIEW_BACKEND_RUNTIME",
  );
  const mode = rawMode === "" ? DEFAULT_DESKTOP_RUNTIME : rawMode.toLowerCase();
  if (!SUPPORTED_BACKEND_RUNTIMES.has(mode)) {
    throw new Error(
      "XRAYVIEW_BACKEND_RUNTIME must be one of mock or desktop.",
    );
  }

  const rawUrl = pickEnvValue(
    normalizedEnv,
    ["XRAYVIEW_BACKEND_URL", "XRAYVIEW_GO_BACKEND_URL"],
    ["VITE_XRAYVIEW_BACKEND_URL", "VITE_XRAYVIEW_GO_BACKEND_URL"],
  );

  return {
    mode,
    backendBaseUrl: normalizeSidecarBaseUrl(rawUrl),
  };
}

export function runtimeRequiresBackendSidecar(mode) {
  return DESKTOP_RUNTIMES_REQUIRING_SIDECAR.has(mode);
}

function sleep(delayMs) {
  return new Promise((resolve) => {
    setTimeout(resolve, delayMs);
  });
}

async function probeSidecar(baseUrl) {
  try {
    const response = await fetch(`${baseUrl}/healthz`, {
      signal: AbortSignal.timeout(PROBE_TIMEOUT_MS),
    });
    if (!response.ok) {
      return null;
    }

    return await response.json();
  } catch {
    return null;
  }
}

function childLogsSummary(logs) {
  const sections = [];
  if (logs.error !== null) {
    sections.push(`spawn error:\n${logs.error.message}`);
  }
  if (logs.stdout.trim() !== "") {
    sections.push(`stdout:\n${logs.stdout.trim()}`);
  }
  if (logs.stderr.trim() !== "") {
    sections.push(`stderr:\n${logs.stderr.trim()}`);
  }

  return sections.length === 0 ? "no child output captured" : sections.join("\n\n");
}

function isLinuxDisplayBootstrapFailure(logs) {
  const combinedOutput = `${logs.stdout}\n${logs.stderr}`;
  return (
    combinedOutput.includes("Failed to initialize GTK") ||
    combinedOutput.includes("Failed to initialize gtk backend") ||
    combinedOutput.includes("failed to init GTK")
  );
}

function startLaunchProcess(executablePath, env) {
  const launchEnv = { ...env };
  const launchArgs = [];
  let command = executablePath;

  if (process.platform === "linux") {
    if (executablePath.endsWith(".AppImage")) {
      launchEnv.APPIMAGE_EXTRACT_AND_RUN ??= "1";
    }

    if (!hasDisplay(launchEnv) && commandExists("xvfb-run")) {
      command = "xvfb-run";
      launchArgs.push("-a", executablePath);
    }
  }

  const child = spawn(command, launchArgs, {
    cwd: path.dirname(executablePath),
    detached: process.platform !== "win32",
    env: launchEnv,
    shell: false,
    stdio: ["ignore", "pipe", "pipe"],
    windowsHide: true,
  });

  const logs = {
    error: null,
    stdout: "",
    stderr: "",
  };
  child.once("error", (error) => {
    logs.error = error;
  });
  child.stdout?.setEncoding("utf8");
  child.stdout?.on("data", (chunk) => {
    logs.stdout += chunk;
  });
  child.stderr?.setEncoding("utf8");
  child.stderr?.on("data", (chunk) => {
    logs.stderr += chunk;
  });

  return { child, logs };
}

function waitForExit(child, timeoutMs) {
  return new Promise((resolve) => {
    if (child.exitCode !== null || child.signalCode !== null) {
      resolve({
        code: child.exitCode,
        signal: child.signalCode,
        timedOut: false,
      });
      return;
    }

    let timeoutId = null;
    const onExit = (code, signal) => {
      if (timeoutId !== null) {
        clearTimeout(timeoutId);
      }
      resolve({ code, signal, timedOut: false });
    };

    child.once("exit", onExit);
    timeoutId = setTimeout(() => {
      child.removeListener("exit", onExit);
      resolve({
        code: child.exitCode,
        signal: child.signalCode,
        timedOut: true,
      });
    }, timeoutMs);
  });
}

async function waitForExpectedSidecar(baseUrl, child, logs, label) {
  const deadline = Date.now() + LAUNCH_TIMEOUT_MS;
  while (Date.now() < deadline) {
    if (child.exitCode !== null || child.signalCode !== null) {
      throw new Error(
        `${label} exited before the backend sidecar became ready.\n${childLogsSummary(logs)}`,
      );
    }

    if (logs.error !== null) {
      throw new Error(
        `${label} failed to launch.\n${childLogsSummary(logs)}`,
      );
    }

    const runtime = await probeSidecar(baseUrl);
    if (
      runtime?.status === "ok" &&
      runtime?.service === EXPECTED_SERVICE_NAME &&
      runtime?.transport === EXPECTED_TRANSPORT_KIND
    ) {
      return runtime;
    }

    await sleep(PROBE_INTERVAL_MS);
  }

  throw new Error(
    `${label} did not start the backend sidecar at ${baseUrl} within ${LAUNCH_TIMEOUT_MS}ms.\n` +
      childLogsSummary(logs),
  );
}

async function waitForSidecarShutdown(baseUrl, label) {
  const deadline = Date.now() + SHUTDOWN_TIMEOUT_MS;
  while (Date.now() < deadline) {
    if ((await probeSidecar(baseUrl)) === null) {
      return;
    }

    await sleep(PROBE_INTERVAL_MS);
  }

  throw new Error(`${label} left a backend sidecar running at ${baseUrl} after shutdown.`);
}

async function terminateProcessTree(child) {
  if (typeof child.pid !== "number") {
    return;
  }

  if (child.exitCode !== null || child.signalCode !== null) {
    return;
  }

  if (process.platform === "win32") {
    spawnSync("taskkill", ["/PID", String(child.pid), "/T", "/F"], {
      shell: true,
      stdio: "ignore",
      windowsHide: true,
    });
    await waitForExit(child, SHUTDOWN_TIMEOUT_MS);
    return;
  }

  try {
    process.kill(-child.pid, "SIGTERM");
  } catch {}

  const gracefulExit = await waitForExit(child, 3_000);
  if (!gracefulExit.timedOut) {
    return;
  }

  try {
    process.kill(-child.pid, "SIGKILL");
  } catch {}

  await waitForExit(child, 2_000);
}

export async function validateDesktopLaunch({
  executablePath,
  label,
  runtimeConfig = resolveDesktopRuntimeConfig(process.env),
}) {
  if (!fs.existsSync(executablePath)) {
    throw new Error(`Cannot launch ${label}; missing executable at ${executablePath}`);
  }

  if (process.platform !== "win32") {
    fs.chmodSync(executablePath, 0o755);
  }

  const xvfbAvailable = commandExists("xvfb-run");
  if (process.platform === "linux" && !hasDisplay(process.env) && !xvfbAvailable) {
    return {
      skipped: true,
      reason:
        "Linux launch smoke requires DISPLAY, WAYLAND_DISPLAY, or xvfb-run in the current environment.",
    };
  }

  const expectSidecar = runtimeRequiresBackendSidecar(runtimeConfig.mode);
  if (expectSidecar) {
    const occupied = await probeSidecar(runtimeConfig.backendBaseUrl);
    if (occupied !== null) {
      throw new Error(
        `${label} launch smoke requires ${runtimeConfig.backendBaseUrl} to be free before startup.`,
      );
    }
  }

  // The desktop shell defaults to an in-process embedded backend with no TCP
  // socket. Force the sidecar path by exporting XRAYVIEW_BACKEND_URL when the
  // smoke test expects to poll a real loopback listener.
  const launchBaseEnv = expectSidecar
    ? { ...process.env, XRAYVIEW_BACKEND_URL: runtimeConfig.backendBaseUrl }
    : process.env;
  const { child, logs } = startLaunchProcess(
    executablePath,
    applyFrontendRuntimeEnv(launchBaseEnv),
  );
  try {
    try {
      if (expectSidecar) {
        await waitForExpectedSidecar(
          runtimeConfig.backendBaseUrl,
          child,
          logs,
          label,
        );
      } else {
        await sleep(1_000);
        if (child.exitCode !== null || child.signalCode !== null || logs.error !== null) {
          throw new Error(
            `${label} exited during launch smoke.\n${childLogsSummary(logs)}`,
          );
        }
      }
    } catch (error) {
      if (process.platform === "linux" && !xvfbAvailable && isLinuxDisplayBootstrapFailure(logs)) {
        return {
          skipped: true,
          reason:
            "Linux launch smoke could not initialize GTK in the current shell; rerun with xvfb-run or a working desktop display.",
        };
      }

      throw error;
    }
  } finally {
    await terminateProcessTree(child);
  }

  if (expectSidecar) {
    await waitForSidecarShutdown(runtimeConfig.backendBaseUrl, label);
  }

  return {
    skipped: false,
  };
}
