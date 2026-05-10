// Validation benchmark for plan step 33: ProcessingTab should not re-render
// when active-study updates do not affect the processing tab surface.

import { performance } from "node:perf_hooks";

const UPDATE_COUNT = 50_000;
const SAMPLES = 15;

const controls = Object.freeze({
  brightness: 8,
  contrast: 1.2,
  invert: false,
  equalize: true,
  palette: "bone",
});

const form = Object.freeze({
  controls,
  compare: true,
  outputPath: "/tmp/processed.dcm",
});

const runStatus = Object.freeze({ state: "idle" });

const preview = Object.freeze({
  previewUrl: "/preview/source.png",
});

const processingOutput = Object.freeze({
  previewUrl: "/preview/processed.png",
});

const processing = Object.freeze({
  form,
  output: processingOutput,
  runStatus,
});

const baseStudy = Object.freeze({
  studyId: "study-1",
  originalPreview: preview,
  processing,
  status: "Study loaded.",
  renderJobId: "render-1",
  analysisJobId: "analysis-1",
});

const manifest = Object.freeze({
  defaultPresetId: "preset-0",
  presets: Array.from({ length: 24 }, (_, index) =>
    Object.freeze({
      id: `preset-${index}`,
      controls: Object.freeze({
        brightness: index === 3 ? controls.brightness : index - 8,
        contrast: index === 3 ? controls.contrast : 0.8 + index * 0.05,
        invert: index === 3 ? controls.invert : index % 2 === 0,
        equalize: index === 3 ? controls.equalize : index % 3 === 0,
        palette: index === 3 ? controls.palette : index % 2 === 0 ? "none" : "hot",
      }),
    })),
});

const processingUi = Object.freeze({
  defaultControls: manifest.presets[0].controls,
  presets: manifest.presets.map((preset) =>
    Object.freeze({
      id: preset.id,
      label: preset.id.toUpperCase(),
      description: `Preset ${preset.id}`,
      controls: preset.controls,
    }),
  ),
});

const states = Array.from({ length: UPDATE_COUNT }, (_, index) => {
  const activeStudy = Object.freeze({
    ...baseStudy,
    status: `Render progress ${index}`,
    renderJobId: `render-${index}`,
  });
  return Object.freeze({
    activeStudyId: activeStudy.studyId,
    studies: Object.freeze({ [activeStudy.studyId]: activeStudy }),
    manifest,
  });
});

function processingControlsEqual(left, right) {
  return (
    left.brightness === right.brightness &&
    Math.abs(left.contrast - right.contrast) < 0.05 &&
    left.invert === right.invert &&
    left.equalize === right.equalize &&
    left.palette === right.palette
  );
}

function selectActiveStudyBefore(state) {
  return state.activeStudyId ? state.studies[state.activeStudyId] ?? null : null;
}

function makeSelectProcessingTabStudy() {
  let lastStudyId = null;
  let lastForm = null;
  let lastRunStatus = null;
  let lastOriginalPreviewUrl = null;
  let lastProcessedPreviewUrl = null;
  let lastResult = null;
  let initialized = false;

  return (state) => {
    const study = state.activeStudyId ? state.studies[state.activeStudyId] ?? null : null;
    const studyId = study?.studyId ?? null;
    const nextForm = study?.processing.form ?? null;
    const nextRunStatus = study?.processing.runStatus ?? null;
    const originalPreviewUrl = study?.originalPreview?.previewUrl ?? null;
    const processedPreviewUrl = study?.processing.output?.previewUrl ?? null;

    if (
      initialized &&
      studyId === lastStudyId &&
      Object.is(nextForm, lastForm) &&
      Object.is(nextRunStatus, lastRunStatus) &&
      originalPreviewUrl === lastOriginalPreviewUrl &&
      processedPreviewUrl === lastProcessedPreviewUrl
    ) {
      return lastResult;
    }

    initialized = true;
    lastStudyId = studyId;
    lastForm = nextForm;
    lastRunStatus = nextRunStatus;
    lastOriginalPreviewUrl = originalPreviewUrl;
    lastProcessedPreviewUrl = processedPreviewUrl;
    lastResult = study && nextForm && nextRunStatus
      ? {
          studyId,
          form: nextForm,
          runStatus: nextRunStatus,
          originalPreviewUrl,
          processedPreviewUrl,
        }
      : null;
    return lastResult;
  };
}

function renderFromActiveStudy(study) {
  return renderProcessingTab({
    studyId: study.studyId,
    form: study.processing.form,
    runStatus: study.processing.runStatus,
    originalPreviewUrl: study.originalPreview?.previewUrl ?? null,
    processedPreviewUrl: study.processing.output?.previewUrl ?? null,
  });
}

function renderProcessingTab(study) {
  const activePreset = processingUi.presets.find((preset) =>
    processingControlsEqual(preset.controls, study.form.controls),
  ) ?? null;
  const defaultPreset =
    manifest.presets.find((preset) => preset.id === manifest.defaultPresetId) ??
    manifest.presets[0];
  const baselinePreset = activePreset ?? defaultPreset;
  const request = {
    controls: study.form.controls,
    compare: study.form.compare,
    outputPath: study.form.outputPath,
    presetId: baselinePreset.id,
    presetControls: baselinePreset.controls,
  };

  let checksum = study.studyId.length + (study.processedPreviewUrl?.length ?? 0);
  checksum += request.presetId.length + (request.outputPath?.length ?? 0);
  checksum += request.controls.brightness + Math.round(request.controls.contrast * 10);
  checksum += request.controls.invert ? 11 : 0;
  checksum += request.controls.equalize ? 13 : 0;
  checksum += request.compare ? 17 : 0;

  for (const preset of processingUi.presets) {
    checksum += preset.id.length + preset.label.length + preset.description.length;
    checksum += preset.controls.brightness + Math.round(preset.controls.contrast * 100);
  }

  return checksum;
}

function benchmark(label, runner) {
  const samples = [];
  let renderCount = 0;
  let checksum = 0;
  for (let sample = 0; sample < SAMPLES; sample++) {
    const started = performance.now();
    const result = runner();
    samples.push(performance.now() - started);
    renderCount += result.renderCount;
    checksum += result.checksum;
  }
  const mean = samples.reduce((total, sample) => total + sample, 0) / samples.length;
  return {
    label,
    mean,
    renderCount: renderCount / SAMPLES,
    checksum,
  };
}

function runBefore() {
  let lastSnapshot = Symbol("initial");
  let renderCount = 0;
  let checksum = 0;

  for (const state of states) {
    const snapshot = selectActiveStudyBefore(state);
    if (!Object.is(snapshot, lastSnapshot)) {
      checksum += renderFromActiveStudy(snapshot);
      renderCount++;
      lastSnapshot = snapshot;
    }
  }

  return { renderCount, checksum };
}

function runAfter() {
  const selectProcessingTabStudy = makeSelectProcessingTabStudy();
  let lastSnapshot = Symbol("initial");
  let renderCount = 0;
  let checksum = 0;

  for (const state of states) {
    const snapshot = selectProcessingTabStudy(state);
    if (!Object.is(snapshot, lastSnapshot)) {
      checksum += renderProcessingTab(snapshot);
      renderCount++;
      lastSnapshot = snapshot;
    }
  }

  return { renderCount, checksum };
}

const selector = makeSelectProcessingTabStudy();
const first = selector(states[0]);
const second = selector(states[1]);
if (first !== second) {
  throw new Error("selector did not preserve reference across unrelated active-study updates");
}

const changedProcessing = selector({
  ...states[0],
  studies: {
    "study-1": {
      ...states[0].studies["study-1"],
      processing: {
        ...states[0].studies["study-1"].processing,
        form: {
          ...states[0].studies["study-1"].processing.form,
          compare: false,
        },
      },
    },
  },
});
if (changedProcessing === first) {
  throw new Error("selector did not publish a new reference after processing form changed");
}

const before = benchmark("before active-study subscription", runBefore);
const after = benchmark("after processing-tab selector", runAfter);
if (after.renderCount >= before.renderCount) {
  throw new Error(`render count did not improve: before=${before.renderCount}, after=${after.renderCount}`);
}
if (before.checksum <= 0 || after.checksum <= 0) {
  throw new Error("benchmark checksum failed");
}

const speedup = before.mean / after.mean;
const timeSaved = before.mean - after.mean;
const renderReduction = ((before.renderCount - after.renderCount) / before.renderCount) * 100;
const formatMs = (value) => `${value.toFixed(3)} ms`;

console.log(
  `ProcessingTab rerender benchmark (${UPDATE_COUNT} unrelated active-study updates/sample, ${SAMPLES} samples)`,
);
console.log(
  `${before.label}: ${formatMs(before.mean)}/sample, ${before.renderCount.toFixed(0)} renders/sample`,
);
console.log(
  `${after.label}: ${formatMs(after.mean)}/sample, ${after.renderCount.toFixed(0)} renders/sample`,
);
console.log(
  `speedup time: ${speedup.toFixed(2)}x, ${formatMs(timeSaved)} faster per ${UPDATE_COUNT} updates`,
);
console.log(`render reduction: ${renderReduction.toFixed(2)}%`);
