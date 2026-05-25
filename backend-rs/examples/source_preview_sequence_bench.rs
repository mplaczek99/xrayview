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

    assert_matching_outputs(&path).expect("matching preview outputs");

    let mut legacy_pixels = 0;
    let legacy_start = Instant::now();
    for _ in 0..iterations {
        legacy_pixels = render_legacy_sequence(black_box(&path)).expect("legacy sequence");
        black_box(legacy_pixels);
    }
    let legacy_elapsed = legacy_start.elapsed();

    let mut shared_pixels = 0;
    let shared_start = Instant::now();
    for _ in 0..iterations {
        shared_pixels =
            render_shared_source_sequence(black_box(&path)).expect("shared-source sequence");
        black_box(shared_pixels);
    }
    let shared_elapsed = shared_start.elapsed();

    let legacy_avg = legacy_elapsed.as_secs_f64() * 1_000.0 / iterations as f64;
    let shared_avg = shared_elapsed.as_secs_f64() * 1_000.0 / iterations as f64;
    println!("source_preview_sequence iterations={iterations} pixels_per_sequence={legacy_pixels}");
    println!(
        "legacy(two file renders): total_ms={:.3} avg_ms={legacy_avg:.3}",
        legacy_elapsed.as_secs_f64() * 1_000.0
    );
    println!(
        "shared(decoded once): total_ms={:.3} avg_ms={shared_avg:.3}",
        shared_elapsed.as_secs_f64() * 1_000.0
    );
    println!(
        "saved_per_sequence_ms={:.3} speedup={:.2}x vmhwm_kb={}",
        legacy_avg - shared_avg,
        legacy_avg / shared_avg,
        peak_resident_set_kb().unwrap_or(0),
    );
    assert_eq!(legacy_pixels, shared_pixels);
}

fn render_legacy_sequence(path: &str) -> Result<usize, String> {
    let render = xrayview_backend_rs::bmp::render_grayscale_preview_file(path)?;
    let analysis =
        xrayview_backend_rs::bmp::render_grayscale_preview_file_for_tooth_analysis(path)?;
    Ok(render.pixels.len() + analysis.pixels.len())
}

fn assert_matching_outputs(path: &str) -> Result<(), String> {
    let legacy_render = xrayview_backend_rs::bmp::render_grayscale_preview_file(path)?;
    let legacy_analysis =
        xrayview_backend_rs::bmp::render_grayscale_preview_file_for_tooth_analysis(path)?;
    let source = xrayview_backend_rs::bmp::decode_source_preview_file(path)?;
    let shared_render = xrayview_backend_rs::bmp::render_grayscale_preview_from_source(&source);
    let shared_analysis =
        xrayview_backend_rs::bmp::render_grayscale_preview_from_source_for_tooth_analysis(&source);

    assert_eq!(legacy_render, shared_render);
    assert_eq!(legacy_analysis, shared_analysis);
    Ok(())
}

fn render_shared_source_sequence(path: &str) -> Result<usize, String> {
    let source = xrayview_backend_rs::bmp::decode_source_preview_file(path)?;
    let render = xrayview_backend_rs::bmp::render_grayscale_preview_from_source(&source);
    let analysis =
        xrayview_backend_rs::bmp::render_grayscale_preview_from_source_for_tooth_analysis(&source);
    Ok(render.pixels.len() + analysis.pixels.len())
}

fn peak_resident_set_kb() -> Option<usize> {
    let status = fs::read_to_string("/proc/self/status").ok()?;
    let line = status.lines().find(|line| line.starts_with("VmHWM:"))?;
    line.split_whitespace().nth(1)?.parse().ok()
}
