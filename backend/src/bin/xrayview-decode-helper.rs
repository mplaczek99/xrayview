use std::io::{self, Write};
use std::path::PathBuf;

use anyhow::{Context, Result};
use clap::Parser;
use xrayview_backend::study::decode_helper::decode_source_study;

#[derive(Parser, Debug)]
#[command(name = "xrayview-decode-helper")]
struct Cli {
    /// Input DICOM path
    #[arg(long)]
    input: PathBuf,
}

fn main() {
    if let Err(error) = run() {
        eprintln!("error: {error}");
        std::process::exit(1);
    }
}

fn run() -> Result<()> {
    let cli = Cli::parse();
    let decoded = decode_source_study(&cli.input)
        .with_context(|| format!("decode source study: {}", cli.input.display()))?;
    serde_json::to_writer(io::stdout(), &decoded)?;
    writeln!(io::stdout())?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::Cli;
    use clap::Parser;
    use std::path::PathBuf;

    #[test]
    fn clap_parses_input_flag() {
        let cli =
            Cli::try_parse_from(["xrayview-decode-helper", "--input", "/tmp/sample-study.dcm"])
                .expect("parse args");

        assert_eq!(cli.input, PathBuf::from("/tmp/sample-study.dcm"));
    }
}
