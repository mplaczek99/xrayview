// Offline trainer for the tooth-segmentation forest.
//
// Reads the labeled BMP + PNG-mask pairs (default: ../images/BMP/Mine paired
// with ../images/PNG by file stem), extracts the SAME texture features the
// inference path uses (`xrayview_backend_rs::tooth_model`), augments each image
// with intensity- and scale-jitter (the augmentations that matter for
// position-free features — a flip barely changes a position-free feature
// distribution, but exposure and anatomy size do), then fits a gradient-boosted
// regression forest with histogram splits and writes the `XVLM2` asset to
// backend-rs/assets/analysis/learned_model.bin.
//
// Tooth label = green OR pink mask pixels (a cavity sits inside the tooth
// silhouette; the separate cavity detector owns pink). Everything else
// (red bone, black background) is non-tooth.
//
//   cargo run --release --locked --example train_tooth
//   cargo run --release --locked --example train_tooth -- <bmp_dir> <png_dir> <out.bin> <trees> <depth>
//
// Reproducible: a fixed-seed xorshift RNG drives all subsampling.

use std::{env, fs, path::Path};

use xrayview_backend_rs::{
    bmp::render_grayscale_preview_file_for_tooth_analysis,
    tooth_model::{FeaturePlanes, TOOTH_FEATURE_COUNT, ToothForest, TreeNode, normalize_gray},
};

// --- Hyperparameters (overridable by CLI args) ---------------------------------
const DEFAULT_TREES: usize = 250;
const DEFAULT_DEPTH: usize = 4;
const LEARNING_RATE: f64 = 0.1;
const MIN_LEAF: usize = 50;
const HIST_BINS: usize = 64;
// Per augmented image, sample at most this many pixels of each class. Bounds the
// training set so a full run is seconds, and balances tooth vs non-tooth.
const SAMPLES_PER_CLASS_PER_VARIANT: usize = 2500;
// Mask-positive (tooth) threshold for the reported training Dice — mirrors the
// inference cut in analysis.rs (TOOTH_SCORE_THRESHOLD).
const REPORT_THRESHOLD: f64 = 0.45;

// Mask archetype colors (see images/PNG palette), with the class each belongs
// to. A mask pixel is assigned to its nearest archetype.
#[derive(Clone, Copy, PartialEq)]
enum Class {
    Tooth,
    Bone,
    Background,
}

const ARCHETYPES: [([f64; 3], Class); 4] = [
    ([120.0, 255.0, 0.0], Class::Tooth),   // green
    ([255.0, 192.0, 203.0], Class::Tooth), // pink → cavity, inside tooth → tooth
    ([255.0, 0.0, 0.0], Class::Bone),      // red  → bone
    ([0.0, 0.0, 0.0], Class::Background),  // black → background
];

// Which class this run trains a one-vs-rest forest for.
#[derive(Clone, Copy)]
enum Target {
    Tooth,
    Bone,
}

impl Target {
    fn parse(s: &str) -> Self {
        match s {
            "tooth" => Target::Tooth,
            "bone" => Target::Bone,
            other => panic!("unknown target {other:?}; expected tooth|bone"),
        }
    }
    fn positive(self) -> Class {
        match self {
            Target::Tooth => Class::Tooth,
            Target::Bone => Class::Bone,
        }
    }
    fn default_out(self) -> &'static str {
        match self {
            Target::Tooth => "assets/analysis/learned_model.bin",
            Target::Bone => "assets/analysis/bone_model.bin",
        }
    }
    fn label(self) -> &'static str {
        match self {
            Target::Tooth => "tooth",
            Target::Bone => "bone",
        }
    }
}

struct Sample {
    features: [f64; TOOTH_FEATURE_COUNT],
    label: f64,
}

struct LabeledImage {
    gray: Vec<u8>,
    positive: Vec<bool>,
    width: usize,
    height: usize,
}

fn main() {
    let mut args = env::args().skip(1);
    let target = Target::parse(&args.next().unwrap_or_else(|| "tooth".to_string()));
    let bmp_dir = args
        .next()
        .unwrap_or_else(|| "../images/BMP/Mine".to_string());
    let png_dir = args.next().unwrap_or_else(|| "../images/PNG".to_string());
    let out_path = args
        .next()
        .unwrap_or_else(|| target.default_out().to_string());
    let trees: usize = args
        .next()
        .map_or(DEFAULT_TREES, |s| s.parse().expect("trees"));
    let depth: usize = args
        .next()
        .map_or(DEFAULT_DEPTH, |s| s.parse().expect("depth"));

    let pairs = collect_pairs(&bmp_dir, &png_dir);
    assert!(
        !pairs.is_empty(),
        "no BMP/PNG pairs found in {bmp_dir} + {png_dir}"
    );
    println!(
        "training {} forest from {} labeled pairs",
        target.label(),
        pairs.len()
    );

    let images: Vec<LabeledImage> = pairs.iter().map(|(b, p)| load_pair(b, p, target)).collect();

    let mut rng = Rng::new(0x5eed_1234_dead_beef);
    let mut samples: Vec<Sample> = Vec::new();
    // gamma jitter (exposure) × scale jitter (anatomy size). gamma=1,scale=1 is
    // the identity variant.
    let variants: &[(f64, f64)] = &[
        (1.0, 1.0),
        (0.8, 1.0),
        (1.25, 1.0),
        (1.0, 0.85),
        (1.0, 1.18),
    ];
    for image in &images {
        for &(gamma, scale) in variants {
            let (gray, positive, w, h) = augment(image, gamma, scale);
            let normalized = normalize_gray(&gray);
            let planes = FeaturePlanes::build(&normalized, w, h);
            collect_samples(&planes, &positive, w, h, &mut rng, &mut samples);
        }
    }
    println!(
        "collected {} samples ({} positive)",
        samples.len(),
        samples.iter().filter(|s| s.label > 0.5).count()
    );

    let forest = train(&samples, trees, depth);
    report_importance(&forest);
    report_dice(&forest, &images);

    let bytes = forest.encode();
    let out = Path::new(&out_path);
    if let Some(parent) = out.parent() {
        fs::create_dir_all(parent).ok();
    }
    fs::write(out, &bytes).expect("write model");
    println!(
        "wrote {} trees, {} bytes to {}",
        forest.trees.len(),
        bytes.len(),
        out_path
    );
}

// --- Data loading --------------------------------------------------------------

fn collect_pairs(bmp_dir: &str, png_dir: &str) -> Vec<(String, String)> {
    let mut pairs = Vec::new();
    let entries = fs::read_dir(bmp_dir).unwrap_or_else(|e| panic!("read {bmp_dir}: {e}"));
    for entry in entries.flatten() {
        let path = entry.path();
        if path.extension().and_then(|e| e.to_str()) != Some("bmp") {
            continue;
        }
        let stem = path.file_stem().and_then(|s| s.to_str()).unwrap_or("");
        let png = Path::new(png_dir).join(format!("{stem}.png"));
        if png.exists() {
            pairs.push((
                path.to_string_lossy().into_owned(),
                png.to_string_lossy().into_owned(),
            ));
        }
    }
    pairs.sort();
    pairs
}

fn load_pair(bmp: &str, png: &str, target: Target) -> LabeledImage {
    let rendered = render_grayscale_preview_file_for_tooth_analysis(bmp)
        .unwrap_or_else(|e| panic!("decode {bmp}: {e}"));
    let (width, height) = (rendered.width as usize, rendered.height as usize);
    let gray: Vec<u8> = rendered.pixels.as_ref().to_vec();

    let (mask_rgb, mw, mh) = load_png_rgb(png);
    assert!(
        mw == width && mh == height,
        "dim mismatch {bmp} ({width}x{height}) vs {png} ({mw}x{mh})"
    );
    let want = target.positive();
    let positive: Vec<bool> = (0..width * height)
        .map(|i| classify(mask_rgb[i * 3], mask_rgb[i * 3 + 1], mask_rgb[i * 3 + 2]) == want)
        .collect();
    LabeledImage {
        gray,
        positive,
        width,
        height,
    }
}

fn load_png_rgb(path: &str) -> (Vec<u8>, usize, usize) {
    let decoder =
        png::Decoder::new(fs::File::open(path).unwrap_or_else(|e| panic!("open {path}: {e}")));
    let mut reader = decoder
        .read_info()
        .unwrap_or_else(|e| panic!("png info {path}: {e}"));
    let mut buf = vec![0_u8; reader.output_buffer_size()];
    let info = reader
        .next_frame(&mut buf)
        .unwrap_or_else(|e| panic!("png frame {path}: {e}"));
    let (w, h) = (info.width as usize, info.height as usize);
    let channels = match info.color_type {
        png::ColorType::Rgba => 4,
        png::ColorType::Rgb => 3,
        other => panic!("unsupported PNG color type {other:?} in {path}"),
    };
    let mut rgb = vec![0_u8; w * h * 3];
    for i in 0..w * h {
        rgb[i * 3] = buf[i * channels];
        rgb[i * 3 + 1] = buf[i * channels + 1];
        rgb[i * 3 + 2] = buf[i * channels + 2];
    }
    (rgb, w, h)
}

// Nearest archetype color wins; returns the class that archetype belongs to.
fn classify(r: u8, g: u8, b: u8) -> Class {
    let p = [f64::from(r), f64::from(g), f64::from(b)];
    let mut best = f64::MAX;
    let mut class = Class::Background;
    for (color, archetype_class) in ARCHETYPES {
        let d = (0..3).map(|k| (p[k] - color[k]).powi(2)).sum::<f64>();
        if d < best {
            best = d;
            class = archetype_class;
        }
    }
    class
}

// --- Augmentation --------------------------------------------------------------

fn augment(image: &LabeledImage, gamma: f64, scale: f64) -> (Vec<u8>, Vec<bool>, usize, usize) {
    let gray = if (gamma - 1.0).abs() < 1e-9 {
        image.gray.clone()
    } else {
        let lut: [u8; 256] = std::array::from_fn(|v| {
            (255.0 * (v as f64 / 255.0).powf(gamma))
                .round()
                .clamp(0.0, 255.0) as u8
        });
        image.gray.iter().map(|&v| lut[v as usize]).collect()
    };
    if (scale - 1.0).abs() < 1e-9 {
        return (gray, image.positive.clone(), image.width, image.height);
    }
    let nw = ((image.width as f64) * scale).round().max(8.0) as usize;
    let nh = ((image.height as f64) * scale).round().max(8.0) as usize;
    let rg = resize_nearest_u8(&gray, image.width, image.height, nw, nh);
    let rt = resize_nearest_bool(&image.positive, image.width, image.height, nw, nh);
    (rg, rt, nw, nh)
}

fn resize_nearest_u8(src: &[u8], w: usize, h: usize, nw: usize, nh: usize) -> Vec<u8> {
    let mut out = vec![0_u8; nw * nh];
    for y in 0..nh {
        let sy = (y * h / nh).min(h - 1);
        for x in 0..nw {
            let sx = (x * w / nw).min(w - 1);
            out[y * nw + x] = src[sy * w + sx];
        }
    }
    out
}

fn resize_nearest_bool(src: &[bool], w: usize, h: usize, nw: usize, nh: usize) -> Vec<bool> {
    let mut out = vec![false; nw * nh];
    for y in 0..nh {
        let sy = (y * h / nh).min(h - 1);
        for x in 0..nw {
            let sx = (x * w / nw).min(w - 1);
            out[y * nw + x] = src[sy * w + sx];
        }
    }
    out
}

fn collect_samples(
    planes: &FeaturePlanes,
    tooth: &[bool],
    width: usize,
    height: usize,
    rng: &mut Rng,
    out: &mut Vec<Sample>,
) {
    let mut tooth_idx = Vec::new();
    let mut other_idx = Vec::new();
    for (i, &is_tooth) in tooth.iter().enumerate().take(width * height) {
        if is_tooth {
            tooth_idx.push(i);
        } else {
            other_idx.push(i);
        }
    }
    for (indices, label) in [(tooth_idx, 1.0_f64), (other_idx, 0.0_f64)] {
        let picked = reservoir(&indices, SAMPLES_PER_CLASS_PER_VARIANT, rng);
        for i in picked {
            let (x, y) = (i % width, i / width);
            out.push(Sample {
                features: planes.features(x, y),
                label,
            });
        }
    }
}

fn reservoir(indices: &[usize], budget: usize, rng: &mut Rng) -> Vec<usize> {
    if indices.len() <= budget {
        return indices.to_vec();
    }
    let mut chosen: Vec<usize> = indices[..budget].to_vec();
    for (seen, &value) in indices.iter().enumerate().skip(budget) {
        let j = (rng.next_u64() as usize) % (seen + 1);
        if j < budget {
            chosen[j] = value;
        }
    }
    chosen
}

// --- Gradient-boosted regression forest (histogram splits) ---------------------

fn train(samples: &[Sample], n_trees: usize, depth: usize) -> ToothForest {
    let n = samples.len();
    // Per-feature quantile bin edges (HIST_BINS-1 internal cuts).
    let edges = compute_bin_edges(samples);
    // Bin every sample once: bins[feature][sample] as u8.
    let mut bins = vec![vec![0_u8; n]; TOOTH_FEATURE_COUNT];
    for (s, sample) in samples.iter().enumerate() {
        for f in 0..TOOTH_FEATURE_COUNT {
            bins[f][s] = bin_of(sample.features[f], &edges[f]) as u8;
        }
    }
    let labels: Vec<f64> = samples.iter().map(|s| s.label).collect();

    let bias = labels.iter().sum::<f64>() / n as f64;
    let mut predictions = vec![bias; n];
    let mut trees: Vec<Vec<TreeNode>> = Vec::with_capacity(n_trees);
    let all: Vec<u32> = (0..n as u32).collect();

    for round in 0..n_trees {
        let residual: Vec<f64> = (0..n).map(|i| labels[i] - predictions[i]).collect();
        let mut nodes: Vec<TreeNode> = Vec::new();
        build_tree(&bins, &residual, &edges, &all, depth, &mut nodes);
        // Apply the new tree to update predictions, evaluating on the RAW
        // features with the stored real-valued thresholds — identical to how
        // inference (ToothForest::score) walks the tree. By construction
        // (bin_of), `feature <= threshold` matches the bin split used to build
        // the node, so training and inference agree bit-for-bit.
        for (i, sample) in samples.iter().enumerate() {
            predictions[i] += LEARNING_RATE * eval_tree_raw(&nodes, &sample.features);
        }
        trees.push(nodes);
        if round % 40 == 0 || round + 1 == n_trees {
            let mse = (0..n)
                .map(|i| (labels[i] - predictions[i]).powi(2))
                .sum::<f64>()
                / n as f64;
            println!("  round {round:4}: train mse = {mse:.5}");
        }
    }

    ToothForest {
        learning_rate: LEARNING_RATE,
        bias,
        trees,
    }
}

fn compute_bin_edges(samples: &[Sample]) -> Vec<Vec<f64>> {
    let mut edges = Vec::with_capacity(TOOTH_FEATURE_COUNT);
    for f in 0..TOOTH_FEATURE_COUNT {
        let mut values: Vec<f64> = samples.iter().map(|s| s.features[f]).collect();
        values.sort_by(|a, b| a.partial_cmp(b).unwrap());
        let mut cuts = Vec::with_capacity(HIST_BINS - 1);
        for k in 1..HIST_BINS {
            let idx = (k * values.len() / HIST_BINS).min(values.len() - 1);
            cuts.push(values[idx]);
        }
        cuts.dedup();
        edges.push(cuts);
    }
    edges
}

// bin(v) = #edges strictly less than v, so "bin <= b ⟺ v <= edges[b]" — the
// exact equivalence the inference `feature <= threshold` relies on.
fn bin_of(value: f64, edges: &[f64]) -> usize {
    edges.partition_point(|e| *e < value)
}

fn build_tree(
    bins: &[Vec<u8>],
    residual: &[f64],
    edges: &[Vec<f64>],
    rows: &[u32],
    depth: usize,
    nodes: &mut Vec<TreeNode>,
) -> i32 {
    let node_index = nodes.len() as i32;
    let (sum, count) = rows.iter().fold((0.0_f64, 0.0_f64), |(s, c), &r| {
        (s + residual[r as usize], c + 1.0)
    });
    let leaf_value = if count > 0.0 { sum / count } else { 0.0 };
    // Reserve the node slot (filled below as leaf or split).
    nodes.push(TreeNode {
        feature: -1,
        threshold: 0.0,
        left: -1,
        right: -1,
        value: leaf_value,
    });

    if depth == 0 || rows.len() < 2 * MIN_LEAF {
        return node_index;
    }
    let Some((feature, bin_split, _gain)) = best_split(bins, residual, rows) else {
        return node_index;
    };
    let threshold = edges[feature][bin_split];
    let (mut left_rows, mut right_rows) = (Vec::new(), Vec::new());
    for &r in rows {
        if bins[feature][r as usize] as usize <= bin_split {
            left_rows.push(r);
        } else {
            right_rows.push(r);
        }
    }
    if left_rows.len() < MIN_LEAF || right_rows.len() < MIN_LEAF {
        return node_index;
    }
    let left = build_tree(bins, residual, edges, &left_rows, depth - 1, nodes);
    let right = build_tree(bins, residual, edges, &right_rows, depth - 1, nodes);
    nodes[node_index as usize] = TreeNode {
        feature: feature as i32,
        threshold,
        left,
        right,
        value: leaf_value,
    };
    node_index
}

// Best (feature, split-bin) by variance-reduction gain: Σ_left²/n_left +
// Σ_right²/n_right − Σ²/n, maximized. Honors MIN_LEAF on both sides.
fn best_split(bins: &[Vec<u8>], residual: &[f64], rows: &[u32]) -> Option<(usize, usize, f64)> {
    let total_sum: f64 = rows.iter().map(|&r| residual[r as usize]).sum();
    let total_count = rows.len() as f64;
    let parent = total_sum * total_sum / total_count;
    let mut best: Option<(usize, usize, f64)> = None;
    for (feature, bin_col) in bins.iter().enumerate() {
        let mut hist_sum = [0.0_f64; HIST_BINS];
        let mut hist_cnt = [0.0_f64; HIST_BINS];
        for &r in rows {
            let b = bin_col[r as usize] as usize;
            hist_sum[b] += residual[r as usize];
            hist_cnt[b] += 1.0;
        }
        let (mut left_sum, mut left_cnt) = (0.0_f64, 0.0_f64);
        for b in 0..HIST_BINS - 1 {
            left_sum += hist_sum[b];
            left_cnt += hist_cnt[b];
            let right_cnt = total_count - left_cnt;
            if left_cnt < MIN_LEAF as f64 || right_cnt < MIN_LEAF as f64 {
                continue;
            }
            let right_sum = total_sum - left_sum;
            let gain = left_sum * left_sum / left_cnt + right_sum * right_sum / right_cnt - parent;
            if best.is_none_or(|(_, _, g)| gain > g) {
                best = Some((feature, b, gain));
            }
        }
    }
    best.filter(|(_, _, g)| *g > 1e-9)
}

fn eval_tree_raw(nodes: &[TreeNode], features: &[f64; TOOTH_FEATURE_COUNT]) -> f64 {
    let mut index = 0_usize;
    loop {
        let node = nodes[index];
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

// Deterministic xorshift64* RNG — reproducible subsampling without a dep.
struct Rng(u64);
impl Rng {
    fn new(seed: u64) -> Self {
        Rng(seed | 1)
    }
    fn next_u64(&mut self) -> u64 {
        let mut x = self.0;
        x ^= x >> 12;
        x ^= x << 25;
        x ^= x >> 27;
        self.0 = x;
        x.wrapping_mul(0x2545_f491_4f6c_dd1d)
    }
}

const FEATURE_NAMES: [&str; TOOTH_FEATURE_COUNT] = [
    "n",
    "m4",
    "m8",
    "m16",
    "m32",
    "s2",
    "s4",
    "s8",
    "s16",
    "s32",
    "grad",
    "n-m32",
    "m4-m32",
    "s2-s16",
    "zcontrast",
    "cov",
];

// How often each feature is used as a split across the whole forest — a cheap
// importance proxy. If the texture features (s2..s16, s2-s8) are near zero, the
// model is leaning on brightness and cannot separate isodense tooth/bone.
fn report_importance(forest: &ToothForest) {
    let mut counts = [0_usize; TOOTH_FEATURE_COUNT];
    for tree in &forest.trees {
        for node in tree {
            if node.feature >= 0 {
                counts[node.feature as usize] += 1;
            }
        }
    }
    let total: usize = counts.iter().sum::<usize>().max(1);
    let mut order: Vec<usize> = (0..TOOTH_FEATURE_COUNT).collect();
    order.sort_by_key(|&f| std::cmp::Reverse(counts[f]));
    print!("split usage:");
    for f in order {
        print!(
            " {}={:.0}%",
            FEATURE_NAMES[f],
            100.0 * counts[f] as f64 / total as f64
        );
    }
    println!();
}

// Sweep the decision threshold and report mean per-image Dice on the (training)
// set at each — lets us bake the best cut into TOOTH_SCORE_THRESHOLD.
fn report_dice(forest: &ToothForest, images: &[LabeledImage]) {
    let thresholds = [0.35, 0.40, 0.45, 0.50, 0.55, 0.60, 0.65];
    let scored: Vec<(Vec<f64>, &Vec<bool>)> = images
        .iter()
        .map(|image| {
            let normalized = normalize_gray(&image.gray);
            let planes = FeaturePlanes::build(&normalized, image.width, image.height);
            let scores: Vec<f64> = (0..image.width * image.height)
                .map(|i| forest.score(&planes.features(i % image.width, i / image.width)))
                .collect();
            (scores, &image.positive)
        })
        .collect();
    for threshold in thresholds {
        let mut dice_total = 0.0;
        // Balanced accuracy = (sensitivity + specificity) / 2, pooled over all
        // pixels. Unlike Dice it does NOT reward flooding a tooth-majority image,
        // so it is the honest guide for "stop greening the bone".
        let (mut tp, mut fp, mut tn, mut fn_) = (0.0_f64, 0.0_f64, 0.0_f64, 0.0_f64);
        for (scores, truth) in &scored {
            let (mut inter, mut pa, mut pb) = (0.0_f64, 0.0_f64, 0.0_f64);
            for (i, &score) in scores.iter().enumerate() {
                let pred = score >= threshold;
                match (pred, truth[i]) {
                    (true, true) => tp += 1.0,
                    (true, false) => fp += 1.0,
                    (false, true) => fn_ += 1.0,
                    (false, false) => tn += 1.0,
                }
                if pred {
                    pa += 1.0;
                }
                if truth[i] {
                    pb += 1.0;
                }
                if pred && truth[i] {
                    inter += 1.0;
                }
            }
            dice_total += if pa + pb > 0.0 {
                2.0 * inter / (pa + pb)
            } else {
                1.0
            };
        }
        let sensitivity = tp / (tp + fn_).max(1.0);
        let specificity = tn / (tn + fp).max(1.0);
        let balanced = 0.5 * (sensitivity + specificity);
        let marker = if (threshold - REPORT_THRESHOLD).abs() < 1e-9 {
            " <- current"
        } else {
            ""
        };
        println!(
            "  thr {threshold:.2}: Dice {:.4}  balAcc {balanced:.4} (sens {sensitivity:.3} spec {specificity:.3}){marker}",
            dice_total / images.len() as f64
        );
    }
}
