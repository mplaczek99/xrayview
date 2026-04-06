use std::fs;
use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::thread;
use std::time::{Duration, Instant};

use chrono::DateTime;
use dicom_core::Tag;
use dicom_dictionary_std::tags;
use dicom_object::{DicomAttribute, DicomObject, OpenFileOptions};
use image::ImageFormat;
use serde::Serialize;
use serde_json::json;
use tempfile::TempDir;
use xrayview_backend::api::{
    AnalyzeStudyCommand, JobCommand, JobResult, JobSnapshot, JobState, ProcessStudyRequest,
    RenderPreviewRequest, RenderStudyCommand, StudyRecord,
};
use xrayview_backend::app::state::AppState;
use xrayview_backend::app::{describe_study, process_study, render_preview};
use xrayview_backend::persistence::StudyCatalogStore;

const WRITE_FIXTURES_ENV: &str = "XRAYVIEW_WRITE_PARITY_FIXTURES";
const SAMPLE_NAME: &str = "sample-dental-radiograph";
const SECONDARY_CAPTURE_SOP_CLASS_UID: &str = "1.2.840.10008.5.1.4.1.1.7";

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct ExportFixture {
    mode: String,
    rows: u16,
    columns: u16,
    samples_per_pixel: u16,
    photometric_interpretation: String,
    planar_configuration: Option<u16>,
    bits_allocated: u16,
    bits_stored: u16,
    high_bit: u16,
    pixel_representation: u16,
    window_center: Option<String>,
    window_width: Option<String>,
    sop_class_uid: String,
    image_type: Vec<String>,
    conversion_type: String,
    series_description: String,
    derivation_description: String,
    manufacturer: String,
    manufacturer_model_name: String,
    software_versions: String,
    study_instance_uid_preserved: bool,
    patient_name_preserved: Option<bool>,
    patient_id_preserved: Option<bool>,
    pixel_spacing_preserved: Option<bool>,
    imager_pixel_spacing_preserved: Option<bool>,
    nominal_scanned_pixel_spacing_preserved: Option<bool>,
    generated_sop_instance_uid_prefix: bool,
    generated_series_instance_uid_prefix: bool,
    instance_creation_date_is_yyyymmdd: bool,
    instance_creation_time_has_six_digits: bool,
    pixel_data_length: usize,
    pixel_data_fnv1a64: String,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct RecentStudyCatalogFixture {
    recent_studies: Vec<RecentStudyEntryFixture>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct RecentStudyEntryFixture {
    input_path: String,
    input_name: String,
    measurement_scale: Option<RecentMeasurementScaleFixture>,
    last_opened_at_is_rfc3339: bool,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct RecentMeasurementScaleFixture {
    row_spacing_mm: f64,
    column_spacing_mm: f64,
    source: String,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct RenderCacheFixture {
    first_run: RenderCacheRunFixture,
    second_run: RenderCacheRunFixture,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct RenderCacheRunFixture {
    state: String,
    from_cache: bool,
    progress_stage: String,
    payload_study_id_matches_top_level: bool,
    preview_path_within_temp_cache: bool,
    preview_exists: bool,
    preview_path_reused_from_first_run: Option<bool>,
    loaded_width: u32,
    loaded_height: u32,
}

#[test]
fn describe_study_matches_phase1_fixture() {
    let description = describe_study(&sample_dicom_path()).expect("describe study");
    assert_json_fixture(&sample_fixture_path("describe-study.json"), &description);
}

#[test]
fn render_preview_matches_phase1_fixture() {
    let temp_dir = TempDir::new().expect("create temp dir");
    let preview_path = temp_dir.path().join("render-preview.png");

    render_preview(RenderPreviewRequest {
        input_path: sample_dicom_path(),
        preview_output: preview_path.clone(),
    })
    .expect("render preview");

    assert_png_fixture(
        &sample_fixture_path("render-preview.png"),
        &preview_path,
    );
}

#[test]
fn process_preview_and_xray_export_match_phase1_fixtures() {
    let temp_dir = TempDir::new().expect("create temp dir");
    let preview_path = temp_dir.path().join("process-xray-preview.png");
    let output_path = temp_dir.path().join("process-xray-output.dcm");

    process_study(ProcessStudyRequest {
        input_path: sample_dicom_path(),
        output_path: Some(output_path.clone()),
        preview_output: Some(preview_path.clone()),
        preset: String::from("xray"),
        invert: false,
        brightness: None,
        contrast: None,
        equalize: false,
        compare: false,
        palette: None,
    })
    .expect("process study");

    assert_png_fixture(
        &sample_fixture_path("process-xray-preview.png"),
        &preview_path,
    );

    let export_fixture = summarize_export_fixture("xray", &sample_dicom_path(), &output_path);
    assert_json_fixture(
        &sample_fixture_path("process-xray-export.json"),
        &export_fixture,
    );
}

#[test]
fn compare_preview_and_rgb_export_match_phase1_fixtures() {
    let temp_dir = TempDir::new().expect("create temp dir");
    let preview_path = temp_dir.path().join("process-xray-compare-preview.png");
    let output_path = temp_dir.path().join("process-xray-compare-output.dcm");

    process_study(ProcessStudyRequest {
        input_path: sample_dicom_path(),
        output_path: Some(output_path.clone()),
        preview_output: Some(preview_path.clone()),
        preset: String::from("xray"),
        invert: false,
        brightness: None,
        contrast: None,
        equalize: false,
        compare: true,
        palette: None,
    })
    .expect("process study in compare mode");

    assert_png_fixture(
        &sample_fixture_path("process-xray-compare-preview.png"),
        &preview_path,
    );

    let export_fixture = summarize_export_fixture("compare", &sample_dicom_path(), &output_path);
    assert_json_fixture(
        &sample_fixture_path("process-xray-compare-export.json"),
        &export_fixture,
    );
}

#[test]
fn analyze_study_matches_phase1_fixture() {
    let temp_dir = TempDir::new().expect("create temp dir");
    let preview_path = temp_dir.path().join("analyze-preview.png");
    let state = AppState::default();
    let study = state
        .open_study(sample_dicom_path())
        .expect("open sample study for analysis");
    let result = state
        .analyze_study(
            AnalyzeStudyCommand {
                study_id: study.study_id,
            },
            preview_path.clone(),
        )
        .expect("analyze study");

    assert_png_fixture(
        &sample_fixture_path("analyze-preview.png"),
        &preview_path,
    );

    assert_json_fixture(
        &sample_fixture_path("analyze-study.json"),
        &json!({
            "analysis": result.analysis,
            "suggestedAnnotations": result.suggested_annotations,
        }),
    );
}

#[test]
fn recent_study_catalog_matches_phase1_fixture() {
    let temp_dir = TempDir::new().expect("create temp dir");
    let store = StudyCatalogStore::new(temp_dir.path().join("catalog.json"));
    let sample_path = sample_dicom_path();
    let description = describe_study(&sample_path).expect("describe study");

    store
        .record_opened_study(&StudyRecord {
            study_id: String::from("phase1-study"),
            input_path: sample_path.clone(),
            input_name: file_name(&sample_path),
            measurement_scale: description.measurement_scale,
        })
        .expect("record opened study");

    let catalog = store.load().expect("load catalog");
    let fixture = RecentStudyCatalogFixture {
        recent_studies: catalog
            .recent_studies
            .into_iter()
            .map(|entry| RecentStudyEntryFixture {
                input_path: to_repo_relative(&entry.input_path),
                input_name: entry.input_name,
                measurement_scale: entry.measurement_scale.map(|scale| RecentMeasurementScaleFixture {
                    row_spacing_mm: scale.row_spacing_mm,
                    column_spacing_mm: scale.column_spacing_mm,
                    source: scale.source,
                }),
                last_opened_at_is_rfc3339: DateTime::parse_from_rfc3339(&entry.last_opened_at)
                    .is_ok(),
            })
            .collect(),
    };

    assert_json_fixture(
        &sample_fixture_path("recent-study-catalog.json"),
        &fixture,
    );
}

#[test]
fn render_cache_behavior_matches_phase1_fixture() {
    let state = AppState::default();
    let publish = noop_publisher();
    let first_study = state
        .open_study(sample_dicom_path())
        .expect("open first study");
    let first_started = state
        .start_render_job(
            RenderStudyCommand {
                study_id: first_study.study_id.clone(),
            },
            publish.clone(),
        )
        .expect("start first render job");
    let first_snapshot = wait_for_terminal_job(&state, &first_started.job_id);
    let first_result = render_result(&first_snapshot);

    let second_study = state
        .open_study(sample_dicom_path())
        .expect("open second study");
    let second_started = state
        .start_render_job(
            RenderStudyCommand {
                study_id: second_study.study_id.clone(),
            },
            publish,
        )
        .expect("start second render job");
    let second_snapshot = state
        .get_job(JobCommand {
            job_id: second_started.job_id,
        })
        .expect("get cached render snapshot");
    let second_result = render_result(&second_snapshot);

    let fixture = RenderCacheFixture {
        first_run: RenderCacheRunFixture {
            state: job_state_label(first_snapshot.state),
            from_cache: first_snapshot.from_cache,
            progress_stage: first_snapshot.progress.stage.clone(),
            payload_study_id_matches_top_level: first_snapshot
                .study_id
                .as_ref()
                .is_some_and(|study_id| study_id == &first_result.study_id),
            preview_path_within_temp_cache: first_result
                .preview_path
                .starts_with(expected_render_cache_dir()),
            preview_exists: first_result.preview_path.is_file(),
            preview_path_reused_from_first_run: None,
            loaded_width: first_result.loaded_width,
            loaded_height: first_result.loaded_height,
        },
        second_run: RenderCacheRunFixture {
            state: job_state_label(second_snapshot.state),
            from_cache: second_snapshot.from_cache,
            progress_stage: second_snapshot.progress.stage.clone(),
            payload_study_id_matches_top_level: second_snapshot
                .study_id
                .as_ref()
                .is_some_and(|study_id| study_id == &second_result.study_id),
            preview_path_within_temp_cache: second_result
                .preview_path
                .starts_with(expected_render_cache_dir()),
            preview_exists: second_result.preview_path.is_file(),
            preview_path_reused_from_first_run: Some(
                second_result.preview_path == first_result.preview_path,
            ),
            loaded_width: second_result.loaded_width,
            loaded_height: second_result.loaded_height,
        },
    };

    assert_json_fixture(&sample_fixture_path("render-cache-hit.json"), &fixture);
}

fn sample_dicom_path() -> PathBuf {
    repo_root().join("images").join(format!("{SAMPLE_NAME}.dcm"))
}

fn repo_root() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR"))
        .parent()
        .expect("backend crate should live under the repo root")
        .to_path_buf()
}

fn sample_fixture_path(name: &str) -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("tests")
        .join("fixtures")
        .join("parity")
        .join(SAMPLE_NAME)
        .join(name)
}

fn assert_json_fixture(path: &Path, value: &impl Serialize) {
    let mut actual = serde_json::to_string_pretty(value).expect("serialize json fixture");
    actual.push('\n');

    maybe_write_fixture(path, actual.as_bytes());

    let expected = fs::read_to_string(path).unwrap_or_else(|error| {
        panic!("read json fixture {}: {error}", path.display());
    });
    assert_eq!(actual, expected, "json fixture mismatch at {}", path.display());
}

fn assert_png_fixture(path: &Path, actual_png_path: &Path) {
    let actual_bytes = fs::read(actual_png_path)
        .unwrap_or_else(|error| panic!("read generated png {}: {error}", actual_png_path.display()));
    maybe_write_fixture(path, &actual_bytes);

    let expected_bytes =
        fs::read(path).unwrap_or_else(|error| panic!("read png fixture {}: {error}", path.display()));

    let expected =
        image::load_from_memory_with_format(&expected_bytes, ImageFormat::Png).expect("decode png fixture");
    let actual =
        image::load_from_memory_with_format(&actual_bytes, ImageFormat::Png).expect("decode generated png");

    assert_eq!(actual.width(), expected.width(), "png width mismatch");
    assert_eq!(actual.height(), expected.height(), "png height mismatch");
    assert_eq!(actual.color(), expected.color(), "png color type mismatch");
    assert_eq!(actual.into_bytes(), expected.into_bytes(), "png pixel mismatch");
}

fn maybe_write_fixture(path: &Path, contents: &[u8]) {
    if !write_fixtures_enabled() {
        return;
    }

    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent)
            .unwrap_or_else(|error| panic!("create fixture dir {}: {error}", parent.display()));
    }

    fs::write(path, contents)
        .unwrap_or_else(|error| panic!("write fixture {}: {error}", path.display()));
}

fn write_fixtures_enabled() -> bool {
    std::env::var_os(WRITE_FIXTURES_ENV).is_some()
}

fn summarize_export_fixture(mode: &str, source_path: &Path, output_path: &Path) -> ExportFixture {
    let source = OpenFileOptions::new()
        .open_file(source_path)
        .expect("open source dicom");
    let output = OpenFileOptions::new()
        .open_file(output_path)
        .expect("open output dicom");
    let pixel_data = output
        .element(tags::PIXEL_DATA)
        .expect("find PixelData")
        .to_bytes()
        .expect("read PixelData bytes");

    ExportFixture {
        mode: mode.to_string(),
        rows: required_u16_attr(&output, tags::ROWS, "Rows"),
        columns: required_u16_attr(&output, tags::COLUMNS, "Columns"),
        samples_per_pixel: required_u16_attr(&output, tags::SAMPLES_PER_PIXEL, "SamplesPerPixel"),
        photometric_interpretation: required_string_attr(
            &output,
            tags::PHOTOMETRIC_INTERPRETATION,
            "PhotometricInterpretation",
        ),
        planar_configuration: optional_u16_attr(&output, tags::PLANAR_CONFIGURATION),
        bits_allocated: required_u16_attr(&output, tags::BITS_ALLOCATED, "BitsAllocated"),
        bits_stored: required_u16_attr(&output, tags::BITS_STORED, "BitsStored"),
        high_bit: required_u16_attr(&output, tags::HIGH_BIT, "HighBit"),
        pixel_representation: required_u16_attr(
            &output,
            tags::PIXEL_REPRESENTATION,
            "PixelRepresentation",
        ),
        window_center: optional_string_attr(&output, tags::WINDOW_CENTER),
        window_width: optional_string_attr(&output, tags::WINDOW_WIDTH),
        sop_class_uid: required_string_attr(&output, tags::SOP_CLASS_UID, "SOPClassUID"),
        image_type: required_string_attr(&output, tags::IMAGE_TYPE, "ImageType")
            .split('\\')
            .map(str::to_string)
            .collect(),
        conversion_type: required_string_attr(&output, tags::CONVERSION_TYPE, "ConversionType"),
        series_description: required_string_attr(
            &output,
            tags::SERIES_DESCRIPTION,
            "SeriesDescription",
        ),
        derivation_description: required_string_attr(
            &output,
            tags::DERIVATION_DESCRIPTION,
            "DerivationDescription",
        ),
        manufacturer: required_string_attr(&output, tags::MANUFACTURER, "Manufacturer"),
        manufacturer_model_name: required_string_attr(
            &output,
            tags::MANUFACTURER_MODEL_NAME,
            "ManufacturerModelName",
        ),
        software_versions: required_string_attr(
            &output,
            tags::SOFTWARE_VERSIONS,
            "SoftwareVersions",
        ),
        study_instance_uid_preserved: same_string_attr(
            &source,
            &output,
            tags::STUDY_INSTANCE_UID,
        )
        .unwrap_or(false),
        patient_name_preserved: same_string_attr(&source, &output, tags::PATIENT_NAME),
        patient_id_preserved: same_string_attr(&source, &output, tags::PATIENT_ID),
        pixel_spacing_preserved: same_string_attr(&source, &output, tags::PIXEL_SPACING),
        imager_pixel_spacing_preserved: same_string_attr(
            &source,
            &output,
            tags::IMAGER_PIXEL_SPACING,
        ),
        nominal_scanned_pixel_spacing_preserved: same_string_attr(
            &source,
            &output,
            tags::NOMINAL_SCANNED_PIXEL_SPACING,
        ),
        generated_sop_instance_uid_prefix: required_string_attr(
            &output,
            tags::SOP_INSTANCE_UID,
            "SOPInstanceUID",
        )
        .starts_with("2.25."),
        generated_series_instance_uid_prefix: required_string_attr(
            &output,
            tags::SERIES_INSTANCE_UID,
            "SeriesInstanceUID",
        )
        .starts_with("2.25."),
        instance_creation_date_is_yyyymmdd: is_yyyymmdd(
            &required_string_attr(&output, tags::INSTANCE_CREATION_DATE, "InstanceCreationDate"),
        ),
        instance_creation_time_has_six_digits: has_six_digits(
            &required_string_attr(&output, tags::INSTANCE_CREATION_TIME, "InstanceCreationTime"),
        ),
        pixel_data_length: pixel_data.len(),
        pixel_data_fnv1a64: fnv1a64(pixel_data.as_ref()),
    }
}

fn wait_for_terminal_job(state: &AppState, job_id: &str) -> JobSnapshot {
    let started = Instant::now();

    loop {
        let snapshot = state
            .get_job(JobCommand {
                job_id: job_id.to_string(),
            })
            .expect("get job snapshot");
        if matches!(
            snapshot.state,
            JobState::Completed | JobState::Failed | JobState::Cancelled
        ) {
            return snapshot;
        }

        assert!(
            started.elapsed() < Duration::from_secs(20),
            "job {job_id} did not reach a terminal state in time"
        );
        thread::sleep(Duration::from_millis(50));
    }
}

fn render_result(snapshot: &JobSnapshot) -> &xrayview_backend::api::RenderStudyCommandResult {
    match snapshot.result.as_ref().expect("render snapshot should include a result") {
        JobResult::RenderStudy(result) => result,
        other => panic!("expected renderStudy result, got {other:?}"),
    }
}

fn noop_publisher() -> Arc<dyn Fn(JobSnapshot) + Send + Sync + 'static> {
    Arc::new(|_| {})
}

fn expected_render_cache_dir() -> PathBuf {
    std::env::temp_dir()
        .join("xrayview")
        .join("cache")
        .join("artifacts")
        .join("render")
}

fn to_repo_relative(path: &Path) -> String {
    path.strip_prefix(repo_root())
        .unwrap_or(path)
        .to_string_lossy()
        .replace('\\', "/")
}

fn file_name(path: &Path) -> String {
    path.file_name()
        .and_then(|value| value.to_str())
        .unwrap_or_default()
        .to_string()
}

fn optional_string_attr(object: &impl DicomObject, tag: Tag) -> Option<String> {
    object
        .attr(tag)
        .ok()?
        .to_str()
        .ok()
        .map(|value| value.trim().to_string())
}

fn required_string_attr(object: &impl DicomObject, tag: Tag, label: &str) -> String {
    optional_string_attr(object, tag)
        .unwrap_or_else(|| panic!("missing required {label} ({tag:?})"))
}

fn optional_u16_attr(object: &impl DicomObject, tag: Tag) -> Option<u16> {
    object.attr(tag).ok()?.to_u16().ok()
}

fn required_u16_attr(object: &impl DicomObject, tag: Tag, label: &str) -> u16 {
    optional_u16_attr(object, tag).unwrap_or_else(|| panic!("missing required {label} ({tag:?})"))
}

fn same_string_attr(source: &impl DicomObject, output: &impl DicomObject, tag: Tag) -> Option<bool> {
    let source_value = optional_string_attr(source, tag)?;
    Some(optional_string_attr(output, tag).as_deref() == Some(source_value.as_str()))
}

fn job_state_label(state: JobState) -> String {
    match state {
        JobState::Queued => "queued",
        JobState::Running => "running",
        JobState::Cancelling => "cancelling",
        JobState::Completed => "completed",
        JobState::Failed => "failed",
        JobState::Cancelled => "cancelled",
    }
    .to_string()
}

fn is_yyyymmdd(value: &str) -> bool {
    value.len() == 8 && value.chars().all(|ch| ch.is_ascii_digit())
}

fn has_six_digits(value: &str) -> bool {
    value.len() == 6 && value.chars().all(|ch| ch.is_ascii_digit())
}

fn fnv1a64(bytes: &[u8]) -> String {
    let mut hash = 0xcbf29ce484222325_u64;
    for byte in bytes {
        hash ^= u64::from(*byte);
        hash = hash.wrapping_mul(0x100000001b3);
    }
    format!("{hash:016x}")
}

#[test]
fn export_fixture_summary_sanity_checks_hold() {
    let temp_dir = TempDir::new().expect("create temp dir");
    let output_path = temp_dir.path().join("export.dcm");

    process_study(ProcessStudyRequest {
        input_path: sample_dicom_path(),
        output_path: Some(output_path.clone()),
        preview_output: None,
        preset: String::from("xray"),
        invert: false,
        brightness: None,
        contrast: None,
        equalize: false,
        compare: false,
        palette: None,
    })
    .expect("process study");

    let export = summarize_export_fixture("xray", &sample_dicom_path(), &output_path);
    assert_eq!(export.sop_class_uid, SECONDARY_CAPTURE_SOP_CLASS_UID);
    assert!(export.generated_sop_instance_uid_prefix);
    assert!(export.generated_series_instance_uid_prefix);
}
