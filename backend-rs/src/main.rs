use std::{env, io, sync::Arc};

use xrayview_backend_rs::{app::App, cli, config::Config, http};

fn main() {
    let args = env::args().skip(1).collect::<Vec<_>>();
    if !should_serve(&args) {
        if let Err(error) = cli::run(&args, &mut io::stdout(), &mut io::stderr()) {
            eprintln!("{error}");
            std::process::exit(1);
        }
        return;
    }

    if args.first().is_some_and(|arg| arg == "serve") && args.len() > 1 {
        eprintln!("serve does not accept arguments");
        std::process::exit(1);
    }

    let config = match Config::load() {
        Ok(config) => config,
        Err(error) => {
            eprintln!("{error}");
            std::process::exit(1);
        }
    };

    let app = match App::new(config) {
        Ok(app) => Arc::new(app),
        Err(error) => {
            eprintln!("{error}");
            std::process::exit(1);
        }
    };

    if let Err(error) = app.prepare() {
        eprintln!("{error}");
        std::process::exit(1);
    }

    if let Err(error) = http::serve(app) {
        eprintln!("{error}");
        std::process::exit(1);
    }
}

fn should_serve(args: &[String]) -> bool {
    match args.first().map(String::as_str) {
        None | Some("serve") => true,
        _ => false,
    }
}
