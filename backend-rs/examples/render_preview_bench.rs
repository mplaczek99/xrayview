// Microbenchmark for the render pipeline (decode + grayscale stretch).
// Reads the BMP once outside the timing loop so we measure render only,
// not file I/O. Also reports peak RSS — useful when comparing memory
// optimizations (the engineering-priorities memory we care about).
//
// Default iterations=100 (decode is more expensive than read_file).

use std::{env, fs, hint::black_box, time::Instant};

fn main() {
    let mut args = env::args().skip(1);
    let path = args
        .next()
        .unwrap_or_else(|| "images/BMP/1.bmp".to_string());
    let iterations = args
        .next()
        .map(|value| value.parse::<usize>().expect("iterations must be a number"))
        .unwrap_or(100);

    // Load bytes once — we're measuring render, not disk.
    let bytes = fs::read(&path).expect("read BMP fixture");

    let mut pixel_count = 0;
    let start = Instant::now();
    for _ in 0..iterations {
        let preview = xrayview_backend_rs::bmp::render_grayscale_preview(black_box(&bytes))
            .expect("render preview");
        pixel_count = preview.pixels.len();
        black_box(preview);
    }
    let elapsed = start.elapsed();

    println!(
        "render_grayscale_preview iterations={iterations} pixels={pixel_count} total_ms={:.3} avg_ms={:.3} vmhwm_kb={}",
        elapsed.as_secs_f64() * 1_000.0,
        elapsed.as_secs_f64() * 1_000.0 / iterations as f64,
        peak_resident_set_kb().unwrap_or(0),
    );
}

// Linux-only: parse VmHWM (peak resident set size) out of /proc/self/status.
// Returns None on non-Linux or if the format ever changes — falls back to
// printing 0 in the report.
fn peak_resident_set_kb() -> Option<usize> {
    let status = fs::read_to_string("/proc/self/status").ok()?;
    let line = status.lines().find(|line| line.starts_with("VmHWM:"))?;
    line.split_whitespace().nth(1)?.parse().ok()
}
