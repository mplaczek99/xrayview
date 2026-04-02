use std::fmt;

use anyhow::Error as AnyhowError;
use serde::Serialize;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
#[serde(rename_all = "camelCase")]
pub enum BackendErrorCode {
    InvalidInput,
    NotFound,
    Cancelled,
    Conflict,
    CacheCorrupted,
    Internal,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct BackendError {
    pub code: BackendErrorCode,
    pub message: String,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub details: Vec<String>,
    pub recoverable: bool,
}

impl BackendError {
    pub fn new(code: BackendErrorCode, message: impl Into<String>) -> Self {
        Self {
            recoverable: matches!(
                code,
                BackendErrorCode::InvalidInput
                    | BackendErrorCode::NotFound
                    | BackendErrorCode::Cancelled
                    | BackendErrorCode::Conflict
                    | BackendErrorCode::CacheCorrupted
            ),
            code,
            message: message.into(),
            details: Vec::new(),
        }
    }

    pub fn invalid_input(message: impl Into<String>) -> Self {
        Self::new(BackendErrorCode::InvalidInput, message)
    }

    pub fn not_found(message: impl Into<String>) -> Self {
        Self::new(BackendErrorCode::NotFound, message)
    }

    pub fn cancelled(message: impl Into<String>) -> Self {
        Self::new(BackendErrorCode::Cancelled, message)
    }

    pub fn conflict(message: impl Into<String>) -> Self {
        Self::new(BackendErrorCode::Conflict, message)
    }

    pub fn cache_corrupted(message: impl Into<String>) -> Self {
        Self::new(BackendErrorCode::CacheCorrupted, message)
    }

    pub fn internal(message: impl Into<String>) -> Self {
        Self::new(BackendErrorCode::Internal, message)
    }

    pub fn with_details(mut self, details: Vec<String>) -> Self {
        self.details = details;
        self
    }
}

impl fmt::Display for BackendError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.message)
    }
}

impl std::error::Error for BackendError {}

impl From<AnyhowError> for BackendError {
    fn from(error: AnyhowError) -> Self {
        let details = error
            .chain()
            .skip(1)
            .map(ToString::to_string)
            .collect::<Vec<_>>();
        let message = error.to_string();
        let lower = message.to_ascii_lowercase();
        let code = if lower.contains("cancelled") {
            BackendErrorCode::Cancelled
        } else if lower.contains("not found") || lower.contains("does not exist") {
            BackendErrorCode::NotFound
        } else if lower.contains("must be")
            || lower.contains("invalid")
            || lower.contains("unsupported")
            || lower.contains("usage:")
            || lower.contains("choose only one")
        {
            BackendErrorCode::InvalidInput
        } else if lower.contains("cache") && (lower.contains("corrupt") || lower.contains("invalid"))
        {
            BackendErrorCode::CacheCorrupted
        } else {
            BackendErrorCode::Internal
        };

        BackendError::new(code, message).with_details(details)
    }
}

pub type BackendResult<T> = Result<T, BackendError>;
