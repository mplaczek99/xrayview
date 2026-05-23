use std::{env, path::PathBuf};

use crate::contracts::SERVICE_NAME;

pub const LOG_LEVEL_ENV_KEY: &str = "XRAYVIEW_BACKEND_LOG_LEVEL";
pub const BASE_DIR_ENV_KEY: &str = "XRAYVIEW_BACKEND_BASE_DIR";
pub const CACHE_DIR_ENV_KEY: &str = "XRAYVIEW_BACKEND_CACHE_DIR";
pub const PERSISTENCE_DIR_ENV_KEY: &str = "XRAYVIEW_BACKEND_PERSISTENCE_DIR";

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Config {
    pub service_name: String,
    pub logging: LoggingConfig,
    pub paths: PathsConfig,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct LoggingConfig {
    pub level: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PathsConfig {
    pub base_dir: PathBuf,
    pub cache_dir: PathBuf,
    pub persistence_dir: PathBuf,
}

impl Default for Config {
    fn default() -> Self {
        let base_dir = env::temp_dir().join("xrayview");

        Self {
            service_name: SERVICE_NAME.to_string(),
            logging: LoggingConfig {
                level: "info".to_string(),
            },
            paths: PathsConfig {
                cache_dir: base_dir.join("cache"),
                persistence_dir: base_dir.join("state"),
                base_dir,
            },
        }
    }
}

impl Config {
    pub fn load() -> Result<Self, String> {
        Self::load_from_lookup(|key| env::var(key).ok())
    }

    pub fn load_from_lookup<F>(lookup: F) -> Result<Self, String>
    where
        F: Fn(&str) -> Option<String>,
    {
        let mut config = Self::default();

        if let Some(value) = lookup(LOG_LEVEL_ENV_KEY).filter(|value| !value.is_empty()) {
            let lower = value.to_ascii_lowercase();
            if !matches!(lower.as_str(), "debug" | "info" | "warn" | "error") {
                return Err(format!(
                    "{LOG_LEVEL_ENV_KEY} must be a valid log level: {value}"
                ));
            }
            config.logging.level = lower;
        }

        if let Some(value) = lookup(BASE_DIR_ENV_KEY).filter(|value| !value.is_empty()) {
            config.paths.base_dir = PathBuf::from(value);
            config.paths.cache_dir = config.paths.base_dir.join("cache");
            config.paths.persistence_dir = config.paths.base_dir.join("state");
        }

        if let Some(value) = lookup(CACHE_DIR_ENV_KEY).filter(|value| !value.is_empty()) {
            config.paths.cache_dir = PathBuf::from(value);
        }

        if let Some(value) = lookup(PERSISTENCE_DIR_ENV_KEY).filter(|value| !value.is_empty()) {
            config.paths.persistence_dir = PathBuf::from(value);
        }

        Ok(config)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::{collections::HashMap, path::Path};

    fn lookup_from_map(
        values: HashMap<&'static str, &'static str>,
    ) -> impl Fn(&str) -> Option<String> {
        move |key| values.get(key).map(|value| (*value).to_string())
    }

    #[test]
    fn default_config_uses_temp_base_dir() {
        let config = Config::default();

        assert_eq!(config.logging.level, "info");
        assert_eq!(config.paths.base_dir, env::temp_dir().join("xrayview"));
        assert_eq!(config.paths.cache_dir, config.paths.base_dir.join("cache"));
        assert_eq!(
            config.paths.persistence_dir,
            config.paths.base_dir.join("state")
        );
    }

    #[test]
    fn load_from_lookup_applies_overrides() {
        let config = Config::load_from_lookup(lookup_from_map(HashMap::from([
            (LOG_LEVEL_ENV_KEY, "debug"),
            (BASE_DIR_ENV_KEY, "/tmp/xrayview-backend"),
        ])))
        .unwrap();

        assert_eq!(config.logging.level, "debug");
        assert_eq!(config.paths.base_dir, Path::new("/tmp/xrayview-backend"));
        assert_eq!(
            config.paths.cache_dir,
            Path::new("/tmp/xrayview-backend/cache")
        );
        assert_eq!(
            config.paths.persistence_dir,
            Path::new("/tmp/xrayview-backend/state")
        );
    }

    #[test]
    fn load_from_lookup_allows_explicit_cache_and_persistence_overrides() {
        let config = Config::load_from_lookup(lookup_from_map(HashMap::from([
            (BASE_DIR_ENV_KEY, "/tmp/xrayview-backend"),
            (CACHE_DIR_ENV_KEY, "/var/tmp/xrayview-cache"),
            (PERSISTENCE_DIR_ENV_KEY, "/var/tmp/xrayview-state"),
        ])))
        .unwrap();

        assert_eq!(config.paths.base_dir, Path::new("/tmp/xrayview-backend"));
        assert_eq!(config.paths.cache_dir, Path::new("/var/tmp/xrayview-cache"));
        assert_eq!(
            config.paths.persistence_dir,
            Path::new("/var/tmp/xrayview-state")
        );
    }

    #[test]
    fn load_from_lookup_rejects_invalid_log_level() {
        let error =
            Config::load_from_lookup(lookup_from_map(HashMap::from([(LOG_LEVEL_ENV_KEY, "trace")])))
                .unwrap_err();

        assert!(error.contains(LOG_LEVEL_ENV_KEY));
    }
}
