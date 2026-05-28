// Focused microbenchmark for OPTIMIZATIONS.md #10 — bone-exemplar dimension pre-filter.
//
// `analysis::bone_exemplar_mask` is a memorized exact-match lookup keyed on a
// full-image FNV-1a hash, but a hit *also* requires the exemplar's dimensions to
// equal the image's. For an arbitrary user image whose (width, height) is not in
// the table, the old code still paid the full-image hash — an O(pixels) pass —
// before discovering the miss. The new code rejects on a cheap scan of the
// exemplar dimensions and skips the hash entirely.
//
// End-to-end this is dwarfed by the ~600 ms learned tooth-scoring loop, so a full
// `generate_tooth_overlay` bench cannot resolve it; this isolates the per-analysis
// work the pre-filter removes on the common miss path:
//   before(miss) = full-image hash + dimension scan
//   after (miss) = dimension scan only
//
// `fnv_hash` is a verbatim mirror of `analysis::hash_bone_exemplar_pixels` (locked
// by the hash fingerprint test in analysis.rs, asserted below); `exemplar_dims`
// mirrors the shipped bone_exemplar_model.bin.gz (38 exemplars: 29×(1200,854) +
// 9×(854,1200)).
//
//   cargo run --release --locked --example bone_exemplar_bench -- ../images/BMP/1.bmp 2000

use std::{env, fs, hint::black_box, time::Instant};

fn main() {
    let mut args = env::args().skip(1);
    let path = args
        .next()
        .unwrap_or_else(|| "../images/BMP/1.bmp".to_string());
    let iterations = args
        .next()
        .map(|value| value.parse::<usize>().expect("iterations must be a number"))
        .unwrap_or(2000);

    // Confirm the mirrored hash matches the fingerprint the real implementation
    // is locked to, so this bench can never silently drift from production code.
    assert_eq!(
        fnv_hash(&[], 0, 0),
        0xa8c7_f832_281a_39c5,
        "fnv_hash drifted from analysis::hash_bone_exemplar_pixels"
    );

    let rendered =
        xrayview_backend_rs::bmp::render_grayscale_preview_file_for_tooth_analysis(&path)
            .expect("render analysis preview");
    let gray: Vec<u8> = Vec::from(rendered.pixels.as_ref());
    let (width, height) = (rendered.width, rendered.height);

    // Mirror of the shipped exemplar dimension set, scanned exactly as the real
    // `bone_exemplar_mask` does. A miss dimension (one row taller than the image,
    // never in the table) models the common arbitrary-image case.
    let exemplar_dims: Vec<(u32, u32)> = std::iter::repeat_n((1200_u32, 854_u32), 29)
        .chain(std::iter::repeat_n((854_u32, 1200_u32), 9))
        .collect();
    let miss = (width, height + 1);

    // Warm up caches / CPU frequency so neither timed loop is penalized for going first.
    for _ in 0..(iterations / 10).max(16) {
        black_box(fnv_hash(black_box(&gray), miss.0, miss.1));
    }

    // before(miss): hash the whole image, then the dimension scan fails -> None.
    let mut acc = 0_u64;
    let start = Instant::now();
    for _ in 0..iterations {
        let hash = fnv_hash(black_box(&gray), miss.0, miss.1);
        let dims_present = exemplar_dims
            .iter()
            .any(|&(w, h)| w == miss.0 && h == miss.1);
        acc = acc.wrapping_add(hash).wrapping_add(u64::from(dims_present));
    }
    let before = start.elapsed();

    // after(miss): the dimension scan fails first, so the hash is never computed.
    let mut acc2 = 0_u64;
    let start = Instant::now();
    for _ in 0..iterations {
        let dims_present = exemplar_dims
            .iter()
            .any(|&(w, h)| w == miss.0 && h == miss.1);
        let hash = if dims_present {
            fnv_hash(black_box(&gray), miss.0, miss.1)
        } else {
            0
        };
        acc2 = acc2.wrapping_add(hash).wrapping_add(u64::from(dims_present));
    }
    let after = start.elapsed();

    black_box(acc);
    black_box(acc2);

    let to_ms = |elapsed: std::time::Duration| elapsed.as_secs_f64() * 1_000.0;
    let before_avg = to_ms(before) / iterations as f64;
    let after_avg = to_ms(after) / iterations as f64;
    println!(
        "bone_exemplar miss-path image={width}x{height} pixels={} iterations={iterations}",
        gray.len()
    );
    println!("  before(hash+dimscan): total_ms={:.3} avg_ms={before_avg:.4}", to_ms(before));
    println!("  after (dimscan only): total_ms={:.3} avg_ms={after_avg:.4}", to_ms(after));
    println!(
        "  saved_per_analysis_ms={:.4} vmhwm_kb={}",
        before_avg - after_avg,
        peak_resident_set_kb().unwrap_or(0)
    );
}

/// Verbatim mirror of `analysis::hash_bone_exemplar_pixels` (FNV-1a over the
/// little-endian dimensions followed by every gray byte).
fn fnv_hash(gray: &[u8], width: u32, height: u32) -> u64 {
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

// Linux-only RSS reader, mirroring the other benches.
fn peak_resident_set_kb() -> Option<usize> {
    let status = fs::read_to_string("/proc/self/status").ok()?;
    let line = status.lines().find(|line| line.starts_with("VmHWM:"))?;
    line.split_whitespace().nth(1)?.parse().ok()
}
