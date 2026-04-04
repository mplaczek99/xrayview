use anyhow::Result;

use crate::compare::combine_comparison;
use crate::palette::apply_named_palette;
use crate::preview::PreviewImage;
use crate::render::render_plan::{RenderPlan, render_source_image};
use crate::study::source_image::SourceImage;

use super::{GrayscaleControls, process_grayscale_pixels};

#[derive(Debug, Clone)]
pub struct PipelineOutput {
    pub preview: PreviewImage,
    pub mode: String,
}

pub fn process_source_image(
    source: &SourceImage,
    controls: &GrayscaleControls,
    palette: &str,
    compare: bool,
) -> Result<PipelineOutput> {
    let source_preview = render_source_image(source, &RenderPlan::default());
    let mut processed_pixels = source_preview.pixels.clone();
    let mut mode = process_grayscale_pixels(&mut processed_pixels, controls)?;
    let processed_grayscale =
        PreviewImage::grayscale(source.width, source.height, processed_pixels);

    let mut preview = if palette == "none" {
        processed_grayscale.clone()
    } else {
        mode = format!("{mode} with {palette} palette");
        apply_named_palette(&processed_grayscale, palette)?
    };

    if compare {
        preview = combine_comparison(&source_preview, &preview)?;
        mode = format!("comparison of grayscale and {mode}");
    }

    Ok(PipelineOutput { preview, mode })
}

#[cfg(test)]
mod tests {
    use crate::preview::PreviewFormat;
    use crate::study::source_image::SourceImage;

    use super::{GrayscaleControls, process_source_image};

    #[test]
    fn pipeline_renders_from_source_and_applies_grayscale_controls() {
        let source = SourceImage {
            width: 3,
            height: 1,
            pixels: vec![0.0, 512.0, 1024.0],
            min_value: 0.0,
            max_value: 1024.0,
            default_window: None,
            invert: false,
        };
        let controls = GrayscaleControls {
            invert: false,
            brightness: 20,
            contrast: 1.0,
            equalize: false,
        };

        let output =
            process_source_image(&source, &controls, "none", false).expect("process source image");

        assert_eq!(output.preview.format, PreviewFormat::Gray8);
        assert_eq!(output.preview.pixels, vec![20, 148, 255]);
    }

    #[test]
    fn pipeline_compare_output_is_rgba_and_double_width() {
        let source = SourceImage {
            width: 2,
            height: 1,
            pixels: vec![0.0, 255.0],
            min_value: 0.0,
            max_value: 255.0,
            default_window: None,
            invert: false,
        };
        let controls = GrayscaleControls {
            invert: false,
            brightness: 0,
            contrast: 1.0,
            equalize: false,
        };

        let output =
            process_source_image(&source, &controls, "bone", true).expect("process source image");

        assert_eq!(output.preview.format, PreviewFormat::Rgba8);
        assert_eq!(output.preview.width, 4);
    }
}
