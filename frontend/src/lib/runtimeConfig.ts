import type { RuntimeMode } from "./types";

const BACKEND_RUNTIME_ENV_KEY = "VITE_XRAYVIEW_BACKEND_RUNTIME";
const LEGACY_DESKTOP_RUNTIME_ALIAS = "go-sidecar";

export interface RuntimeConfiguration {
  mode: RuntimeMode;
  selectionSource: "default" | "env";
  warnings: string[];
}

function isRuntimeMode(value: string): value is RuntimeMode {
  return value === "mock" || value === "desktop";
}

function normalizeRuntimeMode(value: string): RuntimeMode | null {
  if (isRuntimeMode(value)) {
    return value;
  }

  if (value === LEGACY_DESKTOP_RUNTIME_ALIAS) {
    return "desktop";
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
        `${BACKEND_RUNTIME_ENV_KEY} must be one of mock or desktop. Falling back to ${defaultMode}.`,
      );
    } else if (!isDesktopRuntime && modeOverride !== "mock") {
      warnings.push(
        `${modeOverride} requires the desktop shell. Falling back to mock in browser mode.`,
      );
      mode = "mock";
    } else {
      if (normalizedMode === LEGACY_DESKTOP_RUNTIME_ALIAS) {
        warnings.push(
          `${BACKEND_RUNTIME_ENV_KEY}=${LEGACY_DESKTOP_RUNTIME_ALIAS} is deprecated for the frontend; use desktop instead.`,
        );
      }

      mode = modeOverride;
    }
  }

  return {
    mode,
    selectionSource,
    warnings,
  };
}
