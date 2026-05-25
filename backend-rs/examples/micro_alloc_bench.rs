use std::{
    alloc::{GlobalAlloc, Layout, System},
    hint::black_box,
    sync::{
        Arc,
        atomic::{AtomicU64, AtomicUsize, Ordering},
    },
    time::Instant,
};

use serde_json::json;
use xrayview_backend_rs::{
    contracts::{JobKind, JobProgress, JobResult, JobSnapshot, JobState},
    processing::{self, GrayscaleControls, Palette},
    render::{self, PreviewImage},
};

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
    let mut args = std::env::args().skip(1);
    let iterations = args
        .next()
        .map(|value| value.parse::<usize>().expect("iterations must be a number"))
        .unwrap_or(100);
    let width = args
        .next()
        .map(|value| value.parse::<u32>().expect("width must be a number"))
        .unwrap_or(2048);
    let height = args
        .next()
        .map(|value| value.parse::<u32>().expect("height must be a number"))
        .unwrap_or(1536);

    bench_process_compare(iterations, width, height);
    bench_encode_rgba_bmp(iterations, width, height);
    bench_clone_job_snapshot(iterations, 8, 256 * 1024);
}

fn bench_process_compare(iterations: usize, width: u32, height: u32) {
    let controls = GrayscaleControls {
        invert: false,
        brightness: 12,
        contrast: 1.2,
        equalize: false,
    };
    let source = PreviewImage::gray(width, height, synthetic_gray(width, height));

    let (elapsed, allocs, bytes, output_bytes) = measure(iterations, || {
        let output =
            processing::process_rendered_preview(source.clone(), controls, Palette::None, true)
                .expect("process comparison preview");
        let bytes = output.preview.pixels.len();
        black_box(output);
        bytes
    });

    print_result(
        "process_compare",
        iterations,
        elapsed,
        allocs,
        bytes,
        output_bytes,
    );
}

fn bench_encode_rgba_bmp(iterations: usize, width: u32, height: u32) {
    let preview = PreviewImage::rgba(width, height, synthetic_rgba(width, height));

    let (elapsed, allocs, bytes, output_bytes) = measure(iterations, || {
        let encoded = render::encode_preview_bmp(black_box(&preview)).expect("encode RGBA BMP");
        let bytes = encoded.len();
        black_box(encoded);
        bytes
    });

    print_result(
        "encode_rgba_bmp",
        iterations,
        elapsed,
        allocs,
        bytes,
        output_bytes,
    );
}

fn bench_clone_job_snapshot(iterations: usize, subscribers: usize, payload_bytes: usize) {
    let snapshot = JobSnapshot {
        job_id: "job-bench".to_string(),
        job_kind: JobKind::ProcessStudy,
        study_id: Some("study-bench".to_string()),
        state: JobState::Completed,
        progress: JobProgress {
            percent: 100,
            stage: "completed".to_string(),
            message: "Completed".to_string(),
        },
        from_cache: false,
        result: Some(Arc::new(JobResult {
            kind: JobKind::ProcessStudy,
            payload: json!({
                "studyId": "study-bench",
                "previewPath": "/tmp/xrayview-preview.bmp",
                "loadedWidth": 2048,
                "loadedHeight": 1536,
                "mode": "comparison of grayscale and grayscale brightness +12 contrast 1.2",
                "diagnosticPayload": "x".repeat(payload_bytes),
            }),
        })),
        error: None,
    };

    let clones_per_iter = subscribers.max(1);
    let (elapsed, allocs, bytes, output_bytes) = measure(iterations, || {
        let mut clones = Vec::with_capacity(clones_per_iter);
        for _ in 0..clones_per_iter {
            clones.push(black_box(&snapshot).clone());
        }
        let result = clones.len();
        black_box(clones);
        result
    });

    println!(
        "clone_job_snapshot iterations={iterations} subscribers={clones_per_iter} payload_bytes={payload_bytes} total_ms={:.3} avg_us={:.3} allocs_per_iter={:.1} alloc_bytes_per_iter={} clones_per_iter={output_bytes}",
        elapsed.as_secs_f64() * 1_000.0,
        elapsed.as_secs_f64() * 1_000_000.0 / iterations as f64,
        allocs as f64 / iterations as f64,
        bytes / iterations as u64,
    );
}

fn measure<T>(
    iterations: usize,
    mut run: impl FnMut() -> T,
) -> (std::time::Duration, usize, u64, T) {
    let warmup = run();
    black_box(warmup);

    let count_start = ALLOC_COUNT.load(Ordering::Relaxed);
    let bytes_start = ALLOC_BYTES.load(Ordering::Relaxed);
    let start = Instant::now();
    let mut output = run();
    for _ in 1..iterations {
        output = run();
    }
    let elapsed = start.elapsed();
    let allocs = ALLOC_COUNT.load(Ordering::Relaxed) - count_start;
    let bytes = ALLOC_BYTES.load(Ordering::Relaxed) - bytes_start;
    (elapsed, allocs, bytes, output)
}

fn print_result(
    name: &str,
    iterations: usize,
    elapsed: std::time::Duration,
    allocs: usize,
    bytes: u64,
    output_bytes: usize,
) {
    println!(
        "{name} iterations={iterations} output_bytes={output_bytes} total_ms={:.3} avg_ms={:.3} allocs_per_iter={:.1} alloc_bytes_per_iter={}",
        elapsed.as_secs_f64() * 1_000.0,
        elapsed.as_secs_f64() * 1_000.0 / iterations as f64,
        allocs as f64 / iterations as f64,
        bytes / iterations as u64,
    );
}

fn synthetic_gray(width: u32, height: u32) -> Vec<u8> {
    let mut pixels = Vec::with_capacity(width as usize * height as usize);
    for y in 0..height {
        for x in 0..width {
            pixels.push(((x.wrapping_mul(17) + y.wrapping_mul(31)) & 0xff) as u8);
        }
    }
    pixels
}

fn synthetic_rgba(width: u32, height: u32) -> Vec<u8> {
    let mut pixels = Vec::with_capacity(width as usize * height as usize * 4);
    for y in 0..height {
        for x in 0..width {
            let r = ((x.wrapping_mul(17) + y.wrapping_mul(31)) & 0xff) as u8;
            let g = r.wrapping_add(53);
            let b = r.wrapping_add(109);
            pixels.extend_from_slice(&[r, g, b, 255]);
        }
    }
    pixels
}
