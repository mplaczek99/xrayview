use std::io::{self, Write};
use std::path::PathBuf;

use anyhow::{Context, Result, bail};
use clap::Parser;
use xrayview_backend::api::{AnalyzeStudyRequest, ProcessStudyRequest, RenderPreviewRequest};
use xrayview_backend::app::{
    analyze_study, describe_study, process_study, processing_manifest, render_preview,
};

#[derive(Parser, Debug)]
#[command(name = "xrayview")]
struct Cli {
    /// Input DICOM path
    #[arg(long)]
    input: Option<PathBuf>,

    /// Output DICOM path
    #[arg(long)]
    output: Option<PathBuf>,

    /// Preview PNG path
    #[arg(long = "preview-output")]
    preview_output: Option<PathBuf>,

    /// Print processing preset metadata as JSON
    #[arg(long)]
    describe_presets: bool,

    /// Print study measurement metadata as JSON
    #[arg(long = "describe-study")]
    describe_study: bool,

    /// Analyze the study and return automatic tooth measurements as JSON
    #[arg(long = "analyze-tooth")]
    analyze_tooth: bool,

    /// Processing preset
    #[arg(long, default_value = "default")]
    preset: String,

    /// Invert grayscale
    #[arg(long)]
    invert: bool,

    /// Brightness adjustment (-256 to 256)
    #[arg(long)]
    brightness: Option<i32>,

    /// Contrast multiplier (>= 0.0)
    #[arg(long)]
    contrast: Option<f64>,

    /// Apply histogram equalization
    #[arg(long)]
    equalize: bool,

    /// Show before/after comparison
    #[arg(long)]
    compare: bool,

    /// Color palette (none, hot, bone)
    #[arg(long)]
    palette: Option<String>,
}

fn main() {
    if let Err(error) = run() {
        eprintln!("error: {error}");
        std::process::exit(1);
    }
}

fn run() -> Result<()> {
    let cli = Cli::parse();
    validate_mode_selection(&cli)?;

    if cli.describe_presets {
        serde_json::to_writer(io::stdout(), &processing_manifest())?;
        writeln!(io::stdout())?;
        return Ok(());
    }

    if cli.describe_study {
        let input_path = cli.input.clone().context("--input is required")?;
        let description = describe_study(&input_path)?;
        serde_json::to_writer(io::stdout(), &description)?;
        writeln!(io::stdout())?;
        return Ok(());
    }

    if cli.analyze_tooth {
        let input_path = cli.input.clone().context("--input is required")?;
        let analysis = analyze_study(AnalyzeStudyRequest {
            input_path,
            preview_output: cli.preview_output.clone(),
        })?;
        serde_json::to_writer(io::stdout(), &analysis.analysis)?;
        writeln!(io::stdout())?;
        return Ok(());
    }

    let input_path = cli.input.clone().context("--input is required")?;

    if is_plain_preview_request(&cli) {
        let preview = render_preview(RenderPreviewRequest {
            input_path,
            preview_output: cli
                .preview_output
                .clone()
                .expect("plain preview request requires a preview output path"),
        })?;

        println!(
            "loaded dicom image: {}x{}",
            preview.loaded_width, preview.loaded_height
        );
        println!(
            "saved grayscale preview image: {}",
            preview.preview_output.display()
        );
        return Ok(());
    }

    let result = process_study(ProcessStudyRequest {
        input_path,
        output_path: cli.output.clone(),
        preview_output: cli.preview_output.clone(),
        preset: cli.preset.clone(),
        invert: cli.invert,
        brightness: cli.brightness,
        contrast: cli.contrast,
        equalize: cli.equalize,
        compare: cli.compare,
        palette: cli.palette.clone(),
    })?;

    println!(
        "loaded dicom image: {}x{}",
        result.loaded_width, result.loaded_height
    );
    if let Some(preview_path) = result.preview_output.as_ref() {
        println!(
            "saved {} preview image: {}",
            result.mode,
            preview_path.display()
        );
    }
    if let Some(output_path) = result.output_path.as_ref() {
        println!(
            "saved {} dicom image: {}",
            result.mode,
            output_path.display()
        );
    }

    Ok(())
}

fn validate_mode_selection(cli: &Cli) -> Result<()> {
    let mode_count = [cli.describe_presets, cli.describe_study, cli.analyze_tooth]
        .into_iter()
        .filter(|enabled| *enabled)
        .count();

    if mode_count > 1 {
        bail!(
            "choose only one backend mode: --describe-presets, --describe-study, or --analyze-tooth"
        );
    }

    Ok(())
}

fn is_plain_preview_request(cli: &Cli) -> bool {
    cli.preview_output.is_some()
        && cli.output.is_none()
        && cli.preset.eq_ignore_ascii_case("default")
        && !cli.invert
        && cli.brightness.is_none()
        && cli.contrast.is_none()
        && !cli.equalize
        && !cli.compare
        && cli.palette.is_none()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn clap_parses_double_dash_flags() {
        let cli = Cli::try_parse_from([
            "xrayview",
            "--input",
            "study.dcm",
            "--describe-study",
            "--equalize",
        ])
        .expect("parse args");

        assert_eq!(cli.input, Some(PathBuf::from("study.dcm")));
        assert!(cli.describe_study);
        assert!(cli.equalize);
    }

    #[test]
    fn clap_parses_tooth_analysis_flag() {
        let cli = Cli::try_parse_from([
            "xrayview",
            "--input",
            "study.dcm",
            "--preview-output",
            "/tmp/study.png",
            "--analyze-tooth",
        ])
        .expect("parse args");

        assert_eq!(cli.input, Some(PathBuf::from("study.dcm")));
        assert_eq!(cli.preview_output, Some(PathBuf::from("/tmp/study.png")));
        assert!(cli.analyze_tooth);
    }

    #[test]
    fn clap_parses_desktop_and_cli_invocations() {
        let cli =
            Cli::try_parse_from(["xrayview", "--describe-presets"]).expect("describe-presets");
        assert!(cli.describe_presets);

        let cli = Cli::try_parse_from([
            "xrayview",
            "--input",
            "foo.dcm",
            "--preview-output",
            "/tmp/out.png",
        ])
        .expect("input + preview-output");
        assert_eq!(cli.input, Some(PathBuf::from("foo.dcm")));
        assert_eq!(cli.preview_output, Some(PathBuf::from("/tmp/out.png")));

        let cli = Cli::try_parse_from([
            "xrayview",
            "--input",
            "foo.dcm",
            "--output",
            "bar.dcm",
            "--invert",
            "--brightness",
            "20",
            "--contrast",
            "1.5",
            "--equalize",
            "--palette",
            "bone",
        ])
        .expect("full processing flags");
        assert_eq!(cli.input, Some(PathBuf::from("foo.dcm")));
        assert_eq!(cli.output, Some(PathBuf::from("bar.dcm")));
        assert!(cli.invert);
        assert_eq!(cli.brightness, Some(20));
        assert_eq!(cli.contrast, Some(1.5));
        assert!(cli.equalize);
        assert_eq!(cli.palette, Some("bone".to_string()));
    }
}
