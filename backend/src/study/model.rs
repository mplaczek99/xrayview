use std::path::PathBuf;

use crate::api::{MeasurementScale, StudyRecord};

pub fn create_study_record(
    study_id: String,
    input_path: PathBuf,
    measurement_scale: Option<MeasurementScale>,
) -> StudyRecord {
    let input_name = input_path
        .file_name()
        .and_then(|name| name.to_str())
        .filter(|name| !name.is_empty())
        .map(str::to_owned)
        .unwrap_or_else(|| input_path.display().to_string());

    StudyRecord {
        study_id,
        input_path,
        input_name,
        measurement_scale,
    }
}

#[cfg(test)]
mod tests {
    use std::path::PathBuf;

    use super::create_study_record;

    #[test]
    fn create_study_record_uses_file_name_when_available() {
        let study = create_study_record(
            String::from("study-1"),
            PathBuf::from("/tmp/sample-study.dcm"),
            None,
        );

        assert_eq!(study.input_name, "sample-study.dcm");
    }

    #[test]
    fn create_study_record_falls_back_to_path_display() {
        let study = create_study_record(String::from("study-2"), PathBuf::from(""), None);

        assert_eq!(study.input_name, "");
    }
}
