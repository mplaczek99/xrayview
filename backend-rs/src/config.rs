use std::{
    env,
    net::{IpAddr, SocketAddr},
    path::PathBuf,
    time::Duration,
};

use crate::contracts::SERVICE_NAME;

pub const HOST_ENV_KEY: &str = "XRAYVIEW_BACKEND_HOST";
pub const PORT_ENV_KEY: &str = "XRAYVIEW_BACKEND_PORT";
pub const LOG_LEVEL_ENV_KEY: &str = "XRAYVIEW_BACKEND_LOG_LEVEL";
pub const BASE_DIR_ENV_KEY: &str = "XRAYVIEW_BACKEND_BASE_DIR";
pub const CACHE_DIR_ENV_KEY: &str = "XRAYVIEW_BACKEND_CACHE_DIR";
pub const PERSISTENCE_DIR_ENV_KEY: &str = "XRAYVIEW_BACKEND_PERSISTENCE_DIR";
pub const SHUTDOWN_TIMEOUT_ENV_KEY: &str = "XRAYVIEW_BACKEND_SHUTDOWN_TIMEOUT";

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Config {
    pub service_name: String,
    pub server: ServerConfig,
    pub logging: LoggingConfig,
    pub paths: PathsConfig,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ServerConfig {
    pub host: String,
    pub port: u16,
    pub shutdown_timeout: Duration,
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
            server: ServerConfig {
                host: "127.0.0.1".to_string(),
                port: 38181,
                shutdown_timeout: Duration::from_secs(5),
            },
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

        if let Some(value) = lookup(HOST_ENV_KEY).filter(|value| !value.is_empty()) {
            config.server.host = value;
        }

        if let Some(value) = lookup(PORT_ENV_KEY).filter(|value| !value.is_empty()) {
            let port = value
                .parse::<u16>()
                .map_err(|_| format!("{PORT_ENV_KEY} must be a valid TCP port: {value:?}"))?;
            if port == 0 {
                return Err(format!(
                    "{PORT_ENV_KEY} must be a valid TCP port: {value:?}"
                ));
            }
            config.server.port = port;
        }

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

        if let Some(value) = lookup(SHUTDOWN_TIMEOUT_ENV_KEY).filter(|value| !value.is_empty()) {
            config.server.shutdown_timeout = parse_duration(&value).ok_or_else(|| {
                format!("{SHUTDOWN_TIMEOUT_ENV_KEY} must be a positive duration: {value:?}")
            })?;
        }

        if !is_loopback_host(&config.server.host) {
            return Err(format!(
                "{HOST_ENV_KEY} must be a loopback host for the local sidecar transport: {:?}",
                config.server.host
            ));
        }

        Ok(config)
    }

    pub fn listen_address(&self) -> String {
        match self.server.host.parse::<IpAddr>() {
            Ok(IpAddr::V6(ip)) => SocketAddr::new(IpAddr::V6(ip), self.server.port).to_string(),
            _ => format!("{}:{}", self.server.host, self.server.port),
        }
    }
}

fn parse_duration(value: &str) -> Option<Duration> {
    let trimmed = value.trim();
    if trimmed.is_empty() {
        return None;
    }

    let (number, unit) = trimmed
        .find(|character: char| !character.is_ascii_digit())
        .map(|index| trimmed.split_at(index))
        .unwrap_or((trimmed, "s"));
    let amount = number.parse::<u64>().ok()?;
    if amount == 0 {
        return None;
    }

    match unit {
        "ms" => Some(Duration::from_millis(amount)),
        "s" => Some(Duration::from_secs(amount)),
        "m" => Some(Duration::from_secs(amount * 60)),
        _ => None,
    }
}

pub fn is_loopback_host(host: &str) -> bool {
    if host.eq_ignore_ascii_case("localhost") {
        return true;
    }

    host.trim_matches(['[', ']'])
        .parse::<IpAddr>()
        .is_ok_and(|ip| ip.is_loopback())
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
    fn default_config_matches_frontend_sidecar_defaults() {
        let config = Config::default();

        assert_eq!(config.server.host, "127.0.0.1");
        assert_eq!(config.server.port, 38181);
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
            (HOST_ENV_KEY, "::1"),
            (PORT_ENV_KEY, "39123"),
            (LOG_LEVEL_ENV_KEY, "debug"),
            (BASE_DIR_ENV_KEY, "/tmp/xrayview-backend"),
            (SHUTDOWN_TIMEOUT_ENV_KEY, "9s"),
        ])))
        .unwrap();

        assert_eq!(config.server.host, "::1");
        assert_eq!(config.server.port, 39123);
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
        assert_eq!(config.server.shutdown_timeout, Duration::from_secs(9));
        assert_eq!(config.listen_address(), "[::1]:39123");
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
    fn load_from_lookup_rejects_invalid_port() {
        let error =
            Config::load_from_lookup(lookup_from_map(HashMap::from([(PORT_ENV_KEY, "abc")])))
                .unwrap_err();

        assert!(error.contains(PORT_ENV_KEY));
    }

    #[test]
    fn load_from_lookup_rejects_non_loopback_host() {
        let error =
            Config::load_from_lookup(lookup_from_map(HashMap::from([(HOST_ENV_KEY, "0.0.0.0")])))
                .unwrap_err();

        assert!(error.contains(HOST_ENV_KEY));
    }
}
