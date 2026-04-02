use std::collections::HashMap;
use std::path::PathBuf;

use uuid::Uuid;

use crate::api::{MeasurementScale, StudyRecord};

use super::model::create_study_record;

#[derive(Debug, Default)]
pub struct StudyRegistry {
    studies: HashMap<String, StudyRecord>,
}

impl StudyRegistry {
    pub fn open_study(
        &mut self,
        input_path: PathBuf,
        measurement_scale: Option<MeasurementScale>,
    ) -> StudyRecord {
        let study = create_study_record(Uuid::new_v4().to_string(), input_path, measurement_scale);
        self.studies.insert(study.study_id.clone(), study.clone());
        study
    }

    pub fn get(&self, study_id: &str) -> Option<&StudyRecord> {
        self.studies.get(study_id)
    }
}

#[cfg(test)]
mod tests {
    use std::path::PathBuf;

    use super::StudyRegistry;

    #[test]
    fn open_study_registers_metadata_and_lookup_by_id() {
        let mut registry = StudyRegistry::default();

        let study = registry.open_study(PathBuf::from("/tmp/example-study.dcm"), None);

        assert_eq!(
            registry
                .get(&study.study_id)
                .map(|entry| entry.input_name.as_str()),
            Some("example-study.dcm")
        );
    }

    #[test]
    fn open_study_assigns_unique_ids() {
        let mut registry = StudyRegistry::default();

        let first = registry.open_study(PathBuf::from("/tmp/one.dcm"), None);
        let second = registry.open_study(PathBuf::from("/tmp/two.dcm"), None);

        assert_ne!(first.study_id, second.study_id);
    }
}
