import type { BackendError } from "./generated/contracts";

function fromCandidate(candidate: Partial<BackendError> | null): BackendError | null {
  if (candidate && typeof candidate.message === "string" && typeof candidate.code === "string") {
    return {
      code: candidate.code,
      message: candidate.message,
      details: Array.isArray(candidate.details)
        ? candidate.details.filter((entry): entry is string => typeof entry === "string")
        : [],
      recoverable: Boolean(candidate.recoverable),
    };
  }
  return null;
}

function tryParseObject(value: string): Partial<BackendError> | null {
  try {
    const parsed: unknown = JSON.parse(value);
    return parsed && typeof parsed === "object" ? (parsed as Partial<BackendError>) : null;
  } catch {
    return null;
  }
}

export function normalizeBackendError(error: unknown): BackendError {
  if (error && typeof error === "object") {
    const direct = fromCandidate(error as Partial<BackendError>);
    if (direct) {
      return direct;
    }
    // Wails rejects with an Error whose message is the JSON-encoded BackendError
    // (see desktop/app.go bindErr); parse it back to recover code/recoverable.
    if (error instanceof Error && error.message.trim()) {
      const structured = fromCandidate(tryParseObject(error.message));
      return (
        structured ?? {
          code: "internal",
          message: error.message,
          details: [],
          recoverable: false,
        }
      );
    }
  }

  if (typeof error === "string" && error.trim()) {
    const structured = fromCandidate(tryParseObject(error));
    return (
      structured ?? {
        code: "internal",
        message: error,
        details: [],
        recoverable: false,
      }
    );
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
