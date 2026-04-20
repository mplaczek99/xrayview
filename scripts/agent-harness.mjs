#!/usr/bin/env node

// agent-harness spins up the Go backend and the Vite dev server together so
// autonomous agents (Claude + Codex, driving playwright-cli or the HTTP API
// directly) can exercise the full xrayview stack from a browser. It is the
// one-command entry point documented in AGENTS.md.
//
// Lifecycle:
//   1. Spawn `go -C backend run ./cmd/xrayviewd` on :38181.
//   2. Poll GET /healthz until 200 OK or the startup budget expires.
//   3. Spawn the Vite dev server with VITE_XRAYVIEW_BACKEND_RUNTIME=http so
//      the browser binds the loopback backend instead of the mock runtime.
//   4. Forward child stdout/stderr prefixed with [backend] / [frontend].
//   5. On SIGINT/SIGTERM, send SIGTERM to both children and wait briefly
//      before falling back to SIGKILL so the parent never hangs.
//
// The harness is deliberately chatty at startup — agents rely on the log
// lines to know when each port is ready. If you tighten the logging here,
// update AGENTS.md too.

import { spawn } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { setTimeout as delay } from "node:timers/promises";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(scriptDir, "..");

const BACKEND_HOST = "127.0.0.1";
const BACKEND_PORT = 38181;
const BACKEND_URL = `http://${BACKEND_HOST}:${BACKEND_PORT}`;
const BACKEND_HEALTH_URL = `${BACKEND_URL}/healthz`;
const FRONTEND_PORT = 5173;
const STARTUP_TIMEOUT_MS = 15_000;
const HEALTH_POLL_MS = 100;
const SHUTDOWN_GRACE_MS = 5_000;

const fixturePath =
  process.env.VITE_XRAYVIEW_AGENT_FIXTURE ??
  path.join(repoRoot, "agent-fixtures", "default.dcm");
const outputDir =
  process.env.VITE_XRAYVIEW_AGENT_OUTPUT_DIR ??
  path.join(repoRoot, "agent-fixtures", "out");

const harnessEnv = {
  ...process.env,
  XRAYVIEW_BACKEND_HOST: BACKEND_HOST,
  XRAYVIEW_BACKEND_PORT: String(BACKEND_PORT),
  VITE_XRAYVIEW_BACKEND_RUNTIME: "http",
  VITE_XRAYVIEW_BACKEND_URL: BACKEND_URL,
  VITE_XRAYVIEW_AGENT_FIXTURE: fixturePath,
  VITE_XRAYVIEW_AGENT_OUTPUT_DIR: outputDir,
};

// prefixStream forwards lines from a child process stream with a tag so the
// combined output is still readable when both servers log at once.
function prefixStream(stream, prefix) {
  let buffer = "";
  stream.setEncoding("utf8");
  stream.on("data", (chunk) => {
    buffer += chunk;
    let newlineIndex;
    while ((newlineIndex = buffer.indexOf("\n")) >= 0) {
      const line = buffer.slice(0, newlineIndex);
      buffer = buffer.slice(newlineIndex + 1);
      if (line.length > 0) {
        process.stdout.write(`${prefix} ${line}\n`);
      }
    }
  });
  stream.on("end", () => {
    if (buffer.length > 0) {
      process.stdout.write(`${prefix} ${buffer}\n`);
    }
  });
}

function spawnChild(label, command, args, options = {}) {
  const child = spawn(command, args, {
    cwd: repoRoot,
    env: harnessEnv,
    stdio: ["ignore", "pipe", "pipe"],
    ...options,
  });

  prefixStream(child.stdout, `[${label}]`);
  prefixStream(child.stderr, `[${label}]`);

  child.on("exit", (code, signal) => {
    const reason = signal ? `signal ${signal}` : `code ${code ?? 0}`;
    process.stdout.write(`[${label}] exited (${reason})\n`);
  });

  return child;
}

async function waitForBackendReady() {
  const deadline = Date.now() + STARTUP_TIMEOUT_MS;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(BACKEND_HEALTH_URL);
      if (response.ok) {
        return;
      }
    } catch {
      // backend not yet accepting connections; fall through to retry.
    }

    await delay(HEALTH_POLL_MS);
  }

  throw new Error(
    `backend did not report ready at ${BACKEND_HEALTH_URL} within ${STARTUP_TIMEOUT_MS}ms`,
  );
}

function killChild(child) {
  if (!child || child.exitCode !== null) {
    return Promise.resolve();
  }

  return new Promise((resolve) => {
    const timer = setTimeout(() => {
      if (child.exitCode === null) {
        child.kill("SIGKILL");
      }
      resolve();
    }, SHUTDOWN_GRACE_MS);

    child.once("exit", () => {
      clearTimeout(timer);
      resolve();
    });

    child.kill("SIGTERM");
  });
}

async function main() {
  process.stdout.write(`[harness] repo = ${repoRoot}\n`);
  process.stdout.write(`[harness] backend = ${BACKEND_URL}\n`);
  process.stdout.write(`[harness] frontend = http://127.0.0.1:${FRONTEND_PORT}\n`);
  process.stdout.write(`[harness] fixture = ${fixturePath}\n`);
  process.stdout.write(`[harness] output dir = ${outputDir}\n`);

  const backend = spawnChild("backend", "go", [
    "-C",
    "backend",
    "run",
    "./cmd/xrayviewd",
  ]);

  try {
    await waitForBackendReady();
  } catch (error) {
    process.stderr.write(`[harness] ${error.message}\n`);
    await killChild(backend);
    process.exit(1);
  }

  process.stdout.write("[harness] backend ready\n");

  const frontend = spawnChild("frontend", "npm", [
    "--prefix",
    "frontend",
    "run",
    "dev",
    "--",
    "--host",
    "127.0.0.1",
    "--port",
    String(FRONTEND_PORT),
    "--strictPort",
  ]);

  let shuttingDown = false;
  async function shutdown(signal) {
    if (shuttingDown) return;
    shuttingDown = true;
    process.stdout.write(`[harness] received ${signal}, shutting down\n`);
    await Promise.all([killChild(frontend), killChild(backend)]);
    process.exit(0);
  }

  process.on("SIGINT", () => void shutdown("SIGINT"));
  process.on("SIGTERM", () => void shutdown("SIGTERM"));

  // Exit together: if either child drops out unexpectedly, tear down the
  // partner so the harness isn't left with half a stack running.
  backend.once("exit", (code) => {
    if (shuttingDown) return;
    process.stderr.write(`[harness] backend exited with code ${code ?? 0}\n`);
    void shutdown("backend-exit");
  });

  frontend.once("exit", (code) => {
    if (shuttingDown) return;
    process.stderr.write(`[harness] frontend exited with code ${code ?? 0}\n`);
    void shutdown("frontend-exit");
  });
}

main().catch((error) => {
  process.stderr.write(`[harness] fatal: ${error.stack ?? error.message}\n`);
  process.exit(1);
});
