use std::{env, io};

use xrayview_backend_rs::cli;

fn main() {
    let args = env::args().skip(1).collect::<Vec<_>>();
    let args = args.iter().map(String::as_str).collect::<Vec<_>>();
    if let Err(error) = cli::run(&args, &mut io::stdout(), &mut io::stderr()) {
        eprintln!("{error}");
        std::process::exit(1);
    }
}
