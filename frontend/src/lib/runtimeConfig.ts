import type { RuntimeMode } from "./types";

const BACKEND_RUNTIME_ENV_KEY = "VITE_XRAYVIEW_BACKEND_RUNTIME";

export interface RuntimeConfiguration {
  mode: RuntimeMode;
  selectionSource: "default" | "env";
  warnings: string[];
}

function isRuntimeMode(value: string): value is RuntimeMode {
  return value === "mock" || value === "go-sidecar";
}

export function resolveRuntimeConfiguration(
  isDesktopRuntime: boolean,
): RuntimeConfiguration {
  const warnings: string[] = [];
  const defaultMode: RuntimeMode = isDesktopRuntime ? "go-sidecar" : "mock";
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
        `${BACKEND_RUNTIME_ENV_KEY} must be one of mock or go-sidecar. Falling back to ${defaultMode}.`,
      );
    } else if (!isDesktopRuntime && normalizedMode !== "mock") {
      warnings.push(
        `${normalizedMode} requires the Wails desktop shell. Falling back to mock in browser mode.`,
      );
      mode = "mock";
    } else {
      mode = normalizedMode;
    }
  }

  return {
    mode,
    selectionSource,
    warnings,
  };
}
