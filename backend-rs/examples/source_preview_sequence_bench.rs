// Comparative benchmark: does the source-preview-cache pattern (decode once,
// render N variants from the same source) actually beat the "just call each
// render variant from scratch" pattern? This bench answers that quantitatively.
//
// Reports a speedup factor — if shared-source ever stops beating repeated
// file-based renders by a solid margin, something has regressed in
// render_grayscale_preview_from_source.

use std::{env, fs, hint::black_box, time::Instant};

fn main() {
    let mut args = env::args().skip(1);
    let path = args
        .next()
        .unwrap_or_else(|| "../images/BMP/1.bmp".to_string());
    let iterations = args
        .next()
        .map(|value| value.parse::<usize>().expect("iterations must be a number"))
        .unwrap_or(200);

    // Correctness gate: prove the two paths produce byte-identical output
    // before we report timings. A 5× speedup is meaningless if the answers
    // differ.
    assert_matching_outputs(&path).expect("matching preview outputs");

    let mut file_based_pixels = 0;
    let file_based_start = Instant::now();
    for _ in 0..iterations {
        file_based_pixels =
            render_file_based_sequence(black_box(&path)).expect("file-based sequence");
        black_box(file_based_pixels);
    }
    let file_based_elapsed = file_based_start.elapsed();

    let mut shared_pixels = 0;
    let shared_start = Instant::now();
    for _ in 0..iterations {
        shared_pixels =
            render_shared_source_sequence(black_box(&path)).expect("shared-source sequence");
        black_box(shared_pixels);
    }
    let shared_elapsed = shared_start.elapsed();

    let file_based_avg = file_based_elapsed.as_secs_f64() * 1_000.0 / iterations as f64;
    let shared_avg = shared_elapsed.as_secs_f64() * 1_000.0 / iterations as f64;
    println!(
        "source_preview_sequence iterations={iterations} pixels_per_sequence={file_based_pixels}"
    );
    println!(
        "file_based(two file renders): total_ms={:.3} avg_ms={file_based_avg:.3}",
        file_based_elapsed.as_secs_f64() * 1_000.0
    );
    println!(
        "shared(decoded once): total_ms={:.3} avg_ms={shared_avg:.3}",
        shared_elapsed.as_secs_f64() * 1_000.0
    );
    println!(
        "saved_per_sequence_ms={:.3} speedup={:.2}x vmhwm_kb={}",
        file_based_avg - shared_avg,
        file_based_avg / shared_avg,
        peak_resident_set_kb().unwrap_or(0),
    );
    assert_eq!(file_based_pixels, shared_pixels);
}

// Two separate file-based render calls: the straightforward baseline.
fn render_file_based_sequence(path: &str) -> Result<usize, String> {
    let render = xrayview_backend_rs::bmp::render_grayscale_preview_file(path)?;
    let analysis =
        xrayview_backend_rs::bmp::render_grayscale_preview_file_for_tooth_analysis(path)?;
    Ok(render.pixels.len() + analysis.pixels.len())
}

fn assert_matching_outputs(path: &str) -> Result<(), String> {
    let file_based_render = xrayview_backend_rs::bmp::render_grayscale_preview_file(path)?;
    let file_based_analysis =
        xrayview_backend_rs::bmp::render_grayscale_preview_file_for_tooth_analysis(path)?;
    let source = xrayview_backend_rs::bmp::decode_source_preview_file(path)?;
    let shared_render = xrayview_backend_rs::bmp::render_grayscale_preview_from_source(&source);
    let shared_analysis =
        xrayview_backend_rs::bmp::render_grayscale_preview_from_source_for_tooth_analysis(&source);

    assert_eq!(file_based_render, shared_render);
    assert_eq!(file_based_analysis, shared_analysis);
    Ok(())
}

// Single decode → render both variants from the shared source. This is the
// pattern App actually uses (via load_source_preview / load_analysis_preview).
fn render_shared_source_sequence(path: &str) -> Result<usize, String> {
    let source = xrayview_backend_rs::bmp::decode_source_preview_file(path)?;
    let render = xrayview_backend_rs::bmp::render_grayscale_preview_from_source(&source);
    let analysis =
        xrayview_backend_rs::bmp::render_grayscale_preview_from_source_for_tooth_analysis(&source);
    Ok(render.pixels.len() + analysis.pixels.len())
}

// Linux-only RSS reader (no-op everywhere else).
fn peak_resident_set_kb() -> Option<usize> {
    let status = fs::read_to_string("/proc/self/status").ok()?;
    let line = status.lines().find(|line| line.starts_with("VmHWM:"))?;
    line.split_whitespace().nth(1)?.parse().ok()
}
