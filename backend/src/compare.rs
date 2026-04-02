use anyhow::{Result, bail};

use crate::preview::{PreviewFormat, PreviewImage};

pub fn combine_comparison(left: &PreviewImage, right: &PreviewImage) -> Result<PreviewImage> {
    // Emit a single RGBA image so the compare view can be saved and displayed
    // by the same preview/export code paths as any other processed result.
    if left.format != PreviewFormat::Gray8 {
        bail!("compare preview requires grayscale source on the left side")
    }
    if left.width != right.width || left.height != right.height {
        bail!("compare preview requires matching image dimensions")
    }

    let width = left.width as usize;
    let height = left.height as usize;
    let combined_width = left
        .width
        .checked_mul(2)
        .ok_or_else(|| anyhow::anyhow!("compare preview width overflow"))?;
    let mut pixels = vec![0_u8; combined_width as usize * height * 4];

    for y in 0..height {
        let dst_row_start = y * combined_width as usize * 4;
        let dst_row = &mut pixels[dst_row_start..dst_row_start + combined_width as usize * 4];

        // The source preview is always grayscale, so expand it to opaque RGBA
        // before appending the processed half.
        let left_row = &left.pixels[y * width..(y + 1) * width];
        for (index, value) in left_row.iter().copied().enumerate() {
            let dst = &mut dst_row[index * 4..index * 4 + 4];
            dst[0] = value;
            dst[1] = value;
            dst[2] = value;
            dst[3] = 255;
        }

        match right.format {
            PreviewFormat::Gray8 => {
                // Processed output can still be grayscale when no palette was applied.
                let right_row = &right.pixels[y * width..(y + 1) * width];
                let base = width * 4;
                for (index, value) in right_row.iter().copied().enumerate() {
                    let dst = &mut dst_row[base + index * 4..base + index * 4 + 4];
                    dst[0] = value;
                    dst[1] = value;
                    dst[2] = value;
                    dst[3] = 255;
                }
            }
            PreviewFormat::Rgba8 => {
                let src_row_start = y * width * 4;
                let src_row = &right.pixels[src_row_start..src_row_start + width * 4];
                dst_row[width * 4..width * 8].copy_from_slice(src_row);
            }
        }
    }

    Ok(PreviewImage::rgba(combined_width, left.height, pixels))
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::path::Path;
    use std::time::Instant;

    use tempfile::TempDir;

    use crate::palette::apply_named_palette;
    use crate::preview::{load_preview, save_preview_png};
    use crate::processing::{GrayscaleControls, process_grayscale_pixels};

    #[test]
    fn combine_comparison_places_images_side_by_side() {
        let left = PreviewImage {
            width: 2,
            height: 1,
            pixels: vec![10, 20],
            format: PreviewFormat::Gray8,
        };
        let right = PreviewImage {
            width: 2,
            height: 1,
            pixels: vec![100, 110, 120, 255, 200, 210, 220, 255],
            format: PreviewFormat::Rgba8,
        };

        let got = combine_comparison(&left, &right).expect("combine comparison");

        assert_eq!(got.width, 4);
        assert_eq!(got.height, 1);
        assert_eq!(got.format, PreviewFormat::Rgba8);
        assert_eq!(
            got.pixels,
            vec![
                10, 10, 10, 255, 20, 20, 20, 255, 100, 110, 120, 255, 200, 210, 220, 255
            ]
        );
    }

    #[test]
    #[ignore = "manual timing benchmark"]
    fn timing_sample_compare_components() {
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

        let original = preview.expect("preview image should exist");
        let process_start = Instant::now();
        let mut processed = None;
        for _ in 0..iterations {
            let mut copy = original.clone();
            let _ = process_grayscale_pixels(&mut copy.pixels, &controls)
                .expect("process grayscale pixels");
            processed = Some(copy);
        }
        let process_avg = process_start.elapsed().as_secs_f64() / f64::from(iterations);

        let processed = processed.expect("processed preview should exist");
        let palette_start = Instant::now();
        let mut colored = None;
        for _ in 0..iterations {
            colored = Some(apply_named_palette(&processed, "bone").expect("apply palette"));
        }
        let palette_avg = palette_start.elapsed().as_secs_f64() / f64::from(iterations);

        let colored = colored.expect("colored preview should exist");
        let compare_start = Instant::now();
        let mut combined = None;
        for _ in 0..iterations {
            combined = Some(combine_comparison(&original, &colored).expect("combine comparison"));
        }
        let compare_avg = compare_start.elapsed().as_secs_f64() / f64::from(iterations);

        let combined = combined.expect("combined preview should exist");
        let save_start = Instant::now();
        for index in 0..iterations {
            let output = output_dir.path().join(format!("compare-{index}.png"));
            save_preview_png(&output, &combined).expect("save preview png");
        }
        let save_avg = save_start.elapsed().as_secs_f64() / f64::from(iterations);

        eprintln!(
            "rust compare load_avg_s={load_avg:.6} process_avg_s={process_avg:.6} palette_avg_s={palette_avg:.6} compare_avg_s={compare_avg:.6} save_png_avg_s={save_avg:.6}"
        );
    }
}
