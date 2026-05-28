// Microbenchmark: how fast can we re-read BMP metadata from disk? Used when
// tuning the open_study path — read_file is on the hot path the first time
// the UI sees a study, and we want it to feel instant.
//
// Run: `cargo run --release --example read_metadata_bench -- path/to.bmp 1000`
// Default fixture path assumes images/BMP/ has been populated locally — the
// directory is gitignored, so on CI you'll have to pass an explicit path.

use std::{env, hint::black_box, time::Instant};

fn main() {
    let mut args = env::args().skip(1);
    let path = args
        .next()
        .unwrap_or_else(|| "images/BMP/1.bmp".to_string());
    let iterations = args
        .next()
        .map(|value| value.parse::<usize>().expect("iterations must be a number"))
        .unwrap_or(1_000);

    let start = Instant::now();
    for _ in 0..iterations {
        // black_box on the input prevents LLVM from hoisting the read out
        // of the loop and on the output prevents it being optimized away.
        let metadata = xrayview_backend_rs::bmp::read_file(black_box(&path)).unwrap();
        black_box(metadata);
    }
    let elapsed = start.elapsed();

    println!(
        "read_file iterations={iterations} total_ms={:.3} avg_us={:.3}",
        elapsed.as_secs_f64() * 1_000.0,
        elapsed.as_secs_f64() * 1_000_000.0 / iterations as f64
    );
}
