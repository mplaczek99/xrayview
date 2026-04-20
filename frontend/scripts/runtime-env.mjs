const SUPPORTED_BACKEND_RUNTIMES = new Set([
  "mock",
  "desktop",
  "http",
]);

function isLoopbackHostname(hostname) {
  return (
    hostname === "localhost" ||
    hostname === "127.0.0.1" ||
    hostname === "[::1]" ||
    hostname === "::1"
  );
}

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
        "XRAYVIEW_BACKEND_RUNTIME must be one of mock, desktop, or http.",
      );
    }

    nextEnv.VITE_XRAYVIEW_BACKEND_RUNTIME = normalizedMode;
    nextEnv.XRAYVIEW_BACKEND_RUNTIME = normalizedMode;
  }

  const rawUrl = pickEnvValue(
    env,
    ["XRAYVIEW_BACKEND_URL", "XRAYVIEW_GO_BACKEND_URL"],
    ["VITE_XRAYVIEW_BACKEND_URL", "VITE_XRAYVIEW_GO_BACKEND_URL"],
  );

  if (rawUrl) {
    try {
      const parsed = new URL(rawUrl);
      if (parsed.protocol !== "http:") {
        throw new Error(`unsupported protocol: ${parsed.protocol}`);
      }

      if (!isLoopbackHostname(parsed.hostname)) {
        throw new Error(
          `host must be localhost, 127.0.0.1, or [::1]: ${parsed.hostname}`,
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
          "URL must not include a path, query, hash, or credentials",
        );
      }
    } catch (error) {
      const reason = error instanceof Error ? error.message : "invalid URL value";
      throw new Error(
        "XRAYVIEW_BACKEND_URL must be an absolute loopback http URL " +
          "(for example http://127.0.0.1:38181). " +
          `(${reason})`,
      );
    }

    const normalizedUrl = rawUrl.replace(/\/+$/, "");
    nextEnv.XRAYVIEW_BACKEND_URL = normalizedUrl;
    nextEnv.XRAYVIEW_GO_BACKEND_URL = normalizedUrl;
    nextEnv.VITE_XRAYVIEW_BACKEND_URL = normalizedUrl;
    nextEnv.VITE_XRAYVIEW_GO_BACKEND_URL = normalizedUrl;
  }

  return nextEnv;
}
