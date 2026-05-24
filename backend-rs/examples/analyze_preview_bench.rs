use std::{env, fs, hint::black_box, time::Instant};

use xrayview_backend_rs::render::PreviewImage;

fn main() {
    let mut args = env::args().skip(1);
    let path = args
        .next()
        .unwrap_or_else(|| "../images/BMP/1.bmp".to_string());
    let iterations = args
        .next()
        .map(|value| value.parse::<usize>().expect("iterations must be a number"))
        .unwrap_or(10);

    let rendered =
        xrayview_backend_rs::bmp::render_grayscale_preview_file_for_tooth_analysis(&path)
            .expect("render analysis preview");
    let mut pixels = Vec::from(rendered.pixels.as_ref());
    if let Some(pixel) = pixels.first_mut() {
        *pixel = pixel.wrapping_add(1);
    }
    let preview = PreviewImage::gray(rendered.width, rendered.height, pixels);

    let mut overlay_pixels = 0;
    let start = Instant::now();
    for _ in 0..iterations {
        let result = xrayview_backend_rs::analysis::generate_tooth_overlay(black_box(&preview))
            .expect("generate tooth overlay");
        overlay_pixels = result.preview.pixels.len() + result.filled_preview.pixels.len();
        black_box(result);
    }
    let elapsed = start.elapsed();

    println!(
        "generate_tooth_overlay iterations={iterations} image={}x{} overlay_pixels={overlay_pixels} total_ms={:.3} avg_ms={:.3} vmhwm_kb={}",
        preview.width,
        preview.height,
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
