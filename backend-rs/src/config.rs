use std::{env, path::PathBuf};

use crate::contracts::SERVICE_NAME;

// Env var keys. These are pub so the desktop shell and tests can reference
// them without hardcoding strings (and the CLAUDE.md docs match them).

// Accepts: debug | info | warn | error. Not "trace" — we don't ship trace logs
// because they're noisy enough to obscure real issues.
pub const LOG_LEVEL_ENV_KEY: &str = "XRAYVIEW_BACKEND_LOG_LEVEL";
// Root that cache_dir and persistence_dir hang off of by default. Setting this
// alone reroutes both subdirs together — the common case.
pub const BASE_DIR_ENV_KEY: &str = "XRAYVIEW_BACKEND_BASE_DIR";
// Explicit override of just the cache dir. Useful when /tmp is small but you
// want persistence on the real disk.
pub const CACHE_DIR_ENV_KEY: &str = "XRAYVIEW_BACKEND_CACHE_DIR";
// Explicit override of just the persistence (state) dir. Symmetric with above.
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
        // tempdir is intentionally ephemeral — defaults are for ad-hoc CLI
        // runs and tests. The desktop shell always overrides BASE_DIR.
        let base_dir = env::temp_dir().join("xrayview");

        Self {
            service_name: SERVICE_NAME.to_string(),
            logging: LoggingConfig {
                // "info" is a deliberate middle ground: verbose enough that a
                // user submitting a bug report has useful context, quiet enough
                // not to spam during normal operation.
                level: "info".to_string(),
            },
            paths: PathsConfig {
                cache_dir: base_dir.join("cache"),
                persistence_dir: base_dir.join("state"),
                // Move base_dir into the struct last so the joins above can
                // borrow it. Field-init order matters here.
                base_dir,
            },
        }
    }
}

impl Config {
    // Production entry point — reads real env vars.
    pub fn load() -> Result<Self, String> {
        Self::load_from_lookup(|key| env::var(key).ok())
    }

    // Same logic but with the env-var source pluggable. Tests use this to
    // hand in a HashMap; production uses the closure above. Keeping this
    // generic over `F` means we never accidentally read real env in tests.
    pub fn load_from_lookup<F>(lookup: F) -> Result<Self, String>
    where
        F: Fn(&str) -> Option<String>,
    {
        let mut config = Self::default();
        let lookup_non_empty = |key| lookup(key).filter(|value| !value.is_empty());

        // Empty strings are treated as "unset", which is the sane Unix-y
        // behavior (FOO= shouldn't break the app).
        if let Some(value) = lookup_non_empty(LOG_LEVEL_ENV_KEY) {
            let lower = value.to_ascii_lowercase();
            // Validate the level *before* storing it — otherwise we'd silently
            // log nothing if a typo'd level slipped through.
            if !matches!(lower.as_str(), "debug" | "info" | "warn" | "error") {
                return Err(format!(
                    "{LOG_LEVEL_ENV_KEY} must be a valid log level: {value}"
                ));
            }
            config.logging.level = lower;
        }

        // Precedence: BASE_DIR resets cache/persistence to its subdirs first,
        // then specific overrides win. So `BASE_DIR=/foo CACHE_DIR=/bar` lands
        // base=/foo, cache=/bar, persistence=/foo/state. Order matters here.
        if let Some(value) = lookup_non_empty(BASE_DIR_ENV_KEY) {
            config.paths.base_dir = PathBuf::from(value);
            config.paths.cache_dir = config.paths.base_dir.join("cache");
            config.paths.persistence_dir = config.paths.base_dir.join("state");
        }

        // Cache-specific override applies *after* BASE_DIR's defaults so the
        // user can pin cache somewhere different from persistence.
        if let Some(value) = lookup_non_empty(CACHE_DIR_ENV_KEY) {
            config.paths.cache_dir = PathBuf::from(value);
        }

        if let Some(value) = lookup_non_empty(PERSISTENCE_DIR_ENV_KEY) {
            config.paths.persistence_dir = PathBuf::from(value);
        }

        Ok(config)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::{collections::HashMap, path::Path};

    // Helper that turns a static map into the lookup closure that
    // load_from_lookup expects. Keeps the tests easy to read.
    fn lookup_from_map(
        values: HashMap<&'static str, &'static str>,
    ) -> impl Fn(&str) -> Option<String> {
        move |key| values.get(key).map(|value| (*value).to_string())
    }

    // Sanity check that the defaults match what the rest of the codebase
    // assumes (especially other crates that build paths off base_dir).
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

    // Verifies the BASE_DIR-resets-children behavior — easy regression if
    // someone reorders the lookup chain in load_from_lookup.
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

    // Pin down the precedence story: explicit CACHE_DIR / PERSISTENCE_DIR
    // must beat BASE_DIR-derived defaults. If this breaks, the docs in
    // CLAUDE.md become a lie.
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

    // "trace" is a valid log level for the `log` crate but we don't accept
    // it — this test guards that policy.
    #[test]
    fn load_from_lookup_rejects_invalid_log_level() {
        let error =
            Config::load_from_lookup(lookup_from_map(HashMap::from([(LOG_LEVEL_ENV_KEY, "trace")])))
                .unwrap_err();

        // Error must mention the env key so users know what to fix.
        assert!(error.contains(LOG_LEVEL_ENV_KEY));
    }
}
