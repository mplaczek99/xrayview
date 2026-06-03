import { fileURLToPath } from "node:url";
import { chromium } from "playwright";
import { createServer } from "vite";

const args = new Map();
for (let index = 2; index < process.argv.length; index += 2) {
  args.set(process.argv[index], process.argv[index + 1]);
}

const iterations = Number(args.get("--iterations") ?? 500);
const sampleCount = Number(args.get("--samples") ?? 7);
const annotationCount = Number(args.get("--annotations") ?? 120);

const server = await createServer({
  configFile: false,
  root: fileURLToPath(new URL("..", import.meta.url)),
  server: {
    host: "127.0.0.1",
    port: 0,
  },
  appType: "spa",
});

await server.listen();
const address = server.httpServer?.address();
if (!address || typeof address === "string") {
  throw new Error("Vite did not expose a loopback server address.");
}

const browser = await chromium.launch();
const samples = [];

try {
  for (let sample = 0; sample < sampleCount; sample += 1) {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.goto(`http://127.0.0.1:${address.port}/`);
    samples.push(
      await page.evaluate(
        async ({ annotationCount, iterations }) => {
          const { renderApp } = await import("/src/app/htmxView.ts");
          const { describeProgress } = await import("/src/features/jobs/progressFormatting.ts");

          const now = 1_800_000;
          const timing = {
            startedAtMs: now - 14_000,
            lastUpdatedAtMs: now,
            lastProgressAtMs: now - 600,
            firstMeasuredSample: { atMs: now - 12_000, percent: 5 },
            measuredSampleCount: 5,
            smoothedRate: 0.0048,
            samples: [
              { atMs: now - 12_000, percent: 5 },
              { atMs: now - 9_000, percent: 18 },
              { atMs: now - 5_500, percent: 34 },
              { atMs: now - 2_000, percent: 50 },
              { atMs: now, percent: 61 },
            ],
          };
          const makeJob = (jobId, jobKind, percent, message) => ({
            jobId,
            jobKind,
            studyId: "study-1",
            state: "running",
            progress: {
              percent,
              stage: "working",
              message,
            },
            fromCache: false,
            result: null,
            error: null,
            timing,
          });
          const jobs = {
            "job-analysis": makeJob("job-analysis", "analyzeStudy", 61, "Analyzing structures..."),
            "job-process": makeJob("job-process", "processStudy", 44, "Processing preview..."),
            "job-render": makeJob("job-render", "renderStudy", 88, "Rendering preview..."),
          };
          const annotations = {
            rectangles: [],
            polylines: [],
            lines: Array.from({ length: annotationCount }, (_, index) => ({
              id: `line-${index}`,
              label: `Measurement ${index}`,
              source: "manual",
              editable: true,
              start: { x: 20 + index, y: 30 + index },
              end: { x: 90 + index, y: 80 + index },
              measurement: { pixelLength: 86.02, calibratedLengthMm: null },
            })),
          };
          const state = {
            manifest: {
              defaultPresetId: "default",
              presets: [
                {
                  id: "default",
                  controls: {
                    brightness: 0,
                    contrast: 1,
                    invert: false,
                    equalize: false,
                    palette: "none",
                  },
                },
              ],
            },
            manifestStatus: "ready",
            activeStudyId: "study-1",
            studies: {
              "study-1": {
                studyId: "study-1",
                inputPath: "/tmp/bench.bmp",
                inputName: "bench.bmp",
                measurementScale: null,
                originalPreview: {
                  studyId: "study-1",
                  previewUrl:
                    "data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw==",
                  imageSize: { width: 1200, height: 820 },
                  measurementScale: null,
                  runtime: "mock",
                },
                analysisPreview: null,
                annotations,
                viewer: {
                  tool: "pan",
                  selectedAnnotationId: null,
                  analysisOverlayMode: "outline",
                },
                processing: {
                  form: {
                    controls: {
                      brightness: 0,
                      contrast: 1,
                      invert: false,
                      equalize: false,
                      palette: "none",
                    },
                    compare: false,
                  },
                  output: null,
                  runStatus: {
                    state: "running",
                    jobId: "job-process",
                    progress: jobs["job-process"].progress,
                    timing,
                  },
                },
                runtime: "mock",
                status: "Analyzing structures...",
                renderJobId: "job-render",
                analysisJobId: "job-analysis",
              },
            },
            studyOrder: ["study-1"],
            jobs,
            jobOrder: ["job-analysis", "job-process", "job-render"],
            pendingJobIds: new Set(["job-analysis", "job-process", "job-render"]),
            isOpeningStudy: false,
            workbenchStatus: "Analyzing structures...",
          };
          const ui = {
            activeTab: "view",
            compareView: "processed",
            jobsExpanded: true,
            dismissedJobIds: new Set(),
          };

          function patchDirect(nextNow) {
            const shell = document.querySelector(".app-shell");
            shell.querySelector(".status-bar__text").textContent = state.workbenchStatus;

            const analysisJob = state.jobs["job-analysis"];
            const analysisDetail = `${Math.round(analysisJob.progress.percent)}%`;
            shell
              .querySelector(".analysis-progress")
              .setAttribute("aria-label", `Analyzing... ${analysisDetail}`);
            shell.querySelector(".analysis-progress__text").textContent = "Analyzing...";
            shell.querySelector(".analysis-progress__detail").textContent = analysisDetail;

            for (const job of Object.values(state.jobs)) {
              const card = shell.querySelector(`[data-job-id="${job.jobId}"]`);
              const progress = describeProgress(job, nextNow);
              const bar = card.querySelector("[data-job-progress-bar]");
              bar.className = `job-card__progress-bar job-card__progress-bar--${job.state}${
                progress.indeterminate ? " job-card__progress-bar--indeterminate" : ""
              }`;
              if (progress.indeterminate) {
                bar.removeAttribute("style");
              } else {
                bar.style.width = `${Math.max(job.progress.percent, 4)}%`;
              }
              card.querySelector("[data-job-message]").textContent = job.progress.message;
              card.querySelector("[data-job-detail]").textContent = progress.detailLabel;
            }
          }

          document.body.innerHTML = renderApp(state, ui, now);

          let checksum = 0;
          const fullStartedAt = performance.now();
          for (let index = 0; index < iterations; index += 1) {
            const tickNow = now + index * 1_000;
            const html = renderApp(state, ui, tickNow);
            const template = document.createElement("template");
            template.innerHTML = html.trim();
            checksum += template.content.querySelectorAll(".job-card").length;
          }
          const fullRenderMs = performance.now() - fullStartedAt;

          const patchStartedAt = performance.now();
          for (let index = 0; index < iterations; index += 1) {
            patchDirect(now + index * 1_000);
            checksum += document.querySelectorAll(".job-card").length;
          }
          const fastPatchMs = performance.now() - patchStartedAt;

          return { fullRenderMs, fastPatchMs, checksum };
        },
        { annotationCount, iterations },
      ),
    );
    await page.close();
  }
} finally {
  await browser.close();
  await server.close();
}

const fullValues = samples.map((sample) => sample.fullRenderMs);
const patchValues = samples.map((sample) => sample.fastPatchMs);
const average = (values) => values.reduce((sum, value) => sum + value, 0) / values.length;
const minimum = (values) => Math.min(...values);
const maximum = (values) => Math.max(...values);

console.log(
  JSON.stringify(
    {
      annotationCount,
      iterations,
      sampleCount,
      fullRenderAverageMs: average(fullValues),
      fullRenderMinimumMs: minimum(fullValues),
      fullRenderMaximumMs: maximum(fullValues),
      fastPatchAverageMs: average(patchValues),
      fastPatchMinimumMs: minimum(patchValues),
      fastPatchMaximumMs: maximum(patchValues),
      speedup: average(fullValues) / average(patchValues),
      samples,
    },
    null,
    2,
  ),
);
