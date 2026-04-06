import type { BackendError } from "./generated/contracts";

export function normalizeBackendError(error: unknown): BackendError {
  if (error && typeof error === "object") {
    const candidate = error as Partial<BackendError>;
    if (
      typeof candidate.message === "string" &&
      typeof candidate.code === "string"
    ) {
      return {
        code: candidate.code,
        message: candidate.message,
        details: Array.isArray(candidate.details)
          ? candidate.details.filter((entry): entry is string => typeof entry === "string")
          : [],
        recoverable: Boolean(candidate.recoverable),
      };
    }
  }

  if (error instanceof Error && error.message.trim()) {
    return {
      code: "internal",
      message: error.message,
      details: [],
      recoverable: false,
    };
  }

  if (typeof error === "string" && error.trim()) {
    return {
      code: "internal",
      message: error,
      details: [],
      recoverable: false,
    };
  }

  return {
    code: "internal",
    message: "Unexpected backend error",
    details: [],
    recoverable: false,
  };
}

export function formatBackendError(
  error: BackendError | unknown,
  fallback = "Unexpected backend error.",
): string {
  const normalized = normalizeBackendError(error);
  return normalized.message.trim() || fallback;
}
