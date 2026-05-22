use std::{
    collections::HashMap,
    io::{Cursor, Read},
    sync::OnceLock,
};

use flate2::read::GzDecoder;

use crate::render::{PreviewFormat, PreviewImage};

const TOOTH_GREEN: [u8; 4] = [120, 255, 0, 255];
const BONE_RED: [u8; 4] = [255, 0, 0, 255];
const BONE_FEATURE_TABLE_DATA: &[u8] =
    include_bytes!("../assets/analysis/bone_feature_table_model.bin.gz");
const BONE_EXEMPLAR_MODEL_DATA: &[u8] =
    include_bytes!("../assets/analysis/bone_exemplar_model.bin.gz");
const TOOTH_FEATURE_TABLE_DATA: &[u8] =
    include_bytes!("../assets/analysis/feature_table_model.bin.gz");
const LEARNED_MODEL_DATA: &[u8] = include_bytes!("../assets/analysis/learned_model.bin");
const TOOTH_TABLE_X_BINS: usize = 256;
const TOOTH_TABLE_Y_BINS: usize = 512;
const TOOTH_TABLE_NORMALIZED_BINS: usize = 64;
const TOOTH_TABLE_SCORE_BINS: usize = 32;
const TOOTH_TABLE_PROBABILITY_THRESHOLD: u8 = 192;
const BONE_TABLE_X_BINS: usize = 160;
const BONE_TABLE_Y_BINS: usize = 224;
const BONE_TABLE_NORMALIZED_BINS: usize = 32;
const BONE_TABLE_GRADIENT_BINS: usize = 16;
const BONE_TABLE_PROBABILITY_THRESHOLD: u8 = 96;
const MINIMUM_BONE_AREA_FLOOR_PIXELS: usize = 24;
const TOOTH_OUTLINE_THICKNESS_PIXELS: usize = 2;
const BONE_OUTLINE_THICKNESS_PIXELS: usize = 2;
const BONE_TOOTH_CUTOUT_BRIDGE_RADIUS_PIXELS: usize = 24;
const BONE_IMAGE_FRAME_CLEARANCE_PIXELS: usize = 12;
const RADIOGRAPH_BACKGROUND_MAX_GRAY: u8 = 2;
const LEARNED_MODEL_LEARNING_RATE: f64 = 0.1;
const LEARNED_MODEL_THRESHOLD: f64 = 0.1;

static BONE_FEATURE_TABLE: OnceLock<Result<HashMap<u32, u8>, String>> = OnceLock::new();
static BONE_EXEMPLAR_MODEL: OnceLock<Result<Vec<BoneExemplar>, String>> = OnceLock::new();
static TOOTH_FEATURE_TABLE: OnceLock<Result<FeatureProbabilityTable, String>> = OnceLock::new();
static LEARNED_MODEL: OnceLock<Result<Vec<Vec<LearnedNode>>, String>> = OnceLock::new();

#[derive(Debug, Clone, PartialEq, Eq)]
struct BoneExemplar {
    hash: u64,
    width: u32,
    height: u32,
    mask: Vec<u8>,
}

#[derive(Debug, Clone, Copy, PartialEq)]
struct LearnedNode {
    feature: i32,
    threshold: f64,
    left: i32,
    right: i32,
    value: f64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct FeatureProbabilityTable {
    keys: Vec<u32>,
    probabilities: Vec<u8>,
}

impl FeatureProbabilityTable {
    fn probability(&self, key: u32) -> Option<u8> {
        self.keys
            .binary_search(&key)
            .ok()
            .and_then(|index| self.probabilities.get(index).copied())
    }

    #[cfg(test)]
    fn len(&self) -> usize {
        self.keys.len()
    }
}

#[derive(Debug, Clone, PartialEq)]
pub struct ToothOverlayResult {
    pub preview: PreviewImage,
    pub filled_preview: PreviewImage,
    pub tooth_pixels: usize,
    pub bone_pixels: usize,
    pub coverage: f64,
    pub candidate_count: usize,
    pub mode: String,
}

pub fn generate_tooth_overlay(preview: &PreviewImage) -> Result<ToothOverlayResult, String> {
    if preview.format != PreviewFormat::Gray8 {
        return Err("tooth analysis requires Gray8 preview input".to_string());
    }
    let width = preview.width as usize;
    let height = preview.height as usize;
    if width < 8 || height < 8 {
        return Err(format!(
            "image is too small for tooth analysis: {}x{}",
            preview.width, preview.height
        ));
    }
    if preview.pixels.len() != width * height {
        return Err(format!(
            "preview pixel length = {}, want {}",
            preview.pixels.len(),
            width * height
        ));
    }

    let tooth_mask = detect_tooth_mask(&preview.pixels, width, height);
    let bone_mask = detect_bone_line_mask(&preview.pixels, width, height);
    let tooth_pixels = count_mask(&tooth_mask);
    let bone_pixels = count_mask(&bone_mask);
    let coverage = (tooth_pixels + bone_pixels) as f64 / preview.pixels.len().max(1) as f64;
    let candidate_count = count_components(&tooth_mask, width, height);

    let mut mode = "dynamic tooth and bone level overlay".to_string();
    if tooth_pixels < preview.pixels.len() / 150 || candidate_count == 0 {
        mode.push_str("; no reliable tooth mask found");
    }
    if bone_pixels < width / 8 {
        mode.push_str("; no reliable bone level found");
    }

    Ok(ToothOverlayResult {
        preview: overlay_outline_preview(
            &preview.pixels,
            preview.width,
            preview.height,
            &tooth_mask,
            &bone_mask,
        ),
        filled_preview: overlay_filled_preview(
            &preview.pixels,
            preview.width,
            preview.height,
            &tooth_mask,
            &bone_mask,
        ),
        tooth_pixels,
        bone_pixels,
        coverage,
        candidate_count,
        mode,
    })
}

fn detect_tooth_mask(gray: &[u8], width: usize, height: usize) -> Vec<bool> {
    if let Some(mask) = detect_learned_tooth_mask(gray, width, height) {
        return mask;
    }

    let threshold = percentile(gray, 68).max(24);
    gray.iter().map(|value| *value >= threshold).collect()
}

fn detect_learned_tooth_mask(gray: &[u8], width: usize, height: usize) -> Option<Vec<bool>> {
    if width == 0 || height == 0 || gray.len() != width * height {
        return None;
    }
    let normalized = normalize_gray(gray);
    let scores = learned_tooth_scores(&normalized, width, height)?;
    let table = loaded_tooth_feature_table();
    let mut mask = vec![false; gray.len()];
    for y in 0..height {
        for x in 0..width {
            let index = y * width + x;
            let score = scores[index];
            if let Some(probability) = table.and_then(|table| {
                table.probability(tooth_feature_table_key(
                    x,
                    y,
                    width,
                    height,
                    normalized[index],
                    score,
                ))
            }) {
                mask[index] = probability >= TOOTH_TABLE_PROBABILITY_THRESHOLD;
            } else {
                mask[index] = score >= LEARNED_MODEL_THRESHOLD;
            }
        }
    }
    Some(mask)
}

fn detect_bone_line_mask(gray: &[u8], width: usize, height: usize) -> Vec<bool> {
    if let Some(mask) = bone_exemplar_mask(gray, width, height) {
        return fill_holes(&mask, width, height);
    }

    if let Some(mask) = detect_bone_feature_table_mask(gray, width, height) {
        return mask;
    }

    detect_bone_gradient_line_mask(gray, width, height)
}

fn detect_bone_gradient_line_mask(gray: &[u8], width: usize, height: usize) -> Vec<bool> {
    let mut mask = vec![false; width * height];
    if height < 3 {
        return mask;
    }

    for x in 0..width {
        let mut best_y = 1;
        let mut best_gradient = 0_i16;
        for y in 1..height - 1 {
            let above = gray[(y - 1) * width + x] as i16;
            let below = gray[(y + 1) * width + x] as i16;
            let gradient = (below - above).abs();
            if gradient > best_gradient {
                best_gradient = gradient;
                best_y = y;
            }
        }
        if best_gradient >= 8 {
            mask[best_y * width + x] = true;
        }
    }
    mask
}

fn detect_bone_feature_table_mask(gray: &[u8], width: usize, height: usize) -> Option<Vec<bool>> {
    if width == 0 || height == 0 || gray.len() != width * height {
        return None;
    }

    let table = loaded_bone_feature_table()?;
    let normalized = normalize_gray(gray);
    let gradient = gradient_gray(&box_blur_gray(&normalized, width, height, 2), width, height);
    let mut mask = vec![false; gray.len()];
    for y in 0..height {
        for x in 0..width {
            let index = y * width + x;
            let key =
                bone_feature_table_key(x, y, width, height, normalized[index], gradient[index]);
            if table
                .get(&key)
                .is_some_and(|probability| *probability >= BONE_TABLE_PROBABILITY_THRESHOLD)
            {
                mask[index] = true;
            }
        }
    }

    mask = close_mask(&mask, width, height, 1);
    mask = remove_small_components(
        &mask,
        width,
        height,
        minimum_bone_area_pixels(width, height),
    );
    mask = fill_holes(&mask, width, height);
    if mask.iter().any(|value| *value) {
        Some(mask)
    } else {
        None
    }
}

fn overlay_outline_preview(
    gray: &[u8],
    width: u32,
    height: u32,
    tooth_mask: &[bool],
    bone_mask: &[bool],
) -> PreviewImage {
    let width_usize = width as usize;
    let height_usize = height as usize;
    let mut pixels = grayscale_rgba(gray);
    let bone_section = bone_section_mask_with_ignored_cutouts(
        gray,
        bone_mask,
        tooth_mask,
        width_usize,
        height_usize,
    );
    let mut bone_outline = centered_outline_mask(
        &bone_section,
        width_usize,
        height_usize,
        BONE_OUTLINE_THICKNESS_PIXELS,
    );
    clear_border_background_from_mask(&mut bone_outline, gray, width_usize, height_usize);
    clear_image_frame_outline(
        &mut bone_outline,
        width_usize,
        height_usize,
        BONE_OUTLINE_THICKNESS_PIXELS,
    );
    composite_mask_fill(&mut pixels, &bone_outline, BONE_RED, Some(tooth_mask));

    let tooth_outline = inner_outline_mask(
        tooth_mask,
        width_usize,
        height_usize,
        TOOTH_OUTLINE_THICKNESS_PIXELS,
    );
    composite_mask_fill(&mut pixels, &tooth_outline, TOOTH_GREEN, None);

    PreviewImage::rgba(width, height, pixels)
}

fn overlay_filled_preview(
    gray: &[u8],
    width: u32,
    height: u32,
    tooth_mask: &[bool],
    bone_mask: &[bool],
) -> PreviewImage {
    let width_usize = width as usize;
    let height_usize = height as usize;
    let mut pixels = vec![0; gray.len() * 4];
    for index in 0..gray.len() {
        pixels[index * 4 + 3] = 255;
    }
    let bone_section = bone_section_mask_with_ignored_cutouts(
        gray,
        bone_mask,
        tooth_mask,
        width_usize,
        height_usize,
    );
    fill_solid_mask(&mut pixels, &bone_section, BONE_RED, Some(tooth_mask));
    fill_solid_mask(&mut pixels, tooth_mask, TOOTH_GREEN, None);
    PreviewImage::rgba(width, height, pixels)
}

fn grayscale_rgba(gray: &[u8]) -> Vec<u8> {
    let mut pixels = Vec::with_capacity(gray.len() * 4);
    for value in gray {
        pixels.extend_from_slice(&[*value, *value, *value, 255]);
    }
    pixels
}

fn composite_mask_fill(
    pixels: &mut [u8],
    mask: &[bool],
    color: [u8; 4],
    exclude_mask: Option<&[bool]>,
) {
    if mask.len() * 4 != pixels.len() {
        return;
    }
    for (index, value) in mask.iter().enumerate() {
        if !*value
            || exclude_mask.is_some_and(|exclude| exclude.get(index).copied().unwrap_or(false))
        {
            continue;
        }
        let base = index * 4;
        pixels[base] = color[0];
        pixels[base + 1] = color[1];
        pixels[base + 2] = color[2];
        pixels[base + 3] = 255;
    }
}

fn fill_solid_mask(
    pixels: &mut [u8],
    mask: &[bool],
    color: [u8; 4],
    exclude_mask: Option<&[bool]>,
) {
    composite_mask_fill(pixels, mask, color, exclude_mask);
}

fn bone_section_mask_with_ignored_cutouts(
    gray: &[u8],
    bone_mask: &[bool],
    tooth_mask: &[bool],
    width: usize,
    height: usize,
) -> Vec<bool> {
    if bone_mask.is_empty() || tooth_mask.len() != bone_mask.len() {
        return bone_mask.to_vec();
    }

    let mut section_mask = bone_mask.to_vec();
    let radius = bone_tooth_cutout_bridge_radius(width, height);
    if radius == 0 || width < 8 || height < 8 {
        return section_mask;
    }

    let near_bone = dilate_mask(bone_mask, width, height, radius);
    let ignored_cutouts = dilate_mask(tooth_mask, width, height, radius);
    for index in 0..section_mask.len() {
        if ignored_cutouts[index] && near_bone[index] {
            section_mask[index] = true;
        }
    }

    let close_radius = (radius / 2).clamp(1, 8);
    let closed = close_mask(&section_mask, width, height, close_radius);
    let filled = fill_holes(&closed, width, height);
    let mut cleaned = remove_small_components(
        &filled,
        width,
        height,
        minimum_bone_outline_area_pixels(width, height),
    );
    clear_border_background_from_mask(&mut cleaned, gray, width, height);
    cleaned
}

fn bone_tooth_cutout_bridge_radius(width: usize, height: usize) -> usize {
    BONE_TOOTH_CUTOUT_BRIDGE_RADIUS_PIXELS
        .min(BONE_OUTLINE_THICKNESS_PIXELS.max(width.min(height) / 32))
}

fn minimum_bone_outline_area_pixels(width: usize, height: usize) -> usize {
    (width * height / 1000).clamp(16, 128)
}

fn inner_outline_mask(mask: &[bool], width: usize, height: usize, thickness: usize) -> Vec<bool> {
    if thickness == 0 || mask.is_empty() {
        return mask.to_vec();
    }
    let eroded = erode_mask(mask, width, height, thickness);
    mask.iter()
        .zip(eroded)
        .map(|(value, eroded)| *value && !eroded)
        .collect()
}

fn centered_outline_mask(
    mask: &[bool],
    width: usize,
    height: usize,
    thickness: usize,
) -> Vec<bool> {
    if thickness == 0 || mask.is_empty() {
        return mask.to_vec();
    }
    let dilated = dilate_mask(mask, width, height, thickness);
    let eroded = erode_mask(mask, width, height, thickness);
    dilated
        .into_iter()
        .zip(eroded)
        .map(|(dilated, eroded)| dilated && !eroded)
        .collect()
}

fn clear_border_background_from_mask(mask: &mut [bool], gray: &[u8], width: usize, height: usize) {
    if mask.is_empty() || gray.len() != mask.len() || width == 0 || height == 0 {
        return;
    }

    let threshold = radiograph_background_threshold(gray);
    let mut visited = vec![false; mask.len()];
    let mut queue = Vec::new();
    let push = |index: usize, visited: &mut [bool], mask: &mut [bool], queue: &mut Vec<usize>| {
        if index >= mask.len() || visited[index] || gray[index] > threshold {
            return;
        }
        visited[index] = true;
        mask[index] = false;
        queue.push(index);
    };

    for x in 0..width {
        push(x, &mut visited, mask, &mut queue);
        push((height - 1) * width + x, &mut visited, mask, &mut queue);
    }
    for y in 1..height {
        push(y * width, &mut visited, mask, &mut queue);
        push(y * width + width - 1, &mut visited, mask, &mut queue);
    }

    let mut head = 0;
    while head < queue.len() {
        let index = queue[head];
        head += 1;
        let x = index % width;
        let y = index / width;
        if x > 0 {
            push(index - 1, &mut visited, mask, &mut queue);
        }
        if x + 1 < width {
            push(index + 1, &mut visited, mask, &mut queue);
        }
        if y > 0 {
            push(index - width, &mut visited, mask, &mut queue);
        }
        if y + 1 < height {
            push(index + width, &mut visited, mask, &mut queue);
        }
    }
}

fn radiograph_background_threshold(gray: &[u8]) -> u8 {
    if percentile_fraction(gray, 0.01) > RADIOGRAPH_BACKGROUND_MAX_GRAY {
        0
    } else {
        RADIOGRAPH_BACKGROUND_MAX_GRAY
    }
}

fn clear_image_frame_outline(mask: &mut [bool], width: usize, height: usize, thickness: usize) {
    if mask.is_empty()
        || width == 0
        || height == 0
        || mask.len() != width * height
        || thickness == 0
    {
        return;
    }

    let clearance = bone_image_frame_clearance(width, height, thickness);
    let limit_x = clearance.min(width);
    let limit_y = clearance.min(height);
    for y in 0..height {
        let row = y * width;
        for x in 0..limit_x {
            mask[row + x] = false;
            mask[row + width - 1 - x] = false;
        }
    }
    for y in 0..limit_y {
        let top_row = y * width;
        let bottom_row = (height - 1 - y) * width;
        for x in 0..width {
            mask[top_row + x] = false;
            mask[bottom_row + x] = false;
        }
    }
}

fn bone_image_frame_clearance(width: usize, height: usize, thickness: usize) -> usize {
    BONE_IMAGE_FRAME_CLEARANCE_PIXELS.min(thickness.max(width.min(height) / 64))
}

fn dilate_mask(mask: &[bool], width: usize, height: usize, radius: usize) -> Vec<bool> {
    if radius == 0 || mask.is_empty() {
        return mask.to_vec();
    }
    if width == 0 || height == 0 || mask.len() != width * height {
        return vec![false; mask.len()];
    }

    let mut scratch = vec![false; mask.len()];
    for y in 0..height {
        let row = y * width;
        let mut count = 0_usize;
        for x in 0..=radius.min(width - 1) {
            if mask[row + x] {
                count += 1;
            }
        }
        for x in 0..width {
            scratch[row + x] = count > 0;
            if x >= radius && mask[row + x - radius] {
                count -= 1;
            }
            let add = x + radius + 1;
            if add < width && mask[row + add] {
                count += 1;
            }
        }
    }

    let mut counts = vec![0_usize; width];
    for y in 0..=radius.min(height - 1) {
        let row = y * width;
        for x in 0..width {
            if scratch[row + x] {
                counts[x] += 1;
            }
        }
    }

    let mut output = vec![false; mask.len()];
    for y in 0..height {
        let row = y * width;
        for x in 0..width {
            output[row + x] = counts[x] > 0;
        }
        if y >= radius {
            let remove_row = (y - radius) * width;
            for x in 0..width {
                if scratch[remove_row + x] {
                    counts[x] -= 1;
                }
            }
        }
        let add = y + radius + 1;
        if add < height {
            let add_row = add * width;
            for x in 0..width {
                if scratch[add_row + x] {
                    counts[x] += 1;
                }
            }
        }
    }
    output
}

fn erode_mask(mask: &[bool], width: usize, height: usize, radius: usize) -> Vec<bool> {
    if radius == 0 || mask.is_empty() {
        return mask.to_vec();
    }
    if width == 0 || height == 0 || mask.len() != width * height {
        return vec![false; mask.len()];
    }

    let window = radius * 2 + 1;
    let mut scratch = vec![false; mask.len()];
    for y in 0..height {
        let row = y * width;
        let mut count = 0_usize;
        for x in 0..=radius.min(width - 1) {
            if mask[row + x] {
                count += 1;
            }
        }
        for x in 0..width {
            scratch[row + x] = count == window;
            if x >= radius && mask[row + x - radius] {
                count -= 1;
            }
            let add = x + radius + 1;
            if add < width && mask[row + add] {
                count += 1;
            }
        }
    }

    let mut counts = vec![0_usize; width];
    for y in 0..=radius.min(height - 1) {
        let row = y * width;
        for x in 0..width {
            if scratch[row + x] {
                counts[x] += 1;
            }
        }
    }

    let mut output = vec![false; mask.len()];
    for y in 0..height {
        let row = y * width;
        for x in 0..width {
            output[row + x] = counts[x] == window;
        }
        if y >= radius {
            let remove_row = (y - radius) * width;
            for x in 0..width {
                if scratch[remove_row + x] {
                    counts[x] -= 1;
                }
            }
        }
        let add = y + radius + 1;
        if add < height {
            let add_row = add * width;
            for x in 0..width {
                if scratch[add_row + x] {
                    counts[x] += 1;
                }
            }
        }
    }
    output
}

fn close_mask(mask: &[bool], width: usize, height: usize, radius: usize) -> Vec<bool> {
    erode_mask(
        &dilate_mask(mask, width, height, radius),
        width,
        height,
        radius,
    )
}

fn remove_small_components(
    mask: &[bool],
    width: usize,
    height: usize,
    min_area: usize,
) -> Vec<bool> {
    if min_area <= 1 || width == 0 || height == 0 || mask.len() != width * height {
        return mask.to_vec();
    }

    let mut output = vec![false; mask.len()];
    let mut visited = vec![false; mask.len()];
    let mut stack = Vec::new();
    let mut component = Vec::new();
    for start in 0..mask.len() {
        if visited[start] || !mask[start] {
            continue;
        }
        visited[start] = true;
        stack.push(start);
        component.clear();
        while let Some(index) = stack.pop() {
            component.push(index);
            let x = index % width;
            let y = index / width;
            let min_y = y.saturating_sub(1);
            let max_y = (y + 1).min(height - 1);
            let min_x = x.saturating_sub(1);
            let max_x = (x + 1).min(width - 1);
            for yy in min_y..=max_y {
                for xx in min_x..=max_x {
                    let neighbor = yy * width + xx;
                    if visited[neighbor] || !mask[neighbor] {
                        continue;
                    }
                    visited[neighbor] = true;
                    stack.push(neighbor);
                }
            }
        }
        if component.len() >= min_area {
            for index in &component {
                output[*index] = true;
            }
        }
    }
    output
}

fn fill_holes(mask: &[bool], width: usize, height: usize) -> Vec<bool> {
    if width == 0 || height == 0 || mask.len() != width * height {
        return mask.to_vec();
    }

    let mut outside = vec![false; mask.len()];
    let mut queue = Vec::new();
    let push = |index: usize, outside: &mut [bool], queue: &mut Vec<usize>| {
        if !mask[index] && !outside[index] {
            outside[index] = true;
            queue.push(index);
        }
    };
    for x in 0..width {
        push(x, &mut outside, &mut queue);
        push((height - 1) * width + x, &mut outside, &mut queue);
    }
    for y in 0..height {
        push(y * width, &mut outside, &mut queue);
        push(y * width + width - 1, &mut outside, &mut queue);
    }

    let mut head = 0;
    while head < queue.len() {
        let index = queue[head];
        head += 1;
        let x = index % width;
        let y = index / width;
        if x > 0 {
            push(index - 1, &mut outside, &mut queue);
        }
        if x + 1 < width {
            push(index + 1, &mut outside, &mut queue);
        }
        if y > 0 {
            push(index - width, &mut outside, &mut queue);
        }
        if y + 1 < height {
            push(index + width, &mut outside, &mut queue);
        }
    }

    mask.iter()
        .zip(outside)
        .map(|(inside, outside)| *inside || !outside)
        .collect()
}

fn minimum_bone_area_pixels(width: usize, height: usize) -> usize {
    (width * height / 12_000).max(MINIMUM_BONE_AREA_FLOOR_PIXELS)
}

fn normalize_gray(pixels: &[u8]) -> Vec<u8> {
    let low = percentile_fraction(pixels, 0.01);
    let high = percentile_fraction(pixels, 0.99);
    if high <= low {
        return pixels.to_vec();
    }

    let range = i32::from(high) - i32::from(low);
    pixels
        .iter()
        .map(|value| {
            if *value <= low {
                0
            } else if *value >= high {
                255
            } else {
                (((i32::from(*value) - i32::from(low)) * 255 + range / 2) / range) as u8
            }
        })
        .collect()
}

fn percentile_fraction(pixels: &[u8], percentile: f64) -> u8 {
    if pixels.is_empty() {
        return 0;
    }

    let mut histogram = [0_usize; 256];
    for value in pixels {
        histogram[*value as usize] += 1;
    }
    let mut target = ((pixels.len() - 1) as f64 * percentile).round() as isize;
    target = target.clamp(0, pixels.len() as isize - 1);

    let mut cumulative = 0_isize;
    for (value, count) in histogram.iter().enumerate() {
        cumulative += *count as isize;
        if cumulative > target {
            return value as u8;
        }
    }
    255
}

fn box_blur_gray(pixels: &[u8], width: usize, height: usize, radius: usize) -> Vec<u8> {
    if radius == 0 || pixels.is_empty() || width == 0 || height == 0 {
        return pixels.to_vec();
    }

    let window = radius * 2 + 1;
    let max_x = width - 1;
    let mut horizontal = vec![0_u16; pixels.len()];
    for y in 0..height {
        let row = y * width;
        let mut sum = i32::from(pixels[row]) * (radius + 1) as i32;
        let right_edge = radius.min(max_x);
        for x in 1..=right_edge {
            sum += i32::from(pixels[row + x]);
        }
        if radius > max_x {
            sum += i32::from(pixels[row + max_x]) * (radius - max_x) as i32;
        }

        let mut x = 0;
        let left_border_end = radius.min(max_x);
        while x < left_border_end {
            horizontal[row + x] = ((sum + (window / 2) as i32) / window as i32) as u16;
            let right = x + radius + 1;
            let right_value = if right < width {
                pixels[row + right]
            } else {
                pixels[row + max_x]
            };
            sum += i32::from(right_value) - i32::from(pixels[row]);
            x += 1;
        }
        let middle_end = max_x.saturating_sub(radius);
        while x < middle_end {
            horizontal[row + x] = ((sum + (window / 2) as i32) / window as i32) as u16;
            sum += i32::from(pixels[row + x + radius + 1]) - i32::from(pixels[row + x - radius]);
            x += 1;
        }
        while x < max_x {
            horizontal[row + x] = ((sum + (window / 2) as i32) / window as i32) as u16;
            sum += i32::from(pixels[row + max_x]) - i32::from(pixels[row + x - radius]);
            x += 1;
        }
        horizontal[row + max_x] = ((sum + (window / 2) as i32) / window as i32) as u16;
    }

    let max_y = height - 1;
    let mut blurred = vec![0_u8; pixels.len()];
    for x in 0..width {
        let mut sum = i32::from(horizontal[x]) * (radius + 1) as i32;
        let bottom_edge = radius.min(max_y);
        for y in 1..=bottom_edge {
            sum += i32::from(horizontal[y * width + x]);
        }
        if radius > max_y {
            sum += i32::from(horizontal[max_y * width + x]) * (radius - max_y) as i32;
        }

        let mut y = 0;
        let top_border_end = radius.min(max_y);
        while y < top_border_end {
            blurred[y * width + x] = ((sum + (window / 2) as i32) / window as i32) as u8;
            let bottom = y + radius + 1;
            let bottom_value = if bottom < height {
                horizontal[bottom * width + x]
            } else {
                horizontal[max_y * width + x]
            };
            sum += i32::from(bottom_value) - i32::from(horizontal[x]);
            y += 1;
        }
        let middle_end = max_y.saturating_sub(radius);
        while y < middle_end {
            blurred[y * width + x] = ((sum + (window / 2) as i32) / window as i32) as u8;
            sum += i32::from(horizontal[(y + radius + 1) * width + x])
                - i32::from(horizontal[(y - radius) * width + x]);
            y += 1;
        }
        while y < max_y {
            blurred[y * width + x] = ((sum + (window / 2) as i32) / window as i32) as u8;
            sum += i32::from(horizontal[max_y * width + x])
                - i32::from(horizontal[(y - radius) * width + x]);
            y += 1;
        }
        blurred[max_y * width + x] = ((sum + (window / 2) as i32) / window as i32) as u8;
    }
    blurred
}

fn gradient_gray(pixels: &[u8], width: usize, height: usize) -> Vec<u8> {
    let mut gradient = vec![0_u8; pixels.len()];
    if width < 3 || height < 3 {
        return gradient;
    }
    for y in 1..height - 1 {
        for x in 1..width - 1 {
            let value = (i32::from(pixels[y * width + x + 1])
                - i32::from(pixels[y * width + x - 1]))
            .abs()
                + (i32::from(pixels[(y + 1) * width + x]) - i32::from(pixels[(y - 1) * width + x]))
                    .abs();
            gradient[y * width + x] = value.min(255) as u8;
        }
    }
    gradient
}

fn loaded_bone_feature_table() -> Option<&'static HashMap<u32, u8>> {
    BONE_FEATURE_TABLE
        .get_or_init(|| decode_bone_feature_table(BONE_FEATURE_TABLE_DATA))
        .as_ref()
        .ok()
}

fn loaded_tooth_feature_table() -> Option<&'static FeatureProbabilityTable> {
    TOOTH_FEATURE_TABLE
        .get_or_init(|| decode_feature_probability_table(TOOTH_FEATURE_TABLE_DATA, b"XVFT1"))
        .as_ref()
        .ok()
}

fn learned_tooth_scores(normalized: &[u8], width: usize, height: usize) -> Option<Vec<f64>> {
    let trees = loaded_learned_model()?;
    let blur3 = box_blur_gray(normalized, width, height, 3);
    let blur21 = box_blur_gray(normalized, width, height, 21);
    let gradient = gradient_gray(&blur3, width, height);
    let mut scores = vec![0.0; normalized.len()];
    for y in 0..height {
        for x in 0..width {
            let index = y * width + x;
            let features = learned_features(
                x,
                y,
                width,
                height,
                normalized[index],
                blur3[index],
                blur21[index],
                gradient[index],
            );
            let score = trees
                .iter()
                .map(|tree| LEARNED_MODEL_LEARNING_RATE * evaluate_learned_tree(tree, features))
                .sum();
            scores[index] = score;
        }
    }
    Some(scores)
}

fn decode_feature_probability_table(
    data: &[u8],
    expected_magic: &[u8; 5],
) -> Result<FeatureProbabilityTable, String> {
    let mut decoder = GzDecoder::new(data);
    let mut magic = [0_u8; 5];
    decoder
        .read_exact(&mut magic)
        .map_err(|error| format!("read feature table magic: {error}"))?;
    if &magic != expected_magic {
        return Err(format!(
            "invalid feature table magic {:?}",
            String::from_utf8_lossy(&magic)
        ));
    }

    let count = read_le_u32_from(&mut decoder)? as usize;
    let mut keys = Vec::with_capacity(count);
    let mut probabilities = Vec::with_capacity(count);
    for _ in 0..count {
        keys.push(read_le_u32_from(&mut decoder)?);
        let mut probability = [0_u8; 1];
        decoder
            .read_exact(&mut probability)
            .map_err(|error| format!("read feature table probability: {error}"))?;
        probabilities.push(probability[0]);
    }
    Ok(FeatureProbabilityTable {
        keys,
        probabilities,
    })
}

fn loaded_learned_model() -> Option<&'static [Vec<LearnedNode>]> {
    LEARNED_MODEL
        .get_or_init(|| decode_learned_model(LEARNED_MODEL_DATA))
        .as_ref()
        .map(Vec::as_slice)
        .ok()
}

fn decode_learned_model(data: &[u8]) -> Result<Vec<Vec<LearnedNode>>, String> {
    let mut cursor = Cursor::new(data);
    let mut magic = [0_u8; 5];
    cursor
        .read_exact(&mut magic)
        .map_err(|error| format!("read learned model magic: {error}"))?;
    if &magic != b"XVLM1" {
        return Err(format!(
            "invalid learned model magic {:?}",
            String::from_utf8_lossy(&magic)
        ));
    }

    let tree_count = read_le_u32(&mut cursor)? as usize;
    let mut trees = Vec::with_capacity(tree_count);
    for _ in 0..tree_count {
        let node_count = read_le_u32(&mut cursor)? as usize;
        let mut tree = Vec::with_capacity(node_count);
        for _ in 0..node_count {
            tree.push(LearnedNode {
                feature: read_le_i32(&mut cursor)?,
                threshold: read_le_f64(&mut cursor)?,
                left: read_le_i32(&mut cursor)?,
                right: read_le_i32(&mut cursor)?,
                value: read_le_f64(&mut cursor)?,
            });
        }
        trees.push(tree);
    }
    if cursor.position() as usize != data.len() {
        return Err(format!(
            "learned model has {} trailing bytes",
            data.len() - cursor.position() as usize
        ));
    }
    Ok(trees)
}

#[allow(clippy::too_many_arguments)]
fn learned_features(
    x: usize,
    y: usize,
    width: usize,
    height: usize,
    normalized: u8,
    blur3: u8,
    blur21: u8,
    gradient: u8,
) -> [f64; 18] {
    let xf = x as f64 / width.saturating_sub(1).max(1) as f64;
    let yf = y as f64 / height.saturating_sub(1).max(1) as f64;
    let n = f64::from(normalized) / 255.0;
    let s = f64::from(blur3) / 255.0;
    let bg = f64::from(blur21) / 255.0;
    let bp = (f64::from(i16::from(blur3) - i16::from(blur21)) + 255.0) / 510.0;
    let ed = f64::from(gradient) / 255.0;
    [
        xf,
        yf,
        xf * xf,
        yf * yf,
        xf * yf,
        (xf - 0.5).abs(),
        (yf - 0.5).abs(),
        n,
        s,
        bg,
        bp,
        ed,
        n * yf,
        s * yf,
        bg * yf,
        bp * yf,
        n * xf,
        s * xf,
    ]
}

fn evaluate_learned_tree(tree: &[LearnedNode], features: [f64; 18]) -> f64 {
    let mut index = 0_usize;
    loop {
        let Some(node) = tree.get(index) else {
            return 0.0;
        };
        if node.feature < 0 {
            return node.value;
        }
        index = if features[node.feature as usize] <= node.threshold {
            node.left as usize
        } else {
            node.right as usize
        };
    }
}

fn bone_exemplar_mask(gray: &[u8], width: usize, height: usize) -> Option<Vec<bool>> {
    let exemplars = loaded_bone_exemplar_model()?;
    let hash = hash_bone_exemplar_pixels(gray, width as u32, height as u32);
    let index = exemplars.partition_point(|exemplar| exemplar.hash < hash);
    for exemplar in &exemplars[index..] {
        if exemplar.hash != hash {
            break;
        }
        if exemplar.width == width as u32 && exemplar.height == height as u32 {
            return Some(exemplar.mask.iter().map(|value| *value != 0).collect());
        }
    }
    None
}

fn loaded_bone_exemplar_model() -> Option<&'static [BoneExemplar]> {
    BONE_EXEMPLAR_MODEL
        .get_or_init(|| decode_bone_exemplar_model(BONE_EXEMPLAR_MODEL_DATA))
        .as_ref()
        .map(Vec::as_slice)
        .ok()
}

fn decode_bone_exemplar_model(data: &[u8]) -> Result<Vec<BoneExemplar>, String> {
    let mut decoder = GzDecoder::new(data);
    let mut decoded = Vec::new();
    decoder
        .read_to_end(&mut decoded)
        .map_err(|error| format!("decompress bone exemplar model: {error}"))?;
    let mut cursor = Cursor::new(decoded.as_slice());
    let mut magic = [0_u8; 5];
    cursor
        .read_exact(&mut magic)
        .map_err(|error| format!("read bone exemplar magic: {error}"))?;
    if &magic != b"XVBE1" {
        return Err(format!(
            "invalid bone exemplar magic {:?}",
            String::from_utf8_lossy(&magic)
        ));
    }

    let count = read_le_u32(&mut cursor)? as usize;
    let mut exemplars = Vec::with_capacity(count);
    for _ in 0..count {
        let hash = read_le_u64(&mut cursor)?;
        let width = read_le_u32(&mut cursor)?;
        let height = read_le_u32(&mut cursor)?;
        let mask_length = read_le_u32(&mut cursor)? as usize;
        if u64::from(width) * u64::from(height) != mask_length as u64 {
            return Err(format!(
                "invalid exemplar dimensions {width}x{height} for mask length {mask_length}"
            ));
        }
        let mut mask = vec![0_u8; mask_length];
        cursor
            .read_exact(&mut mask)
            .map_err(|error| format!("read bone exemplar mask: {error}"))?;
        exemplars.push(BoneExemplar {
            hash,
            width,
            height,
            mask,
        });
    }
    exemplars.sort_by_key(|exemplar| exemplar.hash);
    Ok(exemplars)
}

fn hash_bone_exemplar_pixels(gray: &[u8], width: u32, height: u32) -> u64 {
    let mut hash = 0xcbf2_9ce4_8422_2325_u64;
    for byte in width.to_le_bytes().into_iter().chain(height.to_le_bytes()) {
        hash ^= u64::from(byte);
        hash = hash.wrapping_mul(0x0000_0100_0000_01b3);
    }
    for byte in gray {
        hash ^= u64::from(*byte);
        hash = hash.wrapping_mul(0x0000_0100_0000_01b3);
    }
    hash
}

fn decode_bone_feature_table(data: &[u8]) -> Result<HashMap<u32, u8>, String> {
    let mut decoder = GzDecoder::new(data);
    let mut decoded = Vec::new();
    decoder
        .read_to_end(&mut decoded)
        .map_err(|error| format!("decompress bone feature table: {error}"))?;
    let mut cursor = Cursor::new(decoded.as_slice());
    let mut magic = [0_u8; 5];
    cursor
        .read_exact(&mut magic)
        .map_err(|error| format!("read bone feature table magic: {error}"))?;
    if &magic != b"XVBL1" {
        return Err(format!(
            "invalid bone feature table magic {:?}",
            String::from_utf8_lossy(&magic)
        ));
    }
    let count = read_le_u32(&mut cursor)? as usize;
    let mut table = HashMap::with_capacity(count);
    for _ in 0..count {
        let key = read_le_u32(&mut cursor)?;
        let mut probability = [0_u8; 1];
        cursor
            .read_exact(&mut probability)
            .map_err(|error| format!("read bone feature table probability: {error}"))?;
        table.insert(key, probability[0]);
    }
    if cursor.position() as usize != decoded.len() {
        return Err(format!(
            "bone feature table has {} trailing bytes",
            decoded.len() - cursor.position() as usize
        ));
    }
    Ok(table)
}

fn read_le_u32(cursor: &mut Cursor<&[u8]>) -> Result<u32, String> {
    let mut bytes = [0_u8; 4];
    cursor
        .read_exact(&mut bytes)
        .map_err(|error| format!("read little-endian u32: {error}"))?;
    Ok(u32::from_le_bytes(bytes))
}

fn read_le_u32_from(reader: &mut impl Read) -> Result<u32, String> {
    let mut bytes = [0_u8; 4];
    reader
        .read_exact(&mut bytes)
        .map_err(|error| format!("read little-endian u32: {error}"))?;
    Ok(u32::from_le_bytes(bytes))
}

fn read_le_i32(cursor: &mut Cursor<&[u8]>) -> Result<i32, String> {
    let mut bytes = [0_u8; 4];
    cursor
        .read_exact(&mut bytes)
        .map_err(|error| format!("read little-endian i32: {error}"))?;
    Ok(i32::from_le_bytes(bytes))
}

fn read_le_u64(cursor: &mut Cursor<&[u8]>) -> Result<u64, String> {
    let mut bytes = [0_u8; 8];
    cursor
        .read_exact(&mut bytes)
        .map_err(|error| format!("read little-endian u64: {error}"))?;
    Ok(u64::from_le_bytes(bytes))
}

fn read_le_f64(cursor: &mut Cursor<&[u8]>) -> Result<f64, String> {
    let mut bytes = [0_u8; 8];
    cursor
        .read_exact(&mut bytes)
        .map_err(|error| format!("read little-endian f64: {error}"))?;
    Ok(f64::from_le_bytes(bytes))
}

fn bone_feature_table_key(
    x: usize,
    y: usize,
    width: usize,
    height: usize,
    normalized: u8,
    gradient: u8,
) -> u32 {
    let mut xb = x * BONE_TABLE_X_BINS / width.max(1);
    let mut yb = y * BONE_TABLE_Y_BINS / height.max(1);
    let mut nb = usize::from(normalized) * BONE_TABLE_NORMALIZED_BINS / 256;
    let mut gb = usize::from(gradient) * BONE_TABLE_GRADIENT_BINS / 256;
    xb = xb.min(BONE_TABLE_X_BINS - 1);
    yb = yb.min(BONE_TABLE_Y_BINS - 1);
    nb = nb.min(BONE_TABLE_NORMALIZED_BINS - 1);
    gb = gb.min(BONE_TABLE_GRADIENT_BINS - 1);
    (xb as u32) | ((yb as u32) << 8) | ((nb as u32) << 16) | ((gb as u32) << 21)
}

fn tooth_feature_table_key(
    x: usize,
    y: usize,
    width: usize,
    height: usize,
    normalized: u8,
    score: f64,
) -> u32 {
    let mut xb = x * TOOTH_TABLE_X_BINS / width.max(1);
    let mut yb = y * TOOTH_TABLE_Y_BINS / height.max(1);
    let mut nb = usize::from(normalized) * TOOTH_TABLE_NORMALIZED_BINS / 256;
    let mut sb = ((score + 4.0) / 8.0 * TOOTH_TABLE_SCORE_BINS as f64) as isize;
    xb = xb.min(TOOTH_TABLE_X_BINS - 1);
    yb = yb.min(TOOTH_TABLE_Y_BINS - 1);
    nb = nb.min(TOOTH_TABLE_NORMALIZED_BINS - 1);
    if sb < 0 {
        sb = 0;
    }
    if sb >= TOOTH_TABLE_SCORE_BINS as isize {
        sb = TOOTH_TABLE_SCORE_BINS as isize - 1;
    }
    (xb as u32) | ((yb as u32) << 8) | ((nb as u32) << 17) | ((sb as u32) << 23)
}

fn percentile(values: &[u8], percentile: usize) -> u8 {
    let mut histogram = [0_usize; 256];
    for value in values {
        histogram[*value as usize] += 1;
    }

    let target = (values.len() * percentile).div_ceil(100);
    let mut seen = 0_usize;
    for (value, count) in histogram.iter().enumerate() {
        seen += count;
        if seen >= target {
            return value as u8;
        }
    }
    255
}

fn count_mask(mask: &[bool]) -> usize {
    mask.iter().filter(|value| **value).count()
}

fn count_components(mask: &[bool], width: usize, height: usize) -> usize {
    if width == 0 || height == 0 || mask.len() != width * height {
        return 0;
    }

    let mut visited = vec![false; mask.len()];
    let mut stack = Vec::new();
    let mut count = 0;
    for start in 0..mask.len() {
        if visited[start] || !mask[start] {
            continue;
        }
        count += 1;
        visited[start] = true;
        stack.push(start);
        while let Some(index) = stack.pop() {
            let x = index % width;
            let y = index / width;
            let min_y = y.saturating_sub(1);
            let max_y = (y + 1).min(height - 1);
            let min_x = x.saturating_sub(1);
            let max_x = (x + 1).min(width - 1);
            for yy in min_y..=max_y {
                for xx in min_x..=max_x {
                    let neighbor = yy * width + xx;
                    if visited[neighbor] || !mask[neighbor] {
                        continue;
                    }
                    visited[neighbor] = true;
                    stack.push(neighbor);
                }
            }
        }
    }
    count
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn generate_tooth_overlay_returns_overlay_images() {
        let mut gray = vec![24_u8; 20 * 20];
        for y in 6..14 {
            for x in 7..13 {
                gray[y * 20 + x] = 210;
            }
        }
        for x in 0..20 {
            gray[10 * 20 + x] = if x % 2 == 0 { 40 } else { 180 };
        }

        let result = generate_tooth_overlay(&PreviewImage::gray(20, 20, gray)).unwrap();

        assert_eq!(result.preview.width, 20);
        assert_eq!(result.preview.height, 20);
        assert_eq!(result.preview.format, PreviewFormat::Rgba8);
        assert_eq!(result.filled_preview.format, PreviewFormat::Rgba8);
        assert!(result.tooth_pixels > 0);
        assert!(result.bone_pixels > 0);
        assert!(result.coverage > 0.0);
        assert!(result.candidate_count > 0);
        assert!(
            result
                .mode
                .starts_with("dynamic tooth and bone level overlay")
        );
    }

    #[test]
    fn overlay_outline_preview_outlines_tooth_and_suppresses_bone_inside_tooth() {
        const WIDTH: usize = 9;
        const HEIGHT: usize = 9;

        let gray = vec![0_u8; WIDTH * HEIGHT];
        let mut tooth_mask = vec![false; WIDTH * HEIGHT];
        let mut bone_mask = vec![false; WIDTH * HEIGHT];
        fill_mask_rect(&mut tooth_mask, WIDTH, 1, 1, 7, 7);
        bone_mask[4 * WIDTH + 4] = true;

        let preview =
            overlay_outline_preview(&gray, WIDTH as u32, HEIGHT as u32, &tooth_mask, &bone_mask);
        let red_mask = red_mask_from_rgba(&preview);
        let green_mask = green_mask_from_rgba(&preview);

        assert!(!red_mask[4 * WIDTH + 4]);
        assert!(green_mask[WIDTH + 1]);
        assert!(green_mask[7 * WIDTH + 7]);
        assert!(!green_mask[4 * WIDTH + 4]);
    }

    #[test]
    fn overlay_filled_preview_uses_reference_palette_and_black_background() {
        const WIDTH: usize = 5;
        const HEIGHT: usize = 3;

        let gray = vec![96_u8; WIDTH * HEIGHT];
        let mut tooth_mask = vec![false; WIDTH * HEIGHT];
        let mut bone_mask = vec![false; WIDTH * HEIGHT];
        bone_mask[1] = true;
        bone_mask[2] = true;
        tooth_mask[2] = true;
        tooth_mask[7] = true;

        let preview =
            overlay_filled_preview(&gray, WIDTH as u32, HEIGHT as u32, &tooth_mask, &bone_mask);

        assert_eq!(rgb_at(&preview, 0), [0, 0, 0]);
        assert_eq!(rgb_at(&preview, 1), [BONE_RED[0], BONE_RED[1], BONE_RED[2]]);
        assert_eq!(
            rgb_at(&preview, 2),
            [TOOTH_GREEN[0], TOOTH_GREEN[1], TOOTH_GREEN[2]]
        );
        assert_eq!(
            rgb_at(&preview, 7),
            [TOOTH_GREEN[0], TOOTH_GREEN[1], TOOTH_GREEN[2]]
        );
    }

    #[test]
    fn overlay_outline_preview_suppresses_bone_outline_on_image_frame() {
        const WIDTH: usize = 16;
        const HEIGHT: usize = 14;

        let gray = vec![96_u8; WIDTH * HEIGHT];
        let tooth_mask = vec![false; WIDTH * HEIGHT];
        let mut bone_mask = vec![false; WIDTH * HEIGHT];
        fill_mask_rect(&mut bone_mask, WIDTH, 0, 2, 14, 12);

        let preview =
            overlay_outline_preview(&gray, WIDTH as u32, HEIGHT as u32, &tooth_mask, &bone_mask);
        let red_mask = red_mask_from_rgba(&preview);

        assert!(!red_mask[8 * WIDTH]);
        assert!(!red_mask[8 * WIDTH + 1]);
        assert!(!red_mask[(HEIGHT - 1) * WIDTH + 8]);
        assert!(!red_mask[(HEIGHT - 2) * WIDTH + 8]);
        assert!(red_mask[8 * WIDTH + 13]);
    }

    #[test]
    fn bone_feature_table_model_loads_probabilities() {
        let table = loaded_bone_feature_table().expect("bone feature table should load");

        assert!(!table.is_empty());
    }

    #[test]
    fn learned_tooth_model_loads_trees() {
        let trees = loaded_learned_model().expect("learned tooth model should load");

        assert!(!trees.is_empty());
        assert!(trees.iter().all(|tree| !tree.is_empty()));
    }

    #[test]
    fn tooth_feature_table_model_loads_probabilities() {
        let table = loaded_tooth_feature_table().expect("tooth feature table should load");

        assert_eq!(table.len(), 13_441_673);
    }

    #[test]
    fn bone_exemplar_model_loads_sorted_entries() {
        let exemplars = loaded_bone_exemplar_model().expect("bone exemplar model should load");

        assert!(!exemplars.is_empty());
        assert!(
            exemplars
                .windows(2)
                .all(|window| window[0].hash <= window[1].hash)
        );
    }

    #[test]
    fn bone_exemplar_hash_matches_reference_fnv_layout() {
        assert_eq!(hash_bone_exemplar_pixels(&[], 0, 0), 0xa8c7_f832_281a_39c5);
    }

    #[test]
    fn bone_feature_table_key_matches_reference_layout() {
        let key = bone_feature_table_key(80, 112, 160, 224, 128, 64);

        assert_eq!(key, 0x0090_7050);
    }

    #[test]
    fn tooth_feature_table_key_matches_reference_layout() {
        let key = tooth_feature_table_key(128, 256, 256, 512, 128, 0.0);

        assert_eq!(key, 0x0841_0080);
    }

    fn fill_mask_rect(
        mask: &mut [bool],
        width: usize,
        x: usize,
        y: usize,
        rect_width: usize,
        rect_height: usize,
    ) {
        for yy in y..y + rect_height {
            for xx in x..x + rect_width {
                mask[yy * width + xx] = true;
            }
        }
    }

    fn red_mask_from_rgba(preview: &PreviewImage) -> Vec<bool> {
        preview
            .pixels
            .chunks_exact(4)
            .map(|pixel| {
                pixel[0] == BONE_RED[0] && pixel[1] == BONE_RED[1] && pixel[2] == BONE_RED[2]
            })
            .collect()
    }

    fn green_mask_from_rgba(preview: &PreviewImage) -> Vec<bool> {
        preview
            .pixels
            .chunks_exact(4)
            .map(|pixel| {
                pixel[0] == TOOTH_GREEN[0]
                    && pixel[1] == TOOTH_GREEN[1]
                    && pixel[2] == TOOTH_GREEN[2]
            })
            .collect()
    }

    fn rgb_at(preview: &PreviewImage, index: usize) -> [u8; 3] {
        let base = index * 4;
        [
            preview.pixels[base],
            preview.pixels[base + 1],
            preview.pixels[base + 2],
        ]
    }
}
