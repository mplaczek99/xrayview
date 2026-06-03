use std::{env, io};

use xrayview_backend_rs::cli;

// Tiny shim: this binary is the CLI front-end for the library crate. All real
// behavior lives in cli::run so it can be exercised from tests without
// touching argv/stdio.
fn main() {
    // Skip argv[0] (the program path itself); cli::run expects only the flags.
    let args = env::args().skip(1).collect::<Vec<_>>();
    // Re-borrow as &str so cli::run can stay lifetime-free over the storage.
    let args = args.iter().map(String::as_str).collect::<Vec<_>>();
    if let Err(error) = cli::run(&args, &mut io::stdout(), &mut io::stderr()) {
        // Already-formatted error from the CLI layer — just surface it as-is.
        eprintln!("{error}");
        // Non-zero exit so shell pipelines / CI fail loudly.
        std::process::exit(1);
    }
}
