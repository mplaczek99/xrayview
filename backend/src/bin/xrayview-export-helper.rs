use std::io;
use std::path::PathBuf;

use anyhow::{Context, Result};
use clap::Parser;
use xrayview_backend::export::helper::{
    SecondaryCaptureExportRequest, write_secondary_capture_request,
};

#[derive(Parser, Debug)]
#[command(name = "xrayview-export-helper")]
struct Cli {
    /// Output DICOM path
    #[arg(long)]
    output: PathBuf,
}

fn main() {
    if let Err(error) = run() {
        eprintln!("error: {error}");
        std::process::exit(1);
    }
}

fn run() -> Result<()> {
    let cli = Cli::parse();
    let request: SecondaryCaptureExportRequest =
        serde_json::from_reader(io::stdin()).context("decode export request from stdin")?;
    write_secondary_capture_request(request, &cli.output)?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::Cli;
    use clap::Parser;
    use std::path::PathBuf;

    #[test]
    fn clap_parses_output_flag() {
        let cli = Cli::try_parse_from([
            "xrayview-export-helper",
            "--output",
            "/tmp/processed-output.dcm",
        ])
        .expect("parse args");

        assert_eq!(cli.output, PathBuf::from("/tmp/processed-output.dcm"));
    }
}
