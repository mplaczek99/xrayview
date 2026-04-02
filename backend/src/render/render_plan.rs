use rayon::prelude::*;

use crate::preview::{PreviewFormat, PreviewImage};
use crate::study::source_image::SourceImage;

use super::windowing::{WindowMode, WindowTransform, map_linear};

#[derive(Debug, Clone, Copy, PartialEq, Default)]
pub struct RenderPlan {
    pub window: WindowMode,
}

pub fn render_source_image(source: &SourceImage, plan: &RenderPlan) -> PreviewImage {
    PreviewImage {
        width: source.width,
        height: source.height,
        pixels: render_grayscale_pixels(source, plan),
        format: PreviewFormat::Gray8,
    }
}

pub fn render_grayscale_pixels(source: &SourceImage, plan: &RenderPlan) -> Vec<u8> {
    let mut pixels = vec![0_u8; source.pixels.len()];
    let window = resolve_window(source, *plan);
    let min_value = source.min_value;
    let max_value = source.max_value;
    let invert = source.invert;

    pixels
        .par_iter_mut()
        .zip(source.pixels.par_iter().copied())
        .for_each(|(dst, value)| {
            let mut byte = match window {
                Some(transform) => transform.map(value),
                None => map_linear(value, min_value, max_value),
            };
            if invert {
                byte = 255 - byte;
            }
            *dst = byte;
        });

    pixels
}

fn resolve_window(source: &SourceImage, plan: RenderPlan) -> Option<WindowTransform> {
    match plan.window {
        WindowMode::Default => source.default_window.and_then(WindowTransform::from_window),
        WindowMode::FullRange => None,
        WindowMode::Manual(window) => WindowTransform::from_window(window),
    }
}

#[cfg(test)]
mod tests {
    use crate::render::windowing::{WindowLevel, WindowMode};
    use crate::study::source_image::SourceImage;

    use super::{RenderPlan, render_grayscale_pixels};

    #[test]
    fn default_plan_uses_embedded_window_when_available() {
        let source = SourceImage {
            width: 3,
            height: 1,
            pixels: vec![0.0, 127.5, 255.0],
            min_value: 0.0,
            max_value: 255.0,
            default_window: Some(WindowLevel {
                center: 128.0,
                width: 256.0,
            }),
            invert: false,
        };

        assert_eq!(
            render_grayscale_pixels(&source, &RenderPlan::default()),
            vec![0, 128, 255]
        );
    }

    #[test]
    fn full_range_plan_ignores_embedded_window() {
        let source = SourceImage {
            width: 3,
            height: 1,
            pixels: vec![0.0, 64.0, 128.0],
            min_value: 0.0,
            max_value: 128.0,
            default_window: Some(WindowLevel {
                center: 32.0,
                width: 64.0,
            }),
            invert: false,
        };

        assert_eq!(
            render_grayscale_pixels(
                &source,
                &RenderPlan {
                    window: WindowMode::FullRange,
                }
            ),
            vec![0, 128, 255]
        );
    }
}
