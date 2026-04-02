use std::path::Path;

use anyhow::{Context, Result};

use crate::render::render_plan::{RenderPlan, render_source_image};
use crate::study::source_image::load_source_study;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PreviewImage {
    pub width: u32,
    pub height: u32,
    pub pixels: Vec<u8>,
    pub format: PreviewFormat,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PreviewFormat {
    Gray8,
    Rgba8,
}

#[allow(dead_code)] // used by integration tests
pub fn load_preview(path: &Path) -> Result<PreviewImage> {
    let source = load_source_study(path)?;
    Ok(render_source_image(&source.image, &RenderPlan::default()))
}

impl PreviewImage {
    pub fn grayscale(width: u32, height: u32, pixels: Vec<u8>) -> Self {
        Self {
            width,
            height,
            pixels,
            format: PreviewFormat::Gray8,
        }
    }

    pub fn rgba(width: u32, height: u32, pixels: Vec<u8>) -> Self {
        Self {
            width,
            height,
            pixels,
            format: PreviewFormat::Rgba8,
        }
    }

    pub fn into_dynamic_image(self) -> image::DynamicImage {
        match self.format {
            PreviewFormat::Gray8 => image::DynamicImage::ImageLuma8(
                image::GrayImage::from_raw(self.width, self.height, self.pixels)
                    .expect("valid gray image dimensions"),
            ),
            PreviewFormat::Rgba8 => image::DynamicImage::ImageRgba8(
                image::RgbaImage::from_raw(self.width, self.height, self.pixels)
                    .expect("valid rgba image dimensions"),
            ),
        }
    }
}

pub fn save_preview_png(path: &Path, preview: &PreviewImage) -> Result<()> {
    let color_type = match preview.format {
        PreviewFormat::Gray8 => image::ColorType::L8,
        PreviewFormat::Rgba8 => image::ColorType::Rgba8,
    };

    image::save_buffer_with_format(
        path,
        &preview.pixels,
        preview.width,
        preview.height,
        color_type,
        image::ImageFormat::Png,
    )
    .with_context(|| format!("encode output image: {}", path.display()))
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::render::render_plan::{RenderPlan, render_source_image};
    use crate::study::source_image::load_source_study;

    fn sample_dicom_path() -> std::path::PathBuf {
        let sample =
            Path::new(env!("CARGO_MANIFEST_DIR")).join("../images/sample-dental-radiograph.dcm");
        assert!(
            sample.is_file(),
            "sample fixture missing: {}",
            sample.display()
        );
        sample
    }

    #[test]
    fn load_preview_matches_default_render_plan() {
        let sample = sample_dicom_path();

        let preview = load_preview(&sample).expect("load preview");
        let source = load_source_study(&sample).expect("load source study");
        let rendered = render_source_image(&source.image, &RenderPlan::default());

        assert_eq!(preview, rendered);
    }
}
