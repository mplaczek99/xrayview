use anyhow::{Result, bail};
use rayon::prelude::*;

use crate::preview::{PreviewFormat, PreviewImage};

pub fn apply_named_palette(src: &PreviewImage, name: &str) -> Result<PreviewImage> {
    if src.format != PreviewFormat::Gray8 {
        bail!("pseudocolor palettes require grayscale preview input")
    }

    // Palette application is the last stage in the preview pipeline, so the
    // grayscale image is promoted to RGBA exactly once here.
    let pixels = match name {
        "hot" => apply_palette(src, hot_color),
        "bone" => apply_palette(src, bone_color),
        _ => bail!("palette must be one of: none, hot, bone"),
    };

    Ok(PreviewImage::rgba(src.width, src.height, pixels))
}

fn apply_palette(src: &PreviewImage, color_fn: fn(u8) -> [u8; 4]) -> Vec<u8> {
    // Pre-compute LUT: only 256 possible Gray8 input values, eliminates
    // per-pixel function calls (~4M calls → 256 for a 2048x2048 image).
    let lut: [[u8; 4]; 256] = std::array::from_fn(|i| color_fn(i as u8));

    let mut pixels = vec![0_u8; src.pixels.len() * 4];
    // rayon: parallel pixel loop
    pixels
        .par_chunks_mut(4)
        .zip(src.pixels.par_iter().copied())
        .for_each(|(dst, value)| {
            dst.copy_from_slice(&lut[value as usize]);
        });
    pixels
}

fn hot_color(value: u8) -> [u8; 4] {
    match value {
        0..=84 => [value.saturating_mul(3), 0, 0, 255],
        85..=169 => [255, (value - 85).saturating_mul(3), 0, 255],
        _ => [255, 255, (value - 170).saturating_mul(3), 255],
    }
}

fn bone_color(value: u8) -> [u8; 4] {
    let value = i32::from(value);
    let white_boost = (value - 128).max(0);
    // "bone" stays close to neutral in the shadows, then adds a cool highlight
    // lift so bright anatomy reads more like clinical film.
    let red = clamp8((value * 7) / 8 + white_boost);
    let green = clamp8((value * 7) / 8 + white_boost + value / 16);
    let blue = clamp8(value + white_boost / 2);
    [red, green, blue, 255]
}

fn clamp8(value: i32) -> u8 {
    if value < 0 {
        0
    } else if value > 255 {
        255
    } else {
        value as u8
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::path::Path;
    use std::time::Instant;

    use tempfile::TempDir;

    use crate::preview::{load_preview, save_preview_png};
    use crate::processing::{GrayscaleControls, process_grayscale_pixels};

    #[test]
    fn hot_palette_breakpoints() {
        assert_eq!(hot_color(0), [0, 0, 0, 255]);
        assert_eq!(hot_color(84), [252, 0, 0, 255]);
        assert_eq!(hot_color(85), [255, 0, 0, 255]);
        assert_eq!(hot_color(170), [255, 255, 0, 255]);
        assert_eq!(hot_color(255), [255, 255, 255, 255]);
    }

    #[test]
    fn bone_palette_formula() {
        assert_eq!(bone_color(0), [0, 0, 0, 255]);
        assert_eq!(bone_color(128), [112, 120, 128, 255]);
        assert_eq!(bone_color(255), [255, 255, 255, 255]);
    }

    #[test]
    #[ignore = "manual timing benchmark"]
    fn timing_sample_pseudocolor_components() {
        let sample =
            Path::new(env!("CARGO_MANIFEST_DIR")).join("../images/sample-dental-radiograph.dcm");
        let output_dir = TempDir::new().expect("create temp dir");
        let iterations = 30;
        let controls = GrayscaleControls {
            invert: false,
            brightness: 10,
            contrast: 1.4,
            equalize: true,
            pipeline: None,
        };

        let load_start = Instant::now();
        let mut preview = None;
        for _ in 0..iterations {
            preview = Some(load_preview(&sample).expect("load preview"));
        }
        let load_avg = load_start.elapsed().as_secs_f64() / f64::from(iterations);

        let mut processed = preview.expect("preview image should exist");
        let process_start = Instant::now();
        for _ in 0..iterations {
            let mut copy = processed.clone();
            let _ = process_grayscale_pixels(&mut copy.pixels, &controls)
                .expect("process grayscale pixels");
        }
        let process_avg = process_start.elapsed().as_secs_f64() / f64::from(iterations);

        let _ = process_grayscale_pixels(&mut processed.pixels, &controls)
            .expect("process once for palette timing");
        let palette_start = Instant::now();
        let mut colored = None;
        for _ in 0..iterations {
            colored = Some(apply_named_palette(&processed, "bone").expect("apply palette"));
        }
        let palette_avg = palette_start.elapsed().as_secs_f64() / f64::from(iterations);

        let colored = colored.expect("colored preview should exist");
        let save_start = Instant::now();
        for index in 0..iterations {
            let output = output_dir.path().join(format!("preview-{index}.png"));
            save_preview_png(&output, &colored).expect("save preview png");
        }
        let save_avg = save_start.elapsed().as_secs_f64() / f64::from(iterations);

        eprintln!(
            "rust pseudocolor load_avg_s={load_avg:.6} process_avg_s={process_avg:.6} palette_avg_s={palette_avg:.6} save_png_avg_s={save_avg:.6}"
        );
    }
}
