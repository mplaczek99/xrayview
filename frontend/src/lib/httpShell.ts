import type { ShellAPI } from "./runtimeTypes";

// Environment variables set by scripts/agent-harness.mjs. Agents expect
// deterministic fixtures; falling back to prompts or localStorage would
// stall automated flows, so every picker resolves strictly from env and
// fails with an actionable message when unset.
const FIXTURE_ENV_KEY = "VITE_XRAYVIEW_AGENT_FIXTURE";
const OUTPUT_DIR_ENV_KEY = "VITE_XRAYVIEW_AGENT_OUTPUT_DIR";

function readViteEnv(key: string): string | undefined {
  const value = import.meta.env[key];
  return typeof value === "string" && value.trim() ? value.trim() : undefined;
}

function requireEnv(key: string, purpose: string): string {
  const value = readViteEnv(key);
  if (!value) {
    throw new Error(
      `${key} is not set but is required to ${purpose}. ` +
        `Launch the frontend through scripts/agent-harness.mjs or export the variable explicitly.`,
    );
  }

  return value;
}

function joinPath(dir: string, name: string): string {
  const trimmedDir = dir.replace(/[\\/]+$/, "");
  const trimmedName = name.replace(/^[\\/]+/, "");
  const separator = trimmedDir.includes("\\") ? "\\" : "/";
  return `${trimmedDir}${separator}${trimmedName}`;
}

export function createHttpShellAPI(): ShellAPI {
  return {
    pickDicomFile: async () =>
      requireEnv(FIXTURE_ENV_KEY, "choose the DICOM fixture the agent should open"),
    pickSaveDicomPath: async (defaultName) => {
      const outputDir = requireEnv(
        OUTPUT_DIR_ENV_KEY,
        "choose where processed DICOM files should be written",
      );
      return joinPath(outputDir, defaultName || "study.dcm");
    },
  };
}
