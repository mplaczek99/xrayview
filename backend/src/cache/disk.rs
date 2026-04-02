use std::env;
use std::fs;
use std::path::PathBuf;

use crate::error::{BackendError, BackendResult};

#[derive(Debug, Clone)]
pub struct DiskCache {
    root: PathBuf,
}

impl Default for DiskCache {
    fn default() -> Self {
        Self::new(env::temp_dir().join("xrayview"))
    }
}

impl DiskCache {
    pub fn new(root: PathBuf) -> Self {
        Self { root }
    }

    pub fn root(&self) -> &PathBuf {
        &self.root
    }

    pub fn artifact_path(
        &self,
        namespace: &str,
        key: &str,
        extension: &str,
    ) -> BackendResult<PathBuf> {
        let directory = self.root.join("cache").join("artifacts").join(namespace);
        fs::create_dir_all(&directory).map_err(|error| {
            BackendError::internal(format!(
                "failed to create cache directory {}: {error}",
                directory.display()
            ))
        })?;

        Ok(directory.join(format!("{key}.{extension}")))
    }

    pub fn persistence_path(&self, name: &str) -> BackendResult<PathBuf> {
        let directory = self.root.join("state");
        fs::create_dir_all(&directory).map_err(|error| {
            BackendError::internal(format!(
                "failed to create state directory {}: {error}",
                directory.display()
            ))
        })?;

        Ok(directory.join(name))
    }
}
