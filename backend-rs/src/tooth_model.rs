// Shared tooth-segmentation model: feature extraction + the gradient-boosted
// forest, used by BOTH the inference path (`analysis::detect_tooth_mask`) and
// the offline trainer (`examples/train_tooth.rs`). Keeping them in one module
// is what guarantees train/inference feature parity — there is no second copy
// to drift.
//
// Why these features: tooth-root and alveolar bone are *isodense* (same gray),
// so brightness alone cannot tell them apart. The discriminator is texture —
// trabecular bone is a high-variance speckle at fine scales while a tooth is
// smooth — plus local contrast against the surrounding background. Every
// feature below is computed from the image's own neighbourhood statistics, so
// NONE of them encode absolute pixel position: a tooth is classified the same
// wherever it sits in the frame. That is the property the old absolute-(x,y)
// model lacked, which is why it only worked on its training subject.
//
// All window statistics come from two integral images (sum and sum-of-squares)
// so a mean/variance at any radius is an O(1) lookup — multi-scale texture for
// free instead of a blur per scale.

use std::io::{Cursor, Read};

use byteorder::{LittleEndian, ReadBytesExt, WriteBytesExt};

// Window radii (pixels) for the multi-scale neighbourhood statistics. r2/r4
// catch trabecular speckle (bone); r16/r32 approximate the local background and
// the coarse texture where teeth stay smooth but bone does not.
const WINDOW_RADII: [usize; 5] = [2, 4, 8, 16, 32];
/// Number of per-pixel features the forest consumes. Locked into the model
/// header so a mismatched asset is rejected at load instead of misread.
pub const TOOTH_FEATURE_COUNT: usize = 16;
// Avoids divide-by-zero in the normalized-contrast features (values are in the
// 0..1 range after the /255 scaling, so this is ~quarter of a gray level).
const FEATURE_EPSILON: f64 = 1.0 / 255.0;

const MODEL_MAGIC: &[u8; 5] = b"XVLM2";

/// One regression-tree node. `feature < 0` marks a leaf whose prediction is
/// `value`; otherwise `left`/`right` index sibling nodes in the same tree and
/// the split sends `features[feature] <= threshold` left.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct TreeNode {
    pub feature: i32,
    pub threshold: f64,
    pub left: i32,
    pub right: i32,
    pub value: f64,
}

/// A gradient-boosted forest. Prediction is `bias + learning_rate * Σ tree`.
#[derive(Debug, Clone, PartialEq)]
pub struct ToothForest {
    pub learning_rate: f64,
    pub bias: f64,
    pub trees: Vec<Vec<TreeNode>>,
}

impl ToothForest {
    /// Forest output for one feature vector (a probability-like score in
    /// roughly `[0, 1]`; threshold at ~0.5 for a hard mask).
    #[must_use]
    pub fn score(&self, features: &[f64; TOOTH_FEATURE_COUNT]) -> f64 {
        let mut sum = self.bias;
        for tree in &self.trees {
            sum += self.learning_rate * eval_tree(tree, features);
        }
        sum
    }

    /// Serialize to the `XVLM2` asset format consumed by [`ToothForest::decode`].
    #[must_use]
    pub fn encode(&self) -> Vec<u8> {
        let mut out = Vec::new();
        out.extend_from_slice(MODEL_MAGIC);
        out.write_u32::<LittleEndian>(TOOTH_FEATURE_COUNT as u32)
            .unwrap();
        out.write_f64::<LittleEndian>(self.learning_rate).unwrap();
        out.write_f64::<LittleEndian>(self.bias).unwrap();
        out.write_u32::<LittleEndian>(self.trees.len() as u32)
            .unwrap();
        for tree in &self.trees {
            out.write_u32::<LittleEndian>(tree.len() as u32).unwrap();
            for node in tree {
                out.write_i32::<LittleEndian>(node.feature).unwrap();
                out.write_f64::<LittleEndian>(node.threshold).unwrap();
                out.write_i32::<LittleEndian>(node.left).unwrap();
                out.write_i32::<LittleEndian>(node.right).unwrap();
                out.write_f64::<LittleEndian>(node.value).unwrap();
            }
        }
        out
    }

    /// Parse + validate an `XVLM2` asset. Rejects a wrong magic, a feature-count
    /// mismatch, non-finite numbers, out-of-range child indices, and cycles, so
    /// a corrupt model is caught at load rather than producing garbage masks.
    pub fn decode(data: &[u8]) -> Result<Self, String> {
        let mut cursor = Cursor::new(data);
        let mut magic = [0_u8; 5];
        cursor
            .read_exact(&mut magic)
            .map_err(|error| format!("read tooth model magic: {error}"))?;
        if &magic != MODEL_MAGIC {
            return Err(format!(
                "invalid tooth model magic {:?}",
                String::from_utf8_lossy(&magic)
            ));
        }
        let feature_count = read_u32(&mut cursor)? as usize;
        if feature_count != TOOTH_FEATURE_COUNT {
            return Err(format!(
                "tooth model feature count {feature_count} != expected {TOOTH_FEATURE_COUNT}"
            ));
        }
        let learning_rate = read_f64(&mut cursor)?;
        let bias = read_f64(&mut cursor)?;
        if !learning_rate.is_finite() || !bias.is_finite() {
            return Err("tooth model has non-finite learning rate or bias".to_string());
        }

        let tree_count = read_u32(&mut cursor)? as usize;
        let mut trees = Vec::with_capacity(tree_count);
        for _ in 0..tree_count {
            let node_count = read_u32(&mut cursor)? as usize;
            let mut tree = Vec::with_capacity(node_count);
            for _ in 0..node_count {
                tree.push(TreeNode {
                    feature: read_i32(&mut cursor)?,
                    threshold: read_f64(&mut cursor)?,
                    left: read_i32(&mut cursor)?,
                    right: read_i32(&mut cursor)?,
                    value: read_f64(&mut cursor)?,
                });
            }
            validate_tree(trees.len(), &tree)?;
            trees.push(tree);
        }
        if cursor.position() as usize != data.len() {
            return Err(format!(
                "tooth model has {} trailing bytes",
                data.len() - cursor.position() as usize
            ));
        }
        Ok(Self {
            learning_rate,
            bias,
            trees,
        })
    }
}

fn eval_tree(tree: &[TreeNode], features: &[f64; TOOTH_FEATURE_COUNT]) -> f64 {
    let mut index = 0_usize;
    // Bounded by node count: a validated tree is acyclic, so this terminates at
    // a leaf well within `tree.len()` steps.
    for _ in 0..=tree.len() {
        let Some(node) = tree.get(index) else {
            return 0.0;
        };
        if node.feature < 0 {
            return node.value;
        }
        let Some(feature) = features.get(node.feature as usize) else {
            return 0.0;
        };
        index = if *feature <= node.threshold {
            node.left as usize
        } else {
            node.right as usize
        };
    }
    0.0
}

fn validate_tree(tree_index: usize, tree: &[TreeNode]) -> Result<(), String> {
    if tree.is_empty() {
        return Err(format!("tooth model tree {tree_index} is empty"));
    }
    for (node_index, node) in tree.iter().enumerate() {
        if !node.threshold.is_finite() || !node.value.is_finite() {
            return Err(format!(
                "tooth model tree {tree_index} node {node_index} has non-finite field"
            ));
        }
        if node.feature < 0 {
            continue;
        }
        if node.feature as usize >= TOOTH_FEATURE_COUNT {
            return Err(format!(
                "tooth model tree {tree_index} node {node_index} references feature {}",
                node.feature
            ));
        }
        for (label, child) in [("left", node.left), ("right", node.right)] {
            if child < 0 || child as usize >= tree.len() {
                return Err(format!(
                    "tooth model tree {tree_index} node {node_index} has invalid {label} child {child}"
                ));
            }
        }
    }
    let mut visiting = vec![false; tree.len()];
    let mut visited = vec![false; tree.len()];
    validate_node(tree_index, tree, 0, &mut visiting, &mut visited)
}

fn validate_node(
    tree_index: usize,
    tree: &[TreeNode],
    index: usize,
    visiting: &mut [bool],
    visited: &mut [bool],
) -> Result<(), String> {
    if visited[index] {
        return Ok(());
    }
    if visiting[index] {
        return Err(format!(
            "tooth model tree {tree_index} contains a cycle at node {index}"
        ));
    }
    visiting[index] = true;
    let node = tree[index];
    if node.feature >= 0 {
        validate_node(tree_index, tree, node.left as usize, visiting, visited)?;
        validate_node(tree_index, tree, node.right as usize, visiting, visited)?;
    }
    visiting[index] = false;
    visited[index] = true;
    Ok(())
}

/// Per-image precomputation that makes per-pixel feature extraction O(1):
/// integral images of the normalized intensity and its square, plus a gradient
/// plane. Build once per image, then call [`FeaturePlanes::features`] per pixel.
pub struct FeaturePlanes {
    width: usize,
    height: usize,
    normalized: Vec<u8>,
    // (width + 1) * (height + 1) integral images, so a window sum is four loads.
    sum: Vec<u64>,
    sum_sq: Vec<u64>,
    gradient: Vec<u8>,
}

impl FeaturePlanes {
    /// Build the integral images and gradient plane from a normalized
    /// (contrast-stretched) grayscale buffer.
    #[must_use]
    pub fn build(normalized: &[u8], width: usize, height: usize) -> Self {
        let stride = width + 1;
        let mut sum = vec![0_u64; stride * (height + 1)];
        let mut sum_sq = vec![0_u64; stride * (height + 1)];
        for y in 0..height {
            let mut row_sum = 0_u64;
            let mut row_sum_sq = 0_u64;
            for x in 0..width {
                let value = u64::from(normalized[y * width + x]);
                row_sum += value;
                row_sum_sq += value * value;
                let here = (y + 1) * stride + (x + 1);
                let above = y * stride + (x + 1);
                sum[here] = sum[above] + row_sum;
                sum_sq[here] = sum_sq[above] + row_sum_sq;
            }
        }
        let gradient = gradient_plane(normalized, width, height);
        Self {
            width,
            height,
            normalized: normalized.to_vec(),
            sum,
            sum_sq,
            gradient,
        }
    }

    #[must_use]
    pub fn width(&self) -> usize {
        self.width
    }

    #[must_use]
    pub fn height(&self) -> usize {
        self.height
    }

    /// The contrast-normalized buffer these planes were built from.
    #[must_use]
    pub fn normalized(&self) -> &[u8] {
        &self.normalized
    }

    // Mean and standard deviation of the (clamped) window of radius `r` around
    // (x, y), both in raw 0..255 units. Integral-image lookup → O(1).
    fn window_mean_std(&self, x: usize, y: usize, r: usize) -> (f64, f64) {
        let stride = self.width + 1;
        let x0 = x.saturating_sub(r);
        let y0 = y.saturating_sub(r);
        let x1 = (x + r + 1).min(self.width);
        let y1 = (y + r + 1).min(self.height);
        let area = ((x1 - x0) * (y1 - y0)) as f64;
        let region = |table: &[u64]| -> f64 {
            let a = table[y1 * stride + x1];
            let b = table[y0 * stride + x1];
            let c = table[y1 * stride + x0];
            let d = table[y0 * stride + x0];
            (a + d - b - c) as f64
        };
        let mean = region(&self.sum) / area;
        let variance = (region(&self.sum_sq) / area - mean * mean).max(0.0);
        (mean, variance.sqrt())
    }

    /// The 16-feature vector for pixel (x, y). Position-free by construction —
    /// only neighbourhood statistics enter, never x or y themselves. Several
    /// features are normalized by the local mean/std so they survive exposure
    /// differences between sensors and subjects.
    #[must_use]
    pub fn features(&self, x: usize, y: usize) -> [f64; TOOTH_FEATURE_COUNT] {
        let n = f64::from(self.normalized[y * self.width + x]) / 255.0;
        let (_m2, s2) = self.window_mean_std(x, y, WINDOW_RADII[0]);
        let (m4, s4) = self.window_mean_std(x, y, WINDOW_RADII[1]);
        let (m8, s8) = self.window_mean_std(x, y, WINDOW_RADII[2]);
        let (m16, s16) = self.window_mean_std(x, y, WINDOW_RADII[3]);
        let (m32, s32) = self.window_mean_std(x, y, WINDOW_RADII[4]);
        let grad = f64::from(self.gradient[y * self.width + x]) / 255.0;
        let (m4, m8, m16, m32) = (m4 / 255.0, m8 / 255.0, m16 / 255.0, m32 / 255.0);
        let (s2, s4, s8, s16, s32) = (s2 / 255.0, s4 / 255.0, s8 / 255.0, s16 / 255.0, s32 / 255.0);
        [
            n,                                   // raw brightness
            m4,                                  // local mean (fine)
            m8,                                  // local mean (mid)
            m16,                                 // local mean (background)
            m32,                                 // local mean (broad background)
            s2,                                  // fine texture — high on trabecular bone
            s4,                                  // texture
            s8,                                  // texture (coarser)
            s16,                                 // coarse texture
            s32,                                 // broad texture — teeth stay smooth here
            grad,                                // edge response (PDL / lamina dura, cusp edges)
            n - m32,                             // local contrast vs broad background
            m4 - m32,                            // band-pass: fine vs coarse mean
            s2 - s16,                            // texture-scale contrast (speckle vs smooth)
            (n - m16) / (s16 + FEATURE_EPSILON), // z-scored contrast (exposure-invariant)
            s16 / (m16 + FEATURE_EPSILON),       // coefficient of variation (texture / brightness)
        ]
    }
}

// Gradient magnitude (|dx| + |dy|), clamped to u8, matching the cheap operator
// the bone path already uses. Borders stay 0.
fn gradient_plane(pixels: &[u8], width: usize, height: usize) -> Vec<u8> {
    let mut gradient = vec![0_u8; pixels.len()];
    if width < 3 || height < 3 {
        return gradient;
    }
    for y in 1..height - 1 {
        for x in 1..width - 1 {
            let dx = i32::from(pixels[y * width + x + 1]) - i32::from(pixels[y * width + x - 1]);
            let dy =
                i32::from(pixels[(y + 1) * width + x]) - i32::from(pixels[(y - 1) * width + x]);
            gradient[y * width + x] = (dx.abs() + dy.abs()).min(255) as u8;
        }
    }
    gradient
}

/// Contrast-normalize a grayscale buffer by stretching the 1st–99th percentile
/// to the full 0..255 range. Shared by inference and training so both see the
/// same input distribution. Identical to the stretch the analyzer applied
/// before, lifted here as the single source of truth.
#[must_use]
pub fn normalize_gray(pixels: &[u8]) -> Vec<u8> {
    let (low, high) = percentile_bounds(pixels, 0.01, 0.99);
    if high <= low {
        return pixels.to_vec();
    }
    let range = i32::from(high) - i32::from(low);
    let lut: [u8; 256] = std::array::from_fn(|value| {
        let value = value as u8;
        if value <= low {
            0
        } else if value >= high {
            255
        } else {
            (((i32::from(value) - i32::from(low)) * 255 + range / 2) / range) as u8
        }
    });
    pixels
        .iter()
        .map(|value| lut[usize::from(*value)])
        .collect()
}

fn percentile_bounds(pixels: &[u8], low_p: f64, high_p: f64) -> (u8, u8) {
    if pixels.is_empty() {
        return (0, 0);
    }
    let mut histogram = [0_usize; 256];
    for value in pixels {
        histogram[*value as usize] += 1;
    }
    let target = |p: f64| -> isize {
        (((pixels.len() - 1) as f64 * p).round() as isize).clamp(0, pixels.len() as isize - 1)
    };
    let (low_target, high_target) = (target(low_p), target(high_p));
    let mut low_value = None;
    let mut cumulative = 0_isize;
    for (value, count) in histogram.iter().enumerate() {
        cumulative += *count as isize;
        if low_value.is_none() && cumulative > low_target {
            low_value = Some(value as u8);
        }
        if cumulative > high_target {
            return (low_value.unwrap_or(value as u8), value as u8);
        }
    }
    (low_value.unwrap_or(u8::MAX), u8::MAX)
}

fn read_u32(reader: &mut impl Read) -> Result<u32, String> {
    reader
        .read_u32::<LittleEndian>()
        .map_err(|error| format!("read u32: {error}"))
}

fn read_i32(reader: &mut impl Read) -> Result<i32, String> {
    reader
        .read_i32::<LittleEndian>()
        .map_err(|error| format!("read i32: {error}"))
}

fn read_f64(reader: &mut impl Read) -> Result<f64, String> {
    reader
        .read_f64::<LittleEndian>()
        .map_err(|error| format!("read f64: {error}"))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn encode_decode_round_trips() {
        let forest = ToothForest {
            learning_rate: 0.1,
            bias: 0.25,
            trees: vec![
                vec![
                    TreeNode {
                        feature: 0,
                        threshold: 0.5,
                        left: 1,
                        right: 2,
                        value: 0.0,
                    },
                    TreeNode {
                        feature: -1,
                        threshold: 0.0,
                        left: -1,
                        right: -1,
                        value: 1.0,
                    },
                    TreeNode {
                        feature: -1,
                        threshold: 0.0,
                        left: -1,
                        right: -1,
                        value: -1.0,
                    },
                ],
                vec![TreeNode {
                    feature: -1,
                    threshold: 0.0,
                    left: -1,
                    right: -1,
                    value: 0.5,
                }],
            ],
        };
        let decoded = ToothForest::decode(&forest.encode()).unwrap();
        assert_eq!(decoded, forest);
    }

    #[test]
    fn decode_rejects_feature_count_mismatch() {
        let mut bytes = ToothForest {
            learning_rate: 0.1,
            bias: 0.0,
            trees: vec![vec![TreeNode {
                feature: -1,
                threshold: 0.0,
                left: -1,
                right: -1,
                value: 0.0,
            }]],
        }
        .encode();
        // Corrupt the feature-count u32 that follows the 5-byte magic.
        bytes[5] = TOOTH_FEATURE_COUNT as u8 + 1;
        let error = ToothForest::decode(&bytes).unwrap_err();
        assert!(error.contains("feature count"));
    }

    #[test]
    fn score_follows_splits() {
        // feature[0] <= 0.5 → leaf 1 (value 2.0); else leaf 2 (value -2.0).
        let forest = ToothForest {
            learning_rate: 0.5,
            bias: 1.0,
            trees: vec![vec![
                TreeNode {
                    feature: 0,
                    threshold: 0.5,
                    left: 1,
                    right: 2,
                    value: 0.0,
                },
                TreeNode {
                    feature: -1,
                    threshold: 0.0,
                    left: -1,
                    right: -1,
                    value: 2.0,
                },
                TreeNode {
                    feature: -1,
                    threshold: 0.0,
                    left: -1,
                    right: -1,
                    value: -2.0,
                },
            ]],
        };
        let mut low = [0.0_f64; TOOTH_FEATURE_COUNT];
        low[0] = 0.0;
        let mut high = [0.0_f64; TOOTH_FEATURE_COUNT];
        high[0] = 1.0;
        assert_eq!(forest.score(&low), 1.0 + 0.5 * 2.0);
        assert_eq!(forest.score(&high), 1.0 + 0.5 * -2.0);
    }

    #[test]
    fn features_are_position_invariant_for_uniform_image() {
        // A flat image → every pixel has identical neighbourhood stats, so the
        // feature vector must not vary with (x, y).
        let width = 40;
        let height = 30;
        let normalized = vec![128_u8; width * height];
        let planes = FeaturePlanes::build(&normalized, width, height);
        let a = planes.features(5, 5);
        let b = planes.features(31, 22);
        assert_eq!(a, b);
    }

    #[test]
    fn fine_texture_feature_separates_speckle_from_flat() {
        // Two images, same mean brightness: one flat, one high-frequency
        // checkerboard (a stand-in for trabecular bone). The fine-texture
        // feature (index 4, s2) must be larger on the speckled one.
        let width = 32;
        let height = 32;
        let flat = vec![128_u8; width * height];
        let mut speckle = vec![0_u8; width * height];
        for (index, pixel) in speckle.iter_mut().enumerate() {
            let (x, y) = (index % width, index / width);
            *pixel = if (x + y) % 2 == 0 { 64 } else { 192 };
        }
        // Feature index 5 is s2 (fine-scale local std); see `features`.
        let flat_s2 = FeaturePlanes::build(&flat, width, height).features(16, 16)[5];
        let speckle_s2 = FeaturePlanes::build(&speckle, width, height).features(16, 16)[5];
        assert!(
            speckle_s2 > flat_s2 + 0.1,
            "speckle s2 {speckle_s2} should exceed flat s2 {flat_s2}"
        );
    }
}
