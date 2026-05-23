import type { RuntimeMode } from "./types";

const BACKEND_RUNTIME_ENV_KEY = "VITE_XRAYVIEW_BACKEND_RUNTIME";

export interface RuntimeConfiguration {
  mode: RuntimeMode;
  selectionSource: "default" | "env";
  warnings: string[];
}

function isRuntimeMode(value: string): value is RuntimeMode {
  return value === "mock" || value === "desktop";
}

function normalizeRuntimeMode(value: string): RuntimeMode | null {
  return isRuntimeMode(value) ? value : null;
}

export function resolveRuntimeConfiguration(isDesktopRuntime: boolean): RuntimeConfiguration {
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
    const modeOverride = normalizeRuntimeMode(rawMode.trim().toLowerCase());
    if (!modeOverride) {
      warnings.push(
        `${BACKEND_RUNTIME_ENV_KEY} must be one of mock or desktop. Falling back to ${defaultMode}.`,
      );
    } else if (!isDesktopRuntime && modeOverride === "desktop") {
      warnings.push(
        `${modeOverride} requires the desktop shell. Falling back to mock in browser mode.`,
      );
      mode = "mock";
    } else {
      mode = modeOverride;
    }
  }

  return {
    mode,
    selectionSource,
    warnings,
  };
}
