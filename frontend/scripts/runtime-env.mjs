const SUPPORTED_BACKEND_RUNTIMES = new Set([
  "mock",
  "legacy-rust",
  "go-sidecar",
]);

function pickEnvValue(env, plainKey, viteKey) {
  const value = env[plainKey] ?? env[viteKey];
  return typeof value === "string" ? value.trim() : "";
}

export function applyFrontendRuntimeEnv(env = process.env) {
  const nextEnv = { ...env };
  const rawMode = pickEnvValue(
    env,
    "XRAYVIEW_BACKEND_RUNTIME",
    "VITE_XRAYVIEW_BACKEND_RUNTIME",
  );

  if (rawMode) {
    const normalizedMode = rawMode.toLowerCase();
    if (!SUPPORTED_BACKEND_RUNTIMES.has(normalizedMode)) {
      throw new Error(
        "XRAYVIEW_BACKEND_RUNTIME must be one of mock, legacy-rust, or go-sidecar.",
      );
    }

    nextEnv.VITE_XRAYVIEW_BACKEND_RUNTIME = normalizedMode;
  }

  const rawUrl = pickEnvValue(
    env,
    "XRAYVIEW_GO_BACKEND_URL",
    "VITE_XRAYVIEW_GO_BACKEND_URL",
  );

  if (rawUrl) {
    try {
      new URL(rawUrl);
    } catch (error) {
      const reason = error instanceof Error ? error.message : "invalid URL value";
      throw new Error(`XRAYVIEW_GO_BACKEND_URL must be an absolute URL. (${reason})`);
    }

    nextEnv.VITE_XRAYVIEW_GO_BACKEND_URL = rawUrl.replace(/\/+$/, "");
  }

  return nextEnv;
}
