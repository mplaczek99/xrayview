import type { RuntimeMode } from "./types";

const DEFAULT_GO_SIDECAR_BASE_URL = "http://127.0.0.1:38181";
const BACKEND_RUNTIME_ENV_KEY = "VITE_XRAYVIEW_BACKEND_RUNTIME";
const GO_SIDECAR_URL_ENV_KEY = "VITE_XRAYVIEW_GO_BACKEND_URL";

export interface RuntimeConfiguration {
  mode: RuntimeMode;
  selectionSource: "default" | "env";
  goSidecarBaseUrl: string;
  warnings: string[];
}

function isRuntimeMode(value: string): value is RuntimeMode {
  return (
    value === "mock" ||
    value === "legacy-rust" ||
    value === "go-sidecar"
  );
}

function normalizeUrl(value: string): string {
  return value.replace(/\/+$/, "");
}

function isLoopbackHostname(hostname: string): boolean {
  return (
    hostname === "localhost" ||
    hostname === "127.0.0.1" ||
    hostname === "[::1]" ||
    hostname === "::1"
  );
}

function resolveGoSidecarBaseUrl(rawValue: string | undefined): {
  baseUrl: string;
  warning: string | null;
} {
  if (!rawValue?.trim()) {
    return {
      baseUrl: DEFAULT_GO_SIDECAR_BASE_URL,
      warning: null,
    };
  }

  try {
    const parsed = new URL(rawValue.trim());
    if (parsed.protocol !== "http:") {
      throw new Error(`unsupported protocol: ${parsed.protocol}`);
    }

    if (!isLoopbackHostname(parsed.hostname)) {
      throw new Error(`host must be localhost, 127.0.0.1, or [::1]: ${parsed.hostname}`);
    }

    if (
      (parsed.pathname && parsed.pathname !== "/") ||
      parsed.search ||
      parsed.hash ||
      parsed.username ||
      parsed.password
    ) {
      throw new Error("URL must not include a path, query, hash, or credentials");
    }

    return {
      baseUrl: normalizeUrl(parsed.toString()),
      warning: null,
    };
  } catch (error) {
    const reason =
      error instanceof Error ? error.message : "invalid URL value";
    return {
      baseUrl: DEFAULT_GO_SIDECAR_BASE_URL,
      warning:
        `${GO_SIDECAR_URL_ENV_KEY} must be an absolute loopback http URL ` +
        `(for example ${DEFAULT_GO_SIDECAR_BASE_URL}). ` +
        `Falling back to ${DEFAULT_GO_SIDECAR_BASE_URL}. (${reason})`,
    };
  }
}

export function resolveRuntimeConfiguration(
  isTauriRuntime: boolean,
): RuntimeConfiguration {
  const warnings: string[] = [];
  const defaultMode: RuntimeMode = isTauriRuntime ? "legacy-rust" : "mock";
  const rawMode =
    typeof import.meta.env[BACKEND_RUNTIME_ENV_KEY] === "string"
      ? import.meta.env[BACKEND_RUNTIME_ENV_KEY]
      : undefined;
  let mode: RuntimeMode = defaultMode;
  let selectionSource: RuntimeConfiguration["selectionSource"] = "default";

  if (rawMode?.trim()) {
    selectionSource = "env";
    const normalizedMode = rawMode.trim().toLowerCase();
    if (!isRuntimeMode(normalizedMode)) {
      warnings.push(
        `${BACKEND_RUNTIME_ENV_KEY} must be one of mock, legacy-rust, or go-sidecar. ` +
          `Falling back to ${defaultMode}.`,
      );
    } else if (!isTauriRuntime && normalizedMode !== "mock") {
      warnings.push(
        `${normalizedMode} requires the Tauri shell. Falling back to mock in browser mode.`,
      );
      mode = "mock";
    } else {
      mode = normalizedMode;
    }
  }

  const goSidecarUrl =
    typeof import.meta.env[GO_SIDECAR_URL_ENV_KEY] === "string"
      ? import.meta.env[GO_SIDECAR_URL_ENV_KEY]
      : undefined;
  const { baseUrl: goSidecarBaseUrl, warning } =
    resolveGoSidecarBaseUrl(goSidecarUrl);

  if (warning) {
    warnings.push(warning);
  }

  return {
    mode,
    selectionSource,
    goSidecarBaseUrl,
    warnings,
  };
}
