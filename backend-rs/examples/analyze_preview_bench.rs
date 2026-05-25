use std::{
    alloc::{GlobalAlloc, Layout, System},
    env, fs,
    hint::black_box,
    sync::atomic::{AtomicU64, AtomicUsize, Ordering},
    time::Instant,
};

use xrayview_backend_rs::render::PreviewImage;

// Counting allocator: the buffer-reuse work in `analysis` is about allocator
// traffic, not the ~170 ms tooth-scoring loop, so allocations/bytes per analysis
// is the deterministic metric (wall-clock end-to-end buries a ~1 ms delta under
// scoring-loop and one-time table-load noise).
struct CountingAllocator;
static ALLOC_COUNT: AtomicUsize = AtomicUsize::new(0);
static ALLOC_BYTES: AtomicU64 = AtomicU64::new(0);

unsafe impl GlobalAlloc for CountingAllocator {
    unsafe fn alloc(&self, layout: Layout) -> *mut u8 {
        ALLOC_COUNT.fetch_add(1, Ordering::Relaxed);
        ALLOC_BYTES.fetch_add(layout.size() as u64, Ordering::Relaxed);
        unsafe { System.alloc(layout) }
    }
    unsafe fn dealloc(&self, ptr: *mut u8, layout: Layout) {
        unsafe { System.dealloc(ptr, layout) }
    }
}

#[global_allocator]
static GLOBAL: CountingAllocator = CountingAllocator;

fn main() {
    let mut args = env::args().skip(1);
    let path = args
        .next()
        .unwrap_or_else(|| "../images/BMP/1.bmp".to_string());
    let iterations = args
        .next()
        .map(|value| value.parse::<usize>().expect("iterations must be a number"))
        .unwrap_or(10);

    let rendered =
        xrayview_backend_rs::bmp::render_grayscale_preview_file_for_tooth_analysis(&path)
            .expect("render analysis preview");
    let mut pixels = Vec::from(rendered.pixels.as_ref());
    if let Some(pixel) = pixels.first_mut() {
        *pixel = pixel.wrapping_add(1);
    }
    let preview = PreviewImage::gray(rendered.width, rendered.height, pixels);

    // Warmup: materialize the lazily-loaded ~67 MB feature tables and fault in
    // the rayon thread-pool / first-touch pages so neither the timed loop nor the
    // allocation counters charge those one-time costs.
    black_box(
        xrayview_backend_rs::analysis::generate_tooth_overlay(&preview)
            .expect("generate tooth overlay"),
    );

    let count_start = ALLOC_COUNT.load(Ordering::Relaxed);
    let bytes_start = ALLOC_BYTES.load(Ordering::Relaxed);
    let mut overlay_pixels = 0;
    let start = Instant::now();
    for _ in 0..iterations {
        let result = xrayview_backend_rs::analysis::generate_tooth_overlay(black_box(&preview))
            .expect("generate tooth overlay");
        overlay_pixels = result.preview.pixels.len() + result.filled_preview.pixels.len();
        black_box(result);
    }
    let elapsed = start.elapsed();
    let allocs = ALLOC_COUNT.load(Ordering::Relaxed) - count_start;
    let bytes = ALLOC_BYTES.load(Ordering::Relaxed) - bytes_start;

    // Untimed verification pass: a single FNV-1a checksum over both overlay
    // buffers plus the reported counts lets a before/after run prove the output
    // is bit-identical, not merely close (the guardrail for output-sensitive
    // analysis changes).
    let verify = xrayview_backend_rs::analysis::generate_tooth_overlay(&preview)
        .expect("generate tooth overlay");
    let mut checksum = 0xcbf2_9ce4_8422_2325_u64;
    for byte in verify
        .preview
        .pixels
        .iter()
        .chain(verify.filled_preview.pixels.iter())
    {
        checksum ^= u64::from(*byte);
        checksum = checksum.wrapping_mul(0x0000_0100_0000_01b3);
    }

    println!(
        "generate_tooth_overlay iterations={iterations} image={}x{} overlay_pixels={overlay_pixels} total_ms={:.3} avg_ms={:.3} vmhwm_kb={}",
        preview.width,
        preview.height,
        elapsed.as_secs_f64() * 1_000.0,
        elapsed.as_secs_f64() * 1_000.0 / iterations as f64,
        peak_resident_set_kb().unwrap_or(0),
    );
    println!(
        "allocs_per_iter={:.1} alloc_bytes_per_iter={}",
        allocs as f64 / iterations as f64,
        bytes / iterations as u64,
    );
    println!(
        "checksum=0x{checksum:016x} tooth_pixels={} bone_pixels={} candidate_count={} coverage={:.12}",
        verify.tooth_pixels, verify.bone_pixels, verify.candidate_count, verify.coverage,
    );
}

fn peak_resident_set_kb() -> Option<usize> {
    let status = fs::read_to_string("/proc/self/status").ok()?;
    let line = status.lines().find(|line| line.starts_with("VmHWM:"))?;
    line.split_whitespace().nth(1)?.parse().ok()
}
