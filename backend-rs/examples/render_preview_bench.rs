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

fn peak_resident_set_kb() -> Option<usize> {
    let status = fs::read_to_string("/proc/self/status").ok()?;
    let line = status.lines().find(|line| line.starts_with("VmHWM:"))?;
    line.split_whitespace().nth(1)?.parse().ok()
}
