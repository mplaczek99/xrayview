import type { RuntimeMode } from "./types";

const BACKEND_RUNTIME_ENV_KEY = "VITE_XRAYVIEW_BACKEND_RUNTIME";
const BACKEND_URL_ENV_KEY = "VITE_XRAYVIEW_BACKEND_URL";
const DEFAULT_HTTP_BASE_URL = "http://127.0.0.1:38181";

export interface RuntimeConfiguration {
  mode: RuntimeMode;
  selectionSource: "default" | "env";
  warnings: string[];
  // httpBaseUrl is defined only when mode === "http". It points at the loopback
  // backend the agent harness spawned (or an explicit VITE_XRAYVIEW_BACKEND_URL
  // override). Other modes leave it undefined.
  httpBaseUrl?: string;
}

function isRuntimeMode(value: string): value is RuntimeMode {
  return value === "mock" || value === "desktop" || value === "http";
}

function normalizeRuntimeMode(value: string): RuntimeMode | null {
  if (isRuntimeMode(value)) {
    return value;
  }

  return null;
}

export function resolveRuntimeConfiguration(
  isDesktopRuntime: boolean,
): RuntimeConfiguration {
  const warnings: string[] = [];
  const defaultMode: RuntimeMode = isDesktopRuntime ? "desktop" : "mock";
  const rawMode =
    typeof import.meta.env[BACKEND_RUNTIME_ENV_KEY] === "string"
      ? import.meta.env[BACKEND_RUNTIME_ENV_KEY]
      : undefined;
  let mode: RuntimeMode = defaultMode;
  let selectionSource: RuntimeConfiguration["selectionSource"] = "default";

  if (rawMode?.trim()) {
    selectionSource = "env";
    const normalizedMode = rawMode.trim().toLowerCase();
    const modeOverride = normalizeRuntimeMode(normalizedMode);
    if (!modeOverride) {
      warnings.push(
        `${BACKEND_RUNTIME_ENV_KEY} must be one of mock, desktop, or http. Falling back to ${defaultMode}.`,
      );
    } else if (!isDesktopRuntime && modeOverride === "desktop") {
      // desktop mode relies on the Wails JS bridge that only exists inside
      // the packaged shell. In a plain browser (including the agent harness)
      // fall back to mock so the page still boots — the harness selects
      // "http" explicitly when a live backend is reachable.
      warnings.push(
        `${modeOverride} requires the desktop shell. Falling back to mock in browser mode.`,
      );
      mode = "mock";
    } else {
      mode = modeOverride;
    }
  }

  const config: RuntimeConfiguration = {
    mode,
    selectionSource,
    warnings,
  };

  if (mode === "http") {
    const rawBaseUrl = import.meta.env[BACKEND_URL_ENV_KEY];
    const trimmed =
      typeof rawBaseUrl === "string" ? rawBaseUrl.trim() : "";
    config.httpBaseUrl = trimmed || DEFAULT_HTTP_BASE_URL;
  }

  return config;
}
