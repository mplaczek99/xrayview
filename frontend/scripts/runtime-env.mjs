const SUPPORTED_BACKEND_RUNTIMES = new Set(["mock", "desktop"]);

function pickEnvValue(env, plainKey, viteKey) {
  for (const key of [plainKey, viteKey]) {
    const value = env[key];
    if (typeof value === "string" && value.trim() !== "") {
      return value.trim();
    }
  }

  return "";
}

export function applyFrontendRuntimeEnv(env = process.env) {
  const nextEnv = { ...env };
  const rawMode = pickEnvValue(
    env,
    "XRAYVIEW_BACKEND_RUNTIME",
    "VITE_XRAYVIEW_BACKEND_RUNTIME",
  );

  if (!rawMode) {
    return nextEnv;
  }

  const normalizedMode = rawMode.toLowerCase();
  if (!SUPPORTED_BACKEND_RUNTIMES.has(normalizedMode)) {
    throw new Error("XRAYVIEW_BACKEND_RUNTIME must be one of mock or desktop.");
  }

  nextEnv.VITE_XRAYVIEW_BACKEND_RUNTIME = normalizedMode;
  nextEnv.XRAYVIEW_BACKEND_RUNTIME = normalizedMode;
  return nextEnv;
}
