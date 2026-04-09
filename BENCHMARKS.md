# XRayView Benchmark Baseline

Phase 0 baseline measurements for the architecture migration in `MIGRATION.md`.

Recorded on 2026-04-09 17:02:18 CDT.

- Host: Linux 6.19.10-arch1-1 x86_64 GNU/Linux
- CPU: 13th Gen Intel(R) Core(TM) i5-13400

## Status

- Recorded: backend microbenchmarks, CLI end-to-end timings, `DecodeStudy` calls per workflow, and artifact bytes written.
- Instrumented and ready for manual capture: frontend poll lag and sidecar startup timing.
- Still pending interactive desktop capture: open-study to preview visible, UI poll lag in the real shell, and peak RSS for sidecar and desktop shell.

## Go Benchmarks

Command:

```bash
GOCACHE=/tmp/xrayview-go-build-cache \
GOTMPDIR=/tmp/xrayview-go-tmp \
go -C backend test ./internal/jobs -bench=. -benchmem -count=3
```

| Metric | Median | Raw runs | Bytes/op | Allocs/op |
| --- | --- | --- | --- | --- |
| DecodeStudy wall time | 7.55 ms | 7.61 ms, 7.55 ms, 7.47 ms | 11,144,409 | 184 |
| RenderSourceImage wall time | 3.67 ms | 3.75 ms, 3.61 ms, 3.67 ms | 2,228,240 | 2 |
| ProcessSourceImage wall time | 4.45 ms | 4.12 ms, 4.45 ms, 4.58 ms | 4,456,469 | 3 |
| AnalyzePreview wall time | 315.85 ms | 324.39 ms, 315.85 ms, 314.87 ms | 50,922,440 | 486 |

Raw benchmark output:

```text
BenchmarkDecodeStudy-10            154    7608467 ns/op   11144408 B/op   184 allocs/op
BenchmarkDecodeStudy-10            157    7548439 ns/op   11144409 B/op   184 allocs/op
BenchmarkDecodeStudy-10            146    7465544 ns/op   11144408 B/op   184 allocs/op
BenchmarkRenderSourceImage-10      292    3751219 ns/op    2228240 B/op     2 allocs/op
BenchmarkRenderSourceImage-10      319    3614799 ns/op    2228240 B/op     2 allocs/op
BenchmarkRenderSourceImage-10      351    3665380 ns/op    2228240 B/op     2 allocs/op
BenchmarkProcessSourceImage-10     296    4117603 ns/op    4456468 B/op     3 allocs/op
BenchmarkProcessSourceImage-10     289    4451235 ns/op    4456469 B/op     3 allocs/op
BenchmarkProcessSourceImage-10     244    4577772 ns/op    4456467 B/op     3 allocs/op
BenchmarkAnalyzePreview-10           4  324386520 ns/op   50922444 B/op   486 allocs/op
BenchmarkAnalyzePreview-10           4  315848257 ns/op   50922440 B/op   486 allocs/op
BenchmarkAnalyzePreview-10           4  314870603 ns/op   50922440 B/op   486 allocs/op
```

## CLI Timing

Note: the migration doc uses `images/...`, but with `go -C backend run` the current repo needs `../images/...`.

Commands:

```bash
time env GOCACHE=/tmp/xrayview-go-build-cache GOTMPDIR=/tmp/xrayview-go-tmp \
  go -C backend run ./cmd/xrayview-cli render-preview \
  ../images/sample-dental-radiograph.dcm /tmp/bench-preview.png

time env GOCACHE=/tmp/xrayview-go-build-cache GOTMPDIR=/tmp/xrayview-go-tmp \
  go -C backend run ./cmd/xrayview-cli process-preview \
  ../images/sample-dental-radiograph.dcm /tmp/bench-process.png
```

Results:

- `render-preview`: `real 0m0.261s`, `user 0m0.306s`, `sys 0m0.051s`
- `process-preview`: `real 0m0.262s`, `user 0m0.309s`, `sys 0m0.056s`

## Backend Workflow Baseline

Workflow driver:

```bash
env GOCACHE=/tmp/xrayview-go-build-cache \
GOTMPDIR=/tmp/xrayview-go-tmp \
XRAYVIEW_BACKEND_BASE_DIR=/tmp/xrayview-phase0 \
XRAYVIEW_BACKEND_PORT=38182 \
XRAYVIEW_BENCH_LOG_DECODES=1 \
go -C backend run ./cmd/xrayviewd
```

Measured against one open/render/process/analyze workflow on `images/sample-dental-radiograph.dcm`:

- DecodeStudy calls per workflow: 3
- Decode sequence by job kind: render = 1, process = 2, analyze = 3
- Artifact bytes written under `/tmp/xrayview-phase0/cache/artifacts`: 3,523,516 bytes (`du -sh`: `3.4M`)
- Render preview artifact: 858,509 bytes
- Process preview artifact: 1,806,498 bytes
- Analyze preview artifact: 858,509 bytes
- Processed DICOM output at `/tmp/xrayview-phase0-output.dcm`: 6,685,548 bytes

## Temporary Instrumentation

- Frontend poll lag: open the app, trigger a render/process/analyze job, and watch the devtools console for `[bench] <jobId> visible in ...ms`.
- DecodeStudy workflow counts: run the backend with `XRAYVIEW_BENCH_LOG_DECODES=1`; each job executor logs `workflow_decode_count`.
- Sidecar startup time: run the desktop shell with `XRAYVIEW_BENCH_LOG_SIDECAR_STARTUP=1`; `EnsureStarted` logs duration to stderr.

## Remaining Manual Measurements

- Open-study -> preview visible
- Job completion -> UI update
- Peak RSS (sidecar)
- Peak RSS (desktop shell)
- Sidecar startup time
