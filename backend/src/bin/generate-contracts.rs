use std::env;
use std::path::PathBuf;

use anyhow::{Context, Result};
use xrayview_backend::api::write_typescript_contracts;

fn main() {
    if let Err(error) = run() {
        eprintln!("error: {error}");
        std::process::exit(1);
    }
}

fn run() -> Result<()> {
    let output_path = env::args_os()
        .nth(1)
        .map(PathBuf::from)
        .context("usage: generate-contracts <output-path>")?;

    write_typescript_contracts(&output_path).with_context(|| {
        format!(
            "write generated TypeScript contracts to {}",
            output_path.display()
        )
    })?;

    println!("generated {}", output_path.display());
    Ok(())
}
