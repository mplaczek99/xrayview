use std::collections::VecDeque;

use anyhow::{Context, Result, bail};
use image::GrayImage;
use serde::Serialize;

use crate::MeasurementScale;
use crate::preview::{PreviewFormat, PreviewImage};

const PIXEL_UNITS: &str = "px";
const MILLIMETER_UNITS: &str = "mm";

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ToothAnalysis {
    pub image: ToothImageMetadata,
    pub calibration: ToothCalibration,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub tooth: Option<ToothCandidate>,
    pub warnings: Vec<String>,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ToothImageMetadata {
    pub width: u32,
    pub height: u32,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ToothCalibration {
    pub pixel_units: &'static str,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub measurement_scale: Option<MeasurementScale>,
    pub real_world_measurements_available: bool,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ToothCandidate {
    pub confidence: f64,
    pub mask_area_pixels: u32,
    pub measurements: ToothMeasurementBundle,
    pub geometry: ToothGeometry,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ToothMeasurementBundle {
    pub pixel: ToothMeasurementValues,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub calibrated: Option<ToothMeasurementValues>,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ToothMeasurementValues {
    pub tooth_width: f64,
    pub tooth_height: f64,
    pub bounding_box_width: f64,
    pub bounding_box_height: f64,
    pub units: &'static str,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ToothGeometry {
    pub bounding_box: BoundingBox,
    pub width_line: LineSegment,
    pub height_line: LineSegment,
}

#[derive(Debug, Clone, Copy, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct BoundingBox {
    pub x: u32,
    pub y: u32,
    pub width: u32,
    pub height: u32,
}

#[derive(Debug, Clone, Copy, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct LineSegment {
    pub start: Point,
    pub end: Point,
}

#[derive(Debug, Clone, Copy, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Point {
    pub x: u32,
    pub y: u32,
}

#[derive(Debug, Clone, Copy)]
struct SearchRegion {
    x: u32,
    y: u32,
    width: u32,
    height: u32,
}

impl SearchRegion {
    fn area(self) -> u32 {
        self.width.saturating_mul(self.height)
    }
}

#[derive(Debug, Clone)]
struct ComponentCandidate {
    pixels: Vec<usize>,
    bbox: BoundingBox,
    area: u32,
    score: f64,
    strict: bool,
}

pub fn analyze_preview(
    preview: &PreviewImage,
    measurement_scale: Option<MeasurementScale>,
) -> Result<ToothAnalysis> {
    if preview.format != PreviewFormat::Gray8 {
        bail!("tooth analysis currently requires an 8-bit grayscale preview");
    }

    let width = preview.width;
    let height = preview.height;
    let search = default_search_region(width, height);
    let normalized = normalize_pixels(&preview.pixels);
    let normalized_image = GrayImage::from_raw(width, height, normalized.clone())
        .context("construct normalized grayscale preview")?;
    let small_blur = image::imageops::blur(&normalized_image, 1.4);
    let large_blur = image::imageops::blur(&normalized_image, 9.0);
    let toothness = build_toothness_map(&normalized, &small_blur, &large_blur, width, height);

    let toothness_threshold = percentile_in_region(&toothness, width, search, 0.79).max(118);
    let intensity_threshold = percentile_in_region(&normalized, width, search, 0.69).max(82);

    let mut mask = vec![false; normalized.len()];
    for y in search.y..search.y + search.height {
        let row_start = (y * width) as usize;
        for x in search.x..search.x + search.width {
            let index = row_start + x as usize;
            mask[index] =
                toothness[index] >= toothness_threshold && normalized[index] >= intensity_threshold;
        }
    }

    let mask = open_binary_mask(
        &close_binary_mask(&mask, width as usize, height as usize),
        width as usize,
        height as usize,
    );

    let candidates = collect_candidates(&mask, &normalized, &toothness, width, height, search);
    let selected = select_best_candidate(&candidates, true)
        .or_else(|| select_best_candidate(&candidates, false));

    let mut warnings = Vec::new();
    if measurement_scale.is_none() {
        warnings
            .push("Calibration metadata unavailable; returning pixel measurements only.".into());
    }

    let tooth = selected.map(|candidate| {
        if !candidate.strict {
            warnings.push(
                "No component met the primary tooth filters; using the strongest relaxed candidate."
                    .into(),
            );
        }

        build_tooth_candidate(candidate, measurement_scale, width)
    });

    if tooth.is_none() {
        warnings.push("The backend could not isolate a tooth candidate from this study.".into());
    }

    Ok(ToothAnalysis {
        image: ToothImageMetadata { width, height },
        calibration: ToothCalibration {
            pixel_units: PIXEL_UNITS,
            measurement_scale,
            real_world_measurements_available: measurement_scale.is_some(),
        },
        tooth,
        warnings,
    })
}

fn build_tooth_candidate(
    candidate: &ComponentCandidate,
    measurement_scale: Option<MeasurementScale>,
    image_width: u32,
) -> ToothCandidate {
    let geometry = geometry_from_pixels(&candidate.pixels, candidate.bbox, image_width);
    let pixel = ToothMeasurementValues {
        tooth_width: geometry.width_line.length() as f64,
        tooth_height: geometry.height_line.length() as f64,
        bounding_box_width: candidate.bbox.width as f64,
        bounding_box_height: candidate.bbox.height as f64,
        units: PIXEL_UNITS,
    };
    let calibrated = measurement_scale.map(|scale| ToothMeasurementValues {
        tooth_width: round_measurement(pixel.tooth_width * scale.column_spacing_mm),
        tooth_height: round_measurement(pixel.tooth_height * scale.row_spacing_mm),
        bounding_box_width: round_measurement(pixel.bounding_box_width * scale.column_spacing_mm),
        bounding_box_height: round_measurement(pixel.bounding_box_height * scale.row_spacing_mm),
        units: MILLIMETER_UNITS,
    });

    ToothCandidate {
        confidence: round_confidence(candidate.score),
        mask_area_pixels: candidate.area,
        measurements: ToothMeasurementBundle { pixel, calibrated },
        geometry,
    }
}

fn default_search_region(width: u32, height: u32) -> SearchRegion {
    let x_margin = (width / 8).max(8);
    let top_margin = ((height as f64 * 0.20).round() as u32).max(8);
    let bottom = ((height as f64 * 0.78).round() as u32).max(top_margin + 1);
    SearchRegion {
        x: x_margin,
        y: top_margin,
        width: width.saturating_sub(x_margin * 2).max(1),
        height: bottom.saturating_sub(top_margin).max(1),
    }
}

fn normalize_pixels(pixels: &[u8]) -> Vec<u8> {
    let mut histogram = [0_u32; 256];
    for value in pixels.iter().copied() {
        histogram[value as usize] += 1;
    }

    let total = pixels.len() as u32;
    let lower = histogram_percentile(&histogram, total, 0.02);
    let upper = histogram_percentile(&histogram, total, 0.98);
    if upper <= lower {
        return pixels.to_vec();
    }

    pixels
        .iter()
        .copied()
        .map(|value| {
            if value <= lower {
                0
            } else if value >= upper {
                255
            } else {
                let scaled = (u32::from(value - lower) * 255) / u32::from(upper - lower);
                scaled as u8
            }
        })
        .collect()
}

fn build_toothness_map(
    normalized: &[u8],
    small_blur: &GrayImage,
    large_blur: &GrayImage,
    width: u32,
    height: u32,
) -> Vec<u8> {
    let mut toothness = vec![0_u8; normalized.len()];
    for y in 0..height {
        for x in 0..width {
            let index = (y * width + x) as usize;
            let small = i16::from(small_blur.get_pixel(x, y)[0]);
            let large = i16::from(large_blur.get_pixel(x, y)[0]);
            let local_contrast = (128 + small - large).clamp(0, 255) as u8;
            let gradient = local_gradient(normalized, width, height, x, y);
            let combined = (u16::from(normalized[index]) * 5
                + u16::from(local_contrast) * 4
                + u16::from(gradient) * 2)
                / 11;
            toothness[index] = combined as u8;
        }
    }

    toothness
}

fn local_gradient(pixels: &[u8], width: u32, height: u32, x: u32, y: u32) -> u8 {
    let left = pixels[(y * width + x.saturating_sub(1)) as usize];
    let right = pixels[(y * width + (x + 1).min(width - 1)) as usize];
    let top = pixels[(y.saturating_sub(1) * width + x) as usize];
    let bottom = pixels[((y + 1).min(height - 1) * width + x) as usize];
    let horizontal = (i16::from(right) - i16::from(left)).unsigned_abs();
    let vertical = (i16::from(bottom) - i16::from(top)).unsigned_abs();
    (horizontal + vertical).min(255) as u8
}

fn percentile_in_region(values: &[u8], width: u32, region: SearchRegion, percentile: f64) -> u8 {
    let mut histogram = [0_u32; 256];
    let mut total = 0_u32;
    for y in region.y..region.y + region.height {
        let row_start = (y * width) as usize;
        for x in region.x..region.x + region.width {
            histogram[values[row_start + x as usize] as usize] += 1;
            total += 1;
        }
    }

    histogram_percentile(&histogram, total, percentile)
}

fn histogram_percentile(histogram: &[u32; 256], total: u32, percentile: f64) -> u8 {
    if total == 0 {
        return 0;
    }

    let target = ((f64::from(total.saturating_sub(1)) * percentile).round() as u32) + 1;
    let mut cumulative = 0_u32;
    for (value, count) in histogram.iter().copied().enumerate() {
        cumulative += count;
        if cumulative >= target {
            return value as u8;
        }
    }

    255
}

fn close_binary_mask(mask: &[bool], width: usize, height: usize) -> Vec<bool> {
    erode_binary_mask(&dilate_binary_mask(mask, width, height), width, height)
}

fn open_binary_mask(mask: &[bool], width: usize, height: usize) -> Vec<bool> {
    dilate_binary_mask(&erode_binary_mask(mask, width, height), width, height)
}

fn dilate_binary_mask(mask: &[bool], width: usize, height: usize) -> Vec<bool> {
    let mut dilated = vec![false; mask.len()];
    for y in 0..height {
        for x in 0..width {
            let mut value = false;
            for ny in y.saturating_sub(1)..=(y + 1).min(height - 1) {
                for nx in x.saturating_sub(1)..=(x + 1).min(width - 1) {
                    if mask[ny * width + nx] {
                        value = true;
                        break;
                    }
                }
                if value {
                    break;
                }
            }
            dilated[y * width + x] = value;
        }
    }

    dilated
}

fn erode_binary_mask(mask: &[bool], width: usize, height: usize) -> Vec<bool> {
    let mut eroded = vec![false; mask.len()];
    for y in 0..height {
        for x in 0..width {
            let mut value = true;
            for ny in y.saturating_sub(1)..=(y + 1).min(height - 1) {
                for nx in x.saturating_sub(1)..=(x + 1).min(width - 1) {
                    if !mask[ny * width + nx] {
                        value = false;
                        break;
                    }
                }
                if !value {
                    break;
                }
            }
            eroded[y * width + x] = value;
        }
    }

    eroded
}

fn collect_candidates(
    mask: &[bool],
    normalized: &[u8],
    toothness: &[u8],
    width: u32,
    height: u32,
    search: SearchRegion,
) -> Vec<ComponentCandidate> {
    let width_usize = width as usize;
    let mut visited = vec![false; mask.len()];
    let mut queue = VecDeque::new();
    let mut candidates = Vec::new();

    for y in search.y as usize..(search.y + search.height) as usize {
        for x in search.x as usize..(search.x + search.width) as usize {
            let start_index = y * width_usize + x;
            if visited[start_index] || !mask[start_index] {
                continue;
            }

            visited[start_index] = true;
            queue.push_back(start_index);

            let mut pixels = Vec::new();
            let mut min_x = x as u32;
            let mut max_x = x as u32;
            let mut min_y = y as u32;
            let mut max_y = y as u32;
            let mut intensity_sum = 0_u64;
            let mut toothness_sum = 0_u64;

            while let Some(index) = queue.pop_front() {
                pixels.push(index);
                let px = (index % width_usize) as u32;
                let py = (index / width_usize) as u32;
                min_x = min_x.min(px);
                max_x = max_x.max(px);
                min_y = min_y.min(py);
                max_y = max_y.max(py);
                intensity_sum += u64::from(normalized[index]);
                toothness_sum += u64::from(toothness[index]);

                for ny in py.saturating_sub(1) as usize..=((py + 1).min(height - 1)) as usize {
                    for nx in px.saturating_sub(1) as usize..=((px + 1).min(width - 1)) as usize {
                        let neighbor = ny * width_usize + nx;
                        if !visited[neighbor] && mask[neighbor] {
                            visited[neighbor] = true;
                            queue.push_back(neighbor);
                        }
                    }
                }
            }

            let area = pixels.len() as u32;
            if area == 0 {
                continue;
            }

            let bbox = BoundingBox {
                x: min_x,
                y: min_y,
                width: max_x - min_x + 1,
                height: max_y - min_y + 1,
            };
            let mean_intensity = intensity_sum as f64 / f64::from(area);
            let mean_toothness = toothness_sum as f64 / f64::from(area);
            let strict = is_strict_tooth_candidate(area, bbox, search);
            let score = score_candidate(area, bbox, search, mean_intensity, mean_toothness, strict);

            candidates.push(ComponentCandidate {
                pixels,
                bbox,
                area,
                score,
                strict,
            });
        }
    }

    candidates
}

fn is_strict_tooth_candidate(area: u32, bbox: BoundingBox, search: SearchRegion) -> bool {
    let area_ratio = f64::from(area) / f64::from(search.area().max(1));
    let width_ratio = f64::from(bbox.width) / f64::from(search.width.max(1));
    let height_ratio = f64::from(bbox.height) / f64::from(search.height.max(1));
    let aspect_ratio = f64::from(bbox.height) / f64::from(bbox.width.max(1));

    area_ratio >= 0.001
        && area_ratio <= 0.035
        && width_ratio >= 0.02
        && width_ratio <= 0.16
        && height_ratio >= 0.12
        && height_ratio <= 0.68
        && aspect_ratio >= 0.8
        && aspect_ratio <= 4.5
}

fn score_candidate(
    area: u32,
    bbox: BoundingBox,
    search: SearchRegion,
    mean_intensity: f64,
    mean_toothness: f64,
    strict: bool,
) -> f64 {
    let search_area = f64::from(search.area().max(1));
    let area_score = clamp01(f64::from(area) / (search_area * 0.02));
    let height_score = clamp01(f64::from(bbox.height) / (f64::from(search.height) * 0.46));
    let width_ratio = f64::from(bbox.width) / f64::from(search.width.max(1));
    let width_score = 1.0 - ((width_ratio - 0.08).abs() / 0.08).min(1.0);
    let aspect_ratio = f64::from(bbox.height) / f64::from(bbox.width.max(1));
    let aspect_score = 1.0 - ((aspect_ratio - 1.9).abs() / 1.9).min(1.0);
    let fill_ratio = f64::from(area) / f64::from(bbox.width.saturating_mul(bbox.height).max(1));
    let fill_score = 1.0 - ((fill_ratio - 0.42).abs() / 0.42).min(1.0);
    let mean_score = clamp01((mean_intensity - 110.0) / 120.0);
    let toothness_score = clamp01((mean_toothness - 120.0) / 100.0);
    let center_x = f64::from(bbox.x) + f64::from(bbox.width) / 2.0;
    let search_center_x = f64::from(search.x) + f64::from(search.width) / 2.0;
    let center_score =
        1.0 - ((center_x - search_center_x).abs() / (f64::from(search.width) / 2.0)).min(1.0);

    let mut score = 0.17 * area_score
        + 0.17 * height_score
        + 0.15 * aspect_score
        + 0.10 * width_score
        + 0.09 * fill_score
        + 0.06 * mean_score
        + 0.06 * toothness_score
        + 0.20 * center_score;

    if !strict {
        score *= 0.86;
    }

    score
}

fn select_best_candidate(
    candidates: &[ComponentCandidate],
    strict_only: bool,
) -> Option<&ComponentCandidate> {
    candidates
        .iter()
        .filter(|candidate| !strict_only || candidate.strict)
        .filter(|candidate| candidate.area > 150)
        .max_by(|left, right| left.score.total_cmp(&right.score))
}

fn geometry_from_pixels(pixels: &[usize], bbox: BoundingBox, image_width: u32) -> ToothGeometry {
    let bbox_width = bbox.width as usize;
    let bbox_height = bbox.height as usize;
    let mut row_min = vec![u32::MAX; bbox_height];
    let mut row_max = vec![0_u32; bbox_height];
    let mut row_seen = vec![false; bbox_height];
    let mut col_min = vec![u32::MAX; bbox_width];
    let mut col_max = vec![0_u32; bbox_width];
    let mut col_seen = vec![false; bbox_width];

    for index in pixels.iter().copied() {
        let x = (index as u32) % image_width;
        let y = (index as u32) / image_width;
        let local_x = (x - bbox.x) as usize;
        let local_y = (y - bbox.y) as usize;
        row_min[local_y] = row_min[local_y].min(x);
        row_max[local_y] = row_max[local_y].max(x);
        row_seen[local_y] = true;
        col_min[local_x] = col_min[local_x].min(y);
        col_max[local_x] = col_max[local_x].max(y);
        col_seen[local_x] = true;
    }

    let mut width_line = LineSegment {
        start: Point {
            x: bbox.x,
            y: bbox.y,
        },
        end: Point {
            x: bbox.x + bbox.width.saturating_sub(1),
            y: bbox.y,
        },
    };
    let mut best_width = 0_u32;
    for (offset, seen) in row_seen.iter().copied().enumerate() {
        if !seen {
            continue;
        }
        let span = row_max[offset] - row_min[offset] + 1;
        if span > best_width {
            best_width = span;
            width_line = LineSegment {
                start: Point {
                    x: row_min[offset],
                    y: bbox.y + offset as u32,
                },
                end: Point {
                    x: row_max[offset],
                    y: bbox.y + offset as u32,
                },
            };
        }
    }

    let mut height_line = LineSegment {
        start: Point {
            x: bbox.x,
            y: bbox.y,
        },
        end: Point {
            x: bbox.x,
            y: bbox.y + bbox.height.saturating_sub(1),
        },
    };
    let mut best_height = 0_u32;
    for (offset, seen) in col_seen.iter().copied().enumerate() {
        if !seen {
            continue;
        }
        let span = col_max[offset] - col_min[offset] + 1;
        if span > best_height {
            best_height = span;
            height_line = LineSegment {
                start: Point {
                    x: bbox.x + offset as u32,
                    y: col_min[offset],
                },
                end: Point {
                    x: bbox.x + offset as u32,
                    y: col_max[offset],
                },
            };
        }
    }

    ToothGeometry {
        bounding_box: bbox,
        width_line,
        height_line,
    }
}

impl LineSegment {
    fn length(self) -> u32 {
        if self.start.y == self.end.y {
            self.end.x.saturating_sub(self.start.x) + 1
        } else {
            self.end.y.saturating_sub(self.start.y) + 1
        }
    }
}

fn round_measurement(value: f64) -> f64 {
    (value * 10.0).round() / 10.0
}

fn round_confidence(value: f64) -> f64 {
    ((value.clamp(0.0, 0.99)) * 100.0).round() / 100.0
}

fn clamp01(value: f64) -> f64 {
    value.clamp(0.0, 1.0)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::preview::load_preview;
    use std::path::Path;

    fn synthetic_tooth_preview() -> PreviewImage {
        let width = 240_u32;
        let height = 160_u32;
        let mut pixels = vec![24_u8; (width * height) as usize];

        for y in 24..130 {
            for x in 14..226 {
                pixels[(y * width + x) as usize] = 54;
            }
        }

        fill_rect(&mut pixels, width, 38, 54, 34, 34, 174);
        fill_triangle_root(&mut pixels, width, 38, 88, 62, 32, 174);

        fill_rect(&mut pixels, width, 100, 42, 42, 38, 236);
        fill_triangle_root(&mut pixels, width, 100, 80, 92, 54, 236);

        fill_rect(&mut pixels, width, 172, 56, 28, 32, 160);
        fill_triangle_root(&mut pixels, width, 172, 88, 50, 30, 160);

        PreviewImage {
            width,
            height,
            pixels,
            format: PreviewFormat::Gray8,
        }
    }

    fn fill_rect(
        pixels: &mut [u8],
        width: u32,
        x: u32,
        y: u32,
        rect_width: u32,
        rect_height: u32,
        value: u8,
    ) {
        for yy in y..y + rect_height {
            for xx in x..x + rect_width {
                pixels[(yy * width + xx) as usize] = value;
            }
        }
    }

    fn fill_triangle_root(
        pixels: &mut [u8],
        width: u32,
        x: u32,
        y: u32,
        root_width: u32,
        root_height: u32,
        value: u8,
    ) {
        let center_x = x + root_width / 2;
        for offset in 0..root_height {
            let row_y = y + offset;
            let span = root_width.saturating_sub((offset * root_width) / root_height);
            let half_span = span / 2;
            let start_x = center_x.saturating_sub(half_span);
            let end_x = (center_x + half_span).min(width - 1);
            for xx in start_x..=end_x {
                pixels[(row_y * width + xx) as usize] = value;
            }
        }
    }

    #[test]
    fn synthetic_tooth_prefers_tall_central_candidate() {
        let analysis = analyze_preview(&synthetic_tooth_preview(), None).expect("analyze preview");
        let tooth = analysis.tooth.expect("tooth candidate");

        assert!(tooth.confidence >= 0.5);
        assert!(tooth.geometry.bounding_box.x > 80);
        assert!(tooth.geometry.bounding_box.x < 130);
        assert!(tooth.measurements.pixel.tooth_height > tooth.measurements.pixel.tooth_width);
    }

    #[test]
    fn calibration_generates_millimeter_measurements() {
        let analysis = analyze_preview(
            &synthetic_tooth_preview(),
            Some(MeasurementScale {
                row_spacing_mm: 0.2,
                column_spacing_mm: 0.3,
                source: "PixelSpacing",
            }),
        )
        .expect("analyze preview");
        let tooth = analysis.tooth.expect("tooth candidate");
        let calibrated = tooth.measurements.calibrated.expect("mm measurements");

        assert_eq!(calibrated.units, MILLIMETER_UNITS);
        assert!(calibrated.tooth_width > 0.0);
        assert!(calibrated.tooth_height > 0.0);
    }

    #[test]
    fn sample_dicom_yields_a_candidate_or_structured_warning() {
        let sample =
            Path::new(env!("CARGO_MANIFEST_DIR")).join("../images/sample-dental-radiograph.dcm");
        let preview = load_preview(&sample).expect("load sample preview");
        let analysis = analyze_preview(&preview, None).expect("analyze sample preview");

        assert_eq!(analysis.image.width, 2048);
        assert_eq!(analysis.image.height, 1088);
        assert!(
            analysis.tooth.is_some() || !analysis.warnings.is_empty(),
            "analysis should produce either a candidate or a structured warning"
        );
    }
}
