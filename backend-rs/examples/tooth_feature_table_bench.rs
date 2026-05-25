// Focused microbenchmark for OPTIMIZATIONS.md #16 — tooth feature-table lookup
// locality.
//
// `detect_learned_tooth_mask` performs one `FeatureProbabilityTable::probability`
// lookup per pixel. Before #16 that probed the full 13,441,673-entry / ~53.7 MB
// sorted key array with `binary_search`: ~24 comparisons each striding across a
// buffer far larger than L2/L3 — a cache miss per comparison, per pixel. #16
// buckets the sorted keys by their high bits so every per-pixel search is confined
// to a small, cache-resident slice. The found (key, probability) is identical, so
// analysis output is bit-identical.
//
// End-to-end this is one component of the ~140 ms `generate_tooth_overlay` (which
// is dominated by the tooth-scoring loop), so — like #10 isolating the
// bone-exemplar hash — this bench isolates just the lookup. `decode_table`,
// `tooth_feature_table_key`, the bucket index, and both lookups are verbatim
// mirrors of analysis.rs, asserted against the entry-count (13,441,673) and
// key-layout (0x0841_0080) fingerprints the production code is locked to; the
// bucketed lookup is also asserted to match the plain binary search for every
// query, which is the bit-identical guard.
//
//   cargo run --release --locked --example tooth_feature_table_bench -- ../images/BMP/1.bmp 8

use std::{env, fs, hint::black_box, io::Read, time::Instant};

use flate2::read::GzDecoder;

const TOOTH_FEATURE_TABLE_DATA: &[u8] =
    include_bytes!("../assets/analysis/feature_table_model.bin.gz");
const TOOTH_TABLE_X_BINS: usize = 256;
const TOOTH_TABLE_Y_BINS: usize = 512;
const TOOTH_TABLE_NORMALIZED_BINS: usize = 64;
const TOOTH_TABLE_SCORE_BINS: usize = 32;
const TOOTH_TABLE_BUCKET_SHIFT: u32 = 12;

fn main() {
    let mut args = env::args().skip(1);
    let path = args
        .next()
        .unwrap_or_else(|| "../images/BMP/1.bmp".to_string());
    let rounds = args
        .next()
        .map(|value| value.parse::<usize>().expect("rounds must be a number"))
        .unwrap_or(8);

    // Mirror of analysis::decode_feature_probability_table, locked to production
    // by the entry-count and key-layout fingerprints below.
    let (keys, probabilities) = decode_table();
    assert_eq!(
        keys.len(),
        13_441_673,
        "tooth feature table entry count drifted from analysis.rs"
    );
    assert_eq!(
        tooth_feature_table_key(128, 256, 256, 512, 128, 0.0),
        0x0841_0080,
        "tooth_feature_table_key drifted from analysis.rs layout"
    );
    let bucket_starts = build_bucket_index(&keys);

    // A real image yields the real (x, y, normalized) per pixel; deriving the
    // score from the normalized byte sweeps every sb bin, so the query keys span
    // the whole key space — one lookup per pixel in raster order, the production
    // access pattern that drives the cache behavior under test.
    let rendered =
        xrayview_backend_rs::bmp::render_grayscale_preview_file_for_tooth_analysis(&path)
            .expect("render analysis preview");
    let gray: Vec<u8> = Vec::from(rendered.pixels.as_ref());
    let (width, height) = (rendered.width as usize, rendered.height as usize);
    let mut query_keys = Vec::with_capacity(gray.len());
    for y in 0..height {
        for x in 0..width {
            let normalized = gray[y * width + x];
            let score = f64::from(normalized) / 255.0 * 8.0 - 4.0;
            query_keys.push(tooth_feature_table_key(x, y, width, height, normalized, score));
        }
    }

    // Bit-identical guard: both strategies must return the same Option<u8> for
    // every query. (Also reports the hit rate so the lookup mix is transparent.)
    let mut plain_sum = 0xcbf2_9ce4_8422_2325_u64;
    let mut bucket_sum = 0xcbf2_9ce4_8422_2325_u64;
    let mut hits = 0_usize;
    for &key in &query_keys {
        let plain = probability_plain(&keys, &probabilities, key);
        let bucketed = probability_bucketed(&keys, &probabilities, &bucket_starts, key);
        assert_eq!(plain, bucketed, "bucketed lookup diverged from binary search");
        if plain.is_some() {
            hits += 1;
        }
        plain_sum = fnv_step(plain_sum, plain);
        bucket_sum = fnv_step(bucket_sum, bucketed);
    }
    assert_eq!(plain_sum, bucket_sum);

    // Warm CPU frequency / first-touch pages so neither timed loop pays a cold start.
    let mut warm = 0_u64;
    for &key in query_keys.iter().take((query_keys.len() / 10).max(1)) {
        warm = warm.wrapping_add(u64::from(
            probability_bucketed(&keys, &probabilities, &bucket_starts, key).unwrap_or(0),
        ));
    }
    black_box(warm);

    let lookups = query_keys.len() * rounds;

    // before: full-array binary search over the ~53.7 MB key array.
    let mut acc_plain = 0_u64;
    let start = Instant::now();
    for _ in 0..rounds {
        for &key in &query_keys {
            acc_plain = acc_plain.wrapping_add(u64::from(
                probability_plain(black_box(&keys), &probabilities, black_box(key)).unwrap_or(0),
            ));
        }
    }
    let before = start.elapsed();

    // after: bucketed high-bit index narrows each search to a cache-resident slice.
    let mut acc_bucket = 0_u64;
    let start = Instant::now();
    for _ in 0..rounds {
        for &key in &query_keys {
            acc_bucket = acc_bucket.wrapping_add(u64::from(
                probability_bucketed(black_box(&keys), &probabilities, &bucket_starts, black_box(key))
                    .unwrap_or(0),
            ));
        }
    }
    let after = start.elapsed();

    assert_eq!(acc_plain, acc_bucket, "timed accumulators diverged");
    black_box(acc_plain);
    black_box(acc_bucket);

    let per_pass = query_keys.len();
    let to_ms = |elapsed: std::time::Duration| elapsed.as_secs_f64() * 1_000.0;
    let to_ns = |elapsed: std::time::Duration| elapsed.as_secs_f64() * 1e9 / lookups as f64;
    println!(
        "tooth_feature_table lookup image={width}x{height} lookups/pass={per_pass} rounds={rounds} hits={hits} ({:.1}%)",
        hits as f64 / per_pass as f64 * 100.0,
    );
    println!(
        "  table_entries={} key_bytes={} bucket_index_entries={} bucket_index_bytes={}",
        keys.len(),
        keys.len() * 4,
        bucket_starts.len(),
        bucket_starts.len() * 4,
    );
    println!(
        "  before(full binary_search): total_ms={:.3} avg_ns/lookup={:.2}",
        to_ms(before),
        to_ns(before),
    );
    println!(
        "  after (bucketed high bits): total_ms={:.3} avg_ns/lookup={:.2}",
        to_ms(after),
        to_ns(after),
    );
    println!(
        "  speedup={:.2}x vmhwm_kb={}",
        before.as_secs_f64() / after.as_secs_f64().max(f64::MIN_POSITIVE),
        peak_resident_set_kb().unwrap_or(0),
    );
}

/// Verbatim mirror of `analysis::decode_feature_probability_table` for the tooth
/// table (magic `XVFT1`, LE u32 count, then `count` × (LE u32 key, u8 probability)).
fn decode_table() -> (Vec<u32>, Vec<u8>) {
    let mut decoder = GzDecoder::new(TOOTH_FEATURE_TABLE_DATA);
    let mut magic = [0_u8; 5];
    decoder.read_exact(&mut magic).expect("read feature table magic");
    assert_eq!(&magic, b"XVFT1", "feature table magic drifted");
    let count = read_le_u32(&mut decoder) as usize;
    let mut keys = Vec::with_capacity(count);
    let mut probabilities = Vec::with_capacity(count);
    for _ in 0..count {
        keys.push(read_le_u32(&mut decoder));
        let mut probability = [0_u8; 1];
        decoder
            .read_exact(&mut probability)
            .expect("read feature table probability");
        probabilities.push(probability[0]);
    }
    (keys, probabilities)
}

/// Verbatim mirror of `FeatureProbabilityTable::new`'s bucket construction.
fn build_bucket_index(keys: &[u32]) -> Vec<u32> {
    let bucket_count = keys
        .last()
        .map_or(0, |&max| (max >> TOOTH_TABLE_BUCKET_SHIFT) as usize + 1);
    let mut bucket_starts = vec![0_u32; bucket_count + 1];
    let mut bucket = 0_usize;
    for (index, &key) in keys.iter().enumerate() {
        let target = (key >> TOOTH_TABLE_BUCKET_SHIFT) as usize;
        while bucket < target {
            bucket += 1;
            bucket_starts[bucket] = index as u32;
        }
    }
    for slot in &mut bucket_starts[bucket + 1..] {
        *slot = keys.len() as u32;
    }
    bucket_starts
}

/// Pre-#16 lookup: binary search over the whole sorted key array.
fn probability_plain(keys: &[u32], probabilities: &[u8], key: u32) -> Option<u8> {
    keys.binary_search(&key)
        .ok()
        .and_then(|index| probabilities.get(index).copied())
}

/// Post-#16 lookup: high-bit bucket narrows the search to a small slice.
fn probability_bucketed(
    keys: &[u32],
    probabilities: &[u8],
    bucket_starts: &[u32],
    key: u32,
) -> Option<u8> {
    let bucket = (key >> TOOTH_TABLE_BUCKET_SHIFT) as usize;
    let &end = bucket_starts.get(bucket + 1)?;
    let start = bucket_starts[bucket] as usize;
    let end = end as usize;
    keys[start..end]
        .binary_search(&key)
        .ok()
        .and_then(|offset| probabilities.get(start + offset).copied())
}

/// Verbatim mirror of `analysis::tooth_feature_table_key`.
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

fn read_le_u32(reader: &mut impl Read) -> u32 {
    let mut bytes = [0_u8; 4];
    reader.read_exact(&mut bytes).expect("read little-endian u32");
    u32::from_le_bytes(bytes)
}

/// Folds an `Option<u8>` lookup result into an FNV-1a checksum; `None` is encoded
/// as 256 so a present-but-zero probability never aliases an absent key.
fn fnv_step(mut hash: u64, value: Option<u8>) -> u64 {
    let token = value.map_or(256_u16, u16::from);
    for byte in token.to_le_bytes() {
        hash ^= u64::from(byte);
        hash = hash.wrapping_mul(0x0000_0100_0000_01b3);
    }
    hash
}

fn peak_resident_set_kb() -> Option<usize> {
    let status = fs::read_to_string("/proc/self/status").ok()?;
    let line = status.lines().find(|line| line.starts_with("VmHWM:"))?;
    line.split_whitespace().nth(1)?.parse().ok()
}
