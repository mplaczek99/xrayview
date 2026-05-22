import { selectActiveStudy, selectJobs, selectStudies } from "./store/selectors";
import type { WorkbenchState } from "../features/study/model";
import {
  annotationSourceLabel,
  formatLineMeasurement,
  formatSecondaryMeasurement,
  type ViewerTool,
} from "../features/annotations/tools";
import type { JobSnapshot } from "../features/jobs/model";
import { describeProgress } from "../features/jobs/progressFormatting";
import {
  buildProcessingUiState,
  processingControlsEqual,
} from "../features/processing/presets";
import { createProcessingForm } from "../features/study/model";
import { formatBackendError } from "../lib/backendErrors";
import type {
  AnnotationBundle,
  LineAnnotation,
  MeasurementScale,
} from "../lib/generated/contracts";
import type { ActiveTab } from "../lib/types";
import type { ViewerImageSize } from "../features/viewer/viewport";

const CUSTOM_PRESET_ID = "__custom";

export type CompareView = "original" | "processed" | "split";

export interface HtmxUiState {
  activeTab: ActiveTab;
  compareView: CompareView;
  jobsExpanded: boolean;
  dismissedJobIds: ReadonlySet<string>;
}

export interface ViewerRenderModel {
  previewUrl: string | null;
  viewportResetKey: string | null;
  imageSize: ViewerImageSize | null;
  annotations: AnnotationBundle;
  selectedAnnotationId: string | null;
  tool: ViewerTool;
}

export function escapeHtml(value: unknown): string {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

function attr(value: unknown): string {
  return escapeHtml(value);
}

function checked(value: boolean): string {
  return value ? " checked" : "";
}

function disabled(value: boolean): string {
  return value ? " disabled" : "";
}

function selected(value: boolean): string {
  return value ? " selected" : "";
}

function pressed(value: boolean): string {
  return value ? "true" : "false";
}

function renderStatusIcon(status: string): string {
  if (/cancelled|canceled/i.test(status)) {
    return `
      <svg class="status-bar__icon" viewBox="0 0 16 16" fill="none" aria-hidden="true">
        <circle cx="8" cy="8" r="5.5" stroke="currentColor" stroke-width="1.5"></circle>
        <path d="M5.5 5.5l5 5M10.5 5.5l-5 5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"></path>
      </svg>
    `;
  }
  if (/fail|error/i.test(status)) {
    return `
      <svg class="status-bar__icon status-bar__icon--error" viewBox="0 0 16 16" fill="none" aria-hidden="true">
        <path d="M8 1.5l6.5 12H1.5L8 1.5z" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round"></path>
        <path d="M8 6.5v3M8 11.5v.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"></path>
      </svg>
    `;
  }
  if (/loaded|complete|success/i.test(status)) {
    return `
      <svg class="status-bar__icon status-bar__icon--success" viewBox="0 0 16 16" fill="none" aria-hidden="true">
        <path d="M3.5 8.5l3 3 6-7" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"></path>
      </svg>
    `;
  }
  if (/opening|running|analyzing|processing|cancelling/i.test(status)) {
    return `<span class="status-bar__spinner" aria-hidden="true"></span>`;
  }
  return `
    <svg class="status-bar__icon" viewBox="0 0 16 16" fill="none" aria-hidden="true">
      <circle cx="8" cy="8" r="5.5" stroke="currentColor" stroke-width="1.5"></circle>
      <path d="M8 5v3l2 2" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"></path>
    </svg>
  `;
}

function renderTabs(activeTab: ActiveTab): string {
  const tabs: ActiveTab[] = ["view", "processing"];
  return `
    <nav class="tab-bar" role="tablist" aria-label="Main tabs">
      ${tabs.map((tab) => `
        <button
          id="tab-${tab}"
          class="tab-bar__tab${activeTab === tab ? " tab-bar__tab--active" : ""}"
          type="button"
          role="tab"
          tabindex="${activeTab === tab ? "0" : "-1"}"
          aria-selected="${activeTab === tab ? "true" : "false"}"
          aria-controls="tabpanel-${tab}"
          data-action="set-tab"
          data-tab="${tab}"
          hx-swap="innerHTML"
        >${tab === "view" ? "View" : "Processing"}</button>
      `).join("")}
    </nav>
  `;
}

export function selectViewerRenderModel(state: WorkbenchState): ViewerRenderModel {
  const study = selectActiveStudy(state);
  const analysisPreview = study?.analysisPreview ?? null;
  const originalPreview = study?.originalPreview ?? null;
  const analysisOverlayMode = study?.viewer.analysisOverlayMode ?? "outline";
  const previewUrl = analysisPreview
    ? analysisOverlayMode === "sections"
      ? analysisPreview.filledPreviewUrl
      : analysisPreview.previewUrl
    : originalPreview?.previewUrl ?? null;

  return {
    previewUrl,
    viewportResetKey: study?.studyId ?? null,
    imageSize: (analysisPreview ?? originalPreview)?.imageSize ?? null,
    annotations: study?.annotations ?? { lines: [], rectangles: [], polylines: [] },
    selectedAnnotationId: study?.viewer.selectedAnnotationId ?? null,
    tool: study?.viewer.tool ?? "pan",
  };
}

function renderEmptyView(isOpeningStudy: boolean): string {
  return `
    <div class="view-tab">
      <h2 class="sr-only">View</h2>
      <div class="empty-state">
        ${isOpeningStudy ? `
          <span class="empty-state__loader" aria-hidden="true"></span>
          <h3 class="empty-state__title">Opening study...</h3>
          <p class="empty-state__copy">Loading and rendering the study preview.</p>
        ` : `
          <svg class="empty-state__icon" viewBox="0 0 48 48" fill="none" aria-hidden="true">
            <rect x="6" y="8" width="36" height="32" rx="4" stroke="currentColor" stroke-width="2"></rect>
            <path d="M6 16h36" stroke="currentColor" stroke-width="2"></path>
            <circle cx="18" cy="30" r="5" stroke="currentColor" stroke-width="2"></circle>
            <path d="M30 25l5 5-5 5" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"></path>
          </svg>
          <h3 class="empty-state__title">No study loaded</h3>
          <p class="empty-state__copy">
            Open a bitewing X-ray (BMP) to inspect it, pan and zoom,
            and draw manual line measurements.
          </p>
          <button
            class="button button--primary empty-state__cta"
            type="button"
            data-testid="action-open-study"
            data-action="open-study"
          >Open Study</button>
        `}
      </div>
    </div>
  `;
}

function renderViewerStage(model: ViewerRenderModel): string {
  if (!model.previewUrl) {
    return `
      <div class="viewer-stage viewer-stage--interactive">
        <div class="viewer-placeholder">
          <div class="viewer-placeholder__title">No study loaded</div>
          <p class="viewer-placeholder__copy">
            Open a bitewing X-ray (BMP) to inspect it, pan and zoom,
            or draw a manual line measurement.
          </p>
        </div>
      </div>
    `;
  }

  return `
    <div class="viewer-stage viewer-stage--interactive" data-viewer-stage>
      <div class="viewer-stage__hud">
        <span class="viewer-stage__hud-chip viewer-stage__hud-chip--dist" data-viewer-draft-distance hidden></span>
        <span class="viewer-stage__hud-chip" data-viewer-zoom>100%</span>
        <button
          class="button button--ghost viewer-stage__hud-button"
          type="button"
          data-action="reset-viewer"
        >Reset view</button>
      </div>
      <div class="viewer-canvas viewer-canvas--${attr(model.tool)}" data-viewer-canvas>
        <img
          class="viewer-canvas__image"
          src="${attr(model.previewUrl)}"
          alt="X-ray preview"
          draggable="false"
          data-viewer-image
        />
        <div data-annotation-layer></div>
      </div>
    </div>
  `;
}

function selectedLine(
  annotations: AnnotationBundle,
  selectedAnnotationId: string | null,
): LineAnnotation | null {
  return annotations.lines.find((line) => line.id === selectedAnnotationId) ?? null;
}

function renderViewSidebar(
  annotations: AnnotationBundle,
  selectedAnnotationId: string | null,
  line: LineAnnotation | null,
  measurementScale: MeasurementScale | null,
): string {
  const annotationListClassName =
    annotations.lines.length > 4
      ? "annotation-list annotation-list--scrollable"
      : "annotation-list";

  return `
    <aside class="study-layout__sidebar">
      <section class="measurement-card">
        <h3 class="measurement-card__eyebrow">Viewer Tools</h3>
        <p class="measurement-card__copy">
          Drag to pan, use the mouse wheel to zoom, then switch to Measure
          line to place calibrated annotations in source pixel space.
        </p>
        ${line ? `
          <div class="measurement-card__hero">
            <span class="measurement-card__hero-label">Selected line</span>
            <span class="measurement-card__hero-value">${escapeHtml(formatLineMeasurement(line.measurement))}</span>
          </div>
        ` : ""}
      </section>

      <section class="measurement-card">
        <h3 class="measurement-card__eyebrow">Line Annotations</h3>
        ${annotations.lines.length ? `
          <div class="${annotationListClassName}">
            ${annotations.lines.map((annotation) => `
              <button
                class="annotation-list__item${annotation.id === selectedAnnotationId ? " annotation-list__item--selected" : ""}"
                type="button"
                aria-label="${attr(annotation.label)}"
                aria-pressed="${pressed(annotation.id === selectedAnnotationId)}"
                data-action="select-annotation"
                data-annotation-id="${attr(annotation.id)}"
              >
                <span class="annotation-list__title">${escapeHtml(annotation.label)}</span>
                <span class="annotation-list__meta">${escapeHtml(annotationSourceLabel(annotation.source))}</span>
                <span class="annotation-list__value">${escapeHtml(formatLineMeasurement(annotation.measurement))}</span>
                ${formatSecondaryMeasurement(annotation.measurement) ? `
                  <span class="annotation-list__subvalue">${escapeHtml(formatSecondaryMeasurement(annotation.measurement))}</span>
                ` : ""}
              </button>
            `).join("")}
          </div>
        ` : `
          <p class="measurement-card__copy">
            No manual line annotations yet. Draw one to store a measurement.
          </p>
        `}
      </section>

      ${measurementScale ? `
        <section class="measurement-card">
          <h3 class="measurement-card__eyebrow">Calibration</h3>
          <div class="measurement-card__hero">
            <span class="measurement-card__hero-label">Source</span>
            <span class="measurement-card__hero-value">${escapeHtml(measurementScale.source)}</span>
          </div>
          <p class="measurement-card__copy">
            Row ${measurementScale.rowSpacingMm.toFixed(3)} mm, column
            ${measurementScale.columnSpacingMm.toFixed(3)} mm.
          </p>
        </section>
      ` : ""}
    </aside>
  `;
}

function renderViewTab(state: WorkbenchState): string {
  const study = selectActiveStudy(state);
  if (!study) {
    return renderEmptyView(state.isOpeningStudy);
  }

  const model = selectViewerRenderModel(state);
  const jobs = selectJobs(state);
  const analysisJob = study.analysisJobId ? jobs[study.analysisJobId] ?? null : null;
  const isAnalyzing =
    analysisJob?.state === "queued" ||
    analysisJob?.state === "running" ||
    analysisJob?.state === "cancelling";
  const line = selectedLine(model.annotations, model.selectedAnnotationId);
  const measurementScale =
    study.measurementScale ?? study.originalPreview?.measurementScale ?? null;
  const analysisOverlayMode = study.viewer.analysisOverlayMode;

  return `
    <div class="view-tab">
      <h2 class="sr-only">View</h2>
      <div class="view-panel__toolbar">
        <div class="toolbar-group">
          <button
            class="button button--primary"
            type="button"
            data-testid="action-open-study"
            data-action="open-study"
            ${disabled(state.isOpeningStudy)}
          >
            <svg class="button__icon" viewBox="0 0 16 16" fill="none" aria-hidden="true">
              <path d="M2 3a1 1 0 011-1h4l2 2h4a1 1 0 011 1v7a1 1 0 01-1 1H3a1 1 0 01-1-1V3z" stroke="currentColor" stroke-width="1.5"></path>
            </svg>
            ${state.isOpeningStudy ? "Opening..." : "Open Study"}
          </button>
        </div>

        <span class="toolbar-divider" aria-hidden="true"></span>

        <div class="toolbar-group">
          <button
            class="button button--ghost${model.tool === "pan" ? " viewer-tool--active" : ""}"
            type="button"
            data-testid="action-tool-pan"
            data-action="set-viewer-tool"
            data-tool="pan"
          >
            <svg class="button__icon" viewBox="0 0 16 16" fill="none" aria-hidden="true">
              <path d="M8 1v3M8 12v3M1 8h3M12 8h3M3.5 3.5l2 2M10.5 10.5l2 2M3.5 12.5l2-2M10.5 5.5l2-2" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"></path>
            </svg>
            Pan
          </button>
          <button
            class="button button--ghost${model.tool === "measureLine" ? " viewer-tool--active" : ""}"
            type="button"
            data-testid="action-tool-measure-line"
            data-action="set-viewer-tool"
            data-tool="measureLine"
          >
            <svg class="button__icon" viewBox="0 0 16 16" fill="none" aria-hidden="true">
              <path d="M2 14L14 2" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"></path>
              <path d="M2 14v-4M2 14h4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"></path>
              <path d="M14 2v4M14 2h-4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"></path>
            </svg>
            Measure line
          </button>
        </div>

        <span class="toolbar-divider" aria-hidden="true"></span>

        <div class="toolbar-group">
          <button
            class="button button--ghost"
            type="button"
            data-testid="action-measure-teeth"
            data-action="analyze-study"
            ${disabled(!study.originalPreview || isAnalyzing)}
          >
            <svg class="button__icon" viewBox="0 0 16 16" fill="none" aria-hidden="true">
              <circle cx="8" cy="8" r="6" stroke="currentColor" stroke-width="1.5"></circle>
              <path d="M8 5v6M5 8h6" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"></path>
            </svg>
            ${isAnalyzing ? "Analyzing..." : "Analyze"}
          </button>
          ${study.analysisPreview ? `
            <div class="analysis-toggle" role="group" aria-label="Analysis overlay">
              <button
                class="analysis-toggle__btn${analysisOverlayMode === "outline" ? " analysis-toggle__btn--active" : ""}"
                type="button"
                data-testid="action-analysis-outline"
                aria-pressed="${pressed(analysisOverlayMode === "outline")}"
                data-action="set-analysis-overlay"
                data-mode="outline"
              >Outline</button>
              <button
                class="analysis-toggle__btn${analysisOverlayMode === "sections" ? " analysis-toggle__btn--active" : ""}"
                type="button"
                data-testid="action-analysis-sections"
                aria-pressed="${pressed(analysisOverlayMode === "sections")}"
                data-action="set-analysis-overlay"
                data-mode="sections"
              >Sections</button>
            </div>
          ` : ""}
          <button
            class="button button--ghost"
            type="button"
            data-testid="action-remove-annotation"
            data-action="remove-annotation"
            ${disabled(!model.selectedAnnotationId)}
          >
            <svg class="button__icon" viewBox="0 0 16 16" fill="none" aria-hidden="true">
              <path d="M5 2h6M3 4h10M4 4l1 9a1 1 0 001 1h4a1 1 0 001-1l1-9" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"></path>
            </svg>
            Remove selected
          </button>
        </div>

        ${model.previewUrl ? `<span class="view-panel__filename u-mono">${escapeHtml(study.inputName)}</span>` : ""}
      </div>

      <div class="study-layout">
        <div class="study-layout__viewer">${renderViewerStage(model)}</div>
        ${renderViewSidebar(model.annotations, model.selectedAnnotationId, line, measurementScale)}
      </div>
    </div>
  `;
}

function renderXrayViewer(
  previewUrl: string | null,
  emptyTitle = "No image loaded",
  emptyDescription = "Open a bitewing X-ray (BMP) to view it here.",
): string {
  return `
    <div class="viewer-stage">
      ${previewUrl ? `
        <div class="viewer-stage__media">
          <img
            class="viewer-stage__image"
            src="${attr(previewUrl)}"
            alt="X-ray preview"
            draggable="false"
          />
        </div>
      ` : `
        <div class="viewer-placeholder">
          <div class="viewer-placeholder__title">${escapeHtml(emptyTitle)}</div>
          <p class="viewer-placeholder__copy">${escapeHtml(emptyDescription)}</p>
        </div>
      `}
    </div>
  `;
}

function renderGrayscaleControls(
  controls: WorkbenchState["manifest"]["presets"][number]["controls"],
  busy: boolean,
): string {
  return `
    <section class="form-section">
      <div class="form-label">Grayscale Controls</div>

      <label class="form-toggle">
        <input
          type="checkbox"
          data-action="set-processing-control"
          data-control="invert"
          ${checked(controls.invert)}
          ${disabled(busy)}
        />
        <span>Invert</span>
      </label>

      <div class="form-field form-field--slider">
        <label class="form-field__label" for="proc-brightness">Brightness</label>
        <input
          id="proc-brightness-range"
          class="form-range"
          type="range"
          data-testid="action-set-brightness"
          data-action="set-processing-control"
          data-control="brightness"
          min="-100"
          max="100"
          step="1"
          value="${attr(controls.brightness)}"
          ${disabled(busy)}
        />
        <input
          id="proc-brightness"
          class="form-input form-input--number"
          type="number"
          data-action="set-processing-control"
          data-control="brightness"
          min="-100"
          max="100"
          value="${attr(controls.brightness)}"
          step="1"
          ${disabled(busy)}
        />
      </div>

      <div class="form-field form-field--slider">
        <label class="form-field__label" for="proc-contrast">Contrast</label>
        <input
          id="proc-contrast-range"
          class="form-range"
          type="range"
          data-testid="action-set-contrast"
          data-action="set-processing-control"
          data-control="contrast"
          min="0.1"
          max="3"
          step="0.1"
          value="${attr(controls.contrast)}"
          ${disabled(busy)}
        />
        <input
          id="proc-contrast"
          class="form-input form-input--number"
          type="number"
          data-action="set-processing-control"
          data-control="contrast"
          value="${attr(controls.contrast)}"
          step="0.1"
          min="0.1"
          max="3"
          ${disabled(busy)}
        />
      </div>

      <label class="form-toggle">
        <input
          type="checkbox"
          data-action="set-processing-control"
          data-control="equalize"
          ${checked(controls.equalize)}
          ${disabled(busy)}
        />
        <span>Equalize</span>
      </label>
    </section>
  `;
}

function renderProcessingTab(state: WorkbenchState, ui: HtmxUiState, nowMs: number): string {
  const activeStudy = selectActiveStudy(state);
  const processingUi = buildProcessingUiState(state.manifest);
  const fallbackForm = createProcessingForm(processingUi.defaultControls);
  const processing = activeStudy?.processing ?? null;
  const form = processing?.form ?? fallbackForm;
  const runStatus = processing?.runStatus ?? { state: "idle" as const };
  const activePreset =
    processingUi.presets.find((preset) =>
      processingControlsEqual(preset.controls, form.controls),
    ) ?? null;
  const previewUrl = activeStudy?.originalPreview?.previewUrl ?? null;
  const processedPreviewUrl = processing?.output?.previewUrl ?? null;
  const busy = runStatus.state === "running" || runStatus.state === "cancelling";
  const progressView = busy
    ? describeProgress(
        {
          state: runStatus.state,
          progress: runStatus.progress,
          timing: runStatus.timing,
          fromCache: false,
        },
        nowMs,
      )
    : null;
  const canRun = Boolean(activeStudy) && !busy;

  return `
    <h2 class="sr-only">Processing</h2>
    <div class="processing-tab">
      <div class="processing-tab__preview">
        ${processedPreviewUrl ? `
          <div class="compare-toggle">
            ${(["original", "processed", "split"] as const).map((view) => `
              <button
                class="compare-toggle__btn${ui.compareView === view ? " compare-toggle__btn--active" : ""}"
                type="button"
                data-action="set-compare-view"
                data-compare-view="${view}"
              >${view === "original" ? "Original" : view === "processed" ? "Processed" : "Split"}</button>
            `).join("")}
          </div>
          ${ui.compareView === "split" ? `
            <div class="compare-split">
              <div class="compare-split__pane">
                <div class="compare-split__label">Original</div>
                ${renderXrayViewer(previewUrl)}
              </div>
              <div class="compare-split__pane">
                <div class="compare-split__label">Processed</div>
                ${renderXrayViewer(processedPreviewUrl)}
              </div>
            </div>
          ` : renderXrayViewer(ui.compareView === "processed" ? processedPreviewUrl : previewUrl)}
        ` : renderXrayViewer(
          previewUrl,
          "No image loaded",
          "Load a bitewing X-ray (BMP) in the View tab first.",
        )}
      </div>

      <div class="processing-tab__form">
        <section class="form-section">
          <label class="form-label" for="proc-preset">Preset</label>
          <select
            id="proc-preset"
            class="form-select"
            data-testid="action-select-preset"
            data-action="set-processing-preset"
            ${disabled(busy)}
          >
            ${processingUi.presets.map((preset) => `
              <option value="${attr(preset.id)}"${selected(activePreset?.id === preset.id)}>
                ${escapeHtml(preset.label)}
              </option>
            `).join("")}
            ${!activePreset ? `<option value="${CUSTOM_PRESET_ID}" selected>Custom</option>` : ""}
          </select>
          <p class="form-hint">
            ${escapeHtml(activePreset
              ? activePreset.description
              : "Custom controls. The backend will use the default preset plus the explicit overrides shown below.")}
          </p>
        </section>

        ${renderGrayscaleControls(form.controls, busy)}

        <section class="form-section">
          <label class="form-label" for="proc-palette">Palette</label>
          <select
            id="proc-palette"
            class="form-select"
            data-testid="action-select-palette"
            data-action="set-processing-control"
            data-control="palette"
            ${disabled(busy)}
          >
            ${["none", "hot", "bone"].map((palette) => `
              <option value="${palette}"${selected(form.controls.palette === palette)}>${palette}</option>
            `).join("")}
          </select>
        </section>

        <section class="form-section">
          <label class="form-toggle">
            <input
              type="checkbox"
              data-action="set-processing-compare"
              ${checked(form.compare)}
              ${disabled(busy)}
            />
            <span>Compare</span>
          </label>
          <p class="form-hint">Generate a side-by-side comparison preview.</p>
        </section>

        <div class="processing-tab__actions">
          <button
            class="button button--primary processing-run-btn"
            type="button"
            data-testid="action-start-process"
            data-action="run-processing"
            ${disabled(!canRun)}
          >
            ${busy ? `
              <span class="spinner" aria-hidden="true"></span>
              ${runStatus.state === "cancelling" ? "Cancelling..." : "Processing..."}
            ` : "Run Processing"}
          </button>

          ${runStatus.state === "running" ? `
            <button
              class="button button--ghost"
              type="button"
              data-testid="action-cancel-process"
              data-action="cancel-job"
              data-job-id="${attr(runStatus.jobId)}"
            >Cancel Job</button>
          ` : ""}

          ${runStatus.state === "success" ? `
            <div class="run-status run-status--success">
              ${runStatus.fromCache ? "Processing loaded from cache." : "Processing complete."}
            </div>
          ` : ""}
          ${runStatus.state === "error" ? `
            <div class="run-status run-status--error">
              ${escapeHtml(formatBackendError(runStatus.error, "Processing failed."))}
            </div>
          ` : ""}
          ${runStatus.state === "cancelled" ? `
            <div class="run-status run-status--error">Processing cancelled.</div>
          ` : ""}
          ${busy ? `
            <div class="run-status">
              <div>${escapeHtml(runStatus.progress.message)}</div>
              ${progressView?.detailLabel ? `
                <div class="run-status__detail">${escapeHtml(progressView.detailLabel)}</div>
              ` : ""}
            </div>
          ` : ""}
        </div>
      </div>
    </div>
  `;
}

function titleForJob(kind: string): string {
  switch (kind) {
    case "renderStudy":
      return "Render Preview";
    case "analyzeStudy":
      return "Analyze Teeth And Bone";
    case "processStudy":
      return "Process Study";
    default:
      return kind;
  }
}

function stateLabel(state: string): string {
  switch (state) {
    case "queued":
      return "Queued";
    case "running":
      return "Running";
    case "cancelling":
      return "Cancelling";
    case "completed":
      return "Completed";
    case "failed":
      return "Failed";
    case "cancelled":
      return "Cancelled";
    default:
      return state;
  }
}

function isTerminal(state: string): boolean {
  return state === "completed" || state === "failed" || state === "cancelled";
}

function renderJobCenter(state: WorkbenchState, ui: HtmxUiState, nowMs: number): string {
  const jobMap = selectJobs(state);
  const studies = selectStudies(state);
  const jobs = state.jobOrder
    .map((jobId) => jobMap[jobId])
    .filter((job): job is JobSnapshot => Boolean(job))
    .filter((job) => !ui.dismissedJobIds.has(job.jobId))
    .slice(0, 6);
  const activeCount = jobs.filter((job) => !isTerminal(job.state)).length;

  if (!jobs.length) {
    return "";
  }

  return `
    <section class="job-center" aria-label="Background jobs">
      <div class="job-center__header">
        <button
          class="job-center__toggle"
          type="button"
          data-action="toggle-jobs"
          aria-expanded="${ui.jobsExpanded ? "true" : "false"}"
        >
          <span class="form-collapse-arrow${ui.jobsExpanded ? " form-collapse-arrow--open" : ""}">&#9654;</span>
          <span class="job-center__eyebrow">Jobs</span>
          ${activeCount > 0 ? `<span class="job-center__badge">${activeCount}</span>` : ""}
        </button>
        ${ui.jobsExpanded && jobs.some((job) => isTerminal(job.state)) ? `
          <button
            class="button button--ghost job-center__clear"
            type="button"
            data-action="clear-terminal-jobs"
          >Clear finished</button>
        ` : ""}
      </div>

      ${ui.jobsExpanded ? `
        <div class="job-center__list">
          ${jobs.map((job) => {
            const studyName = job.studyId ? studies[job.studyId]?.inputName ?? null : null;
            const canCancel = job.state === "queued" || job.state === "running";
            const terminal = isTerminal(job.state);
            const progressView = describeProgress(job, nowMs);
            const message =
              job.state === "failed"
                ? formatBackendError(job.error, "Job failed.")
                : job.progress.message;
            const width = Math.max(job.progress.percent, job.state === "completed" ? 100 : 4);

            return `
              <article
                class="job-card"
                data-testid="job-row"
                data-job-kind="${attr(job.jobKind)}"
                data-job-state="${attr(job.state)}"
              >
                <div class="job-card__row">
                  <div>
                    <div class="job-card__title">${escapeHtml(titleForJob(job.jobKind))}</div>
                    <div
                      class="job-card__meta"
                      data-testid="job-state"
                      data-job-state="${attr(job.state)}"
                    >
                      ${escapeHtml(stateLabel(job.state))}
                      ${studyName ? ` - ${escapeHtml(studyName)}` : ""}
                      ${job.fromCache ? " - cache" : ""}
                    </div>
                  </div>
                  ${canCancel ? `
                    <button
                      class="button button--ghost job-card__cancel"
                      type="button"
                      data-testid="action-cancel-job"
                      data-action="cancel-job"
                      data-job-id="${attr(job.jobId)}"
                    >Cancel</button>
                  ` : terminal ? `
                    <button
                      class="button button--ghost job-card__cancel"
                      type="button"
                      data-action="dismiss-job"
                      data-job-id="${attr(job.jobId)}"
                      aria-label="Dismiss"
                    >Dismiss</button>
                  ` : ""}
                </div>

                <div class="job-card__progress">
                  <div
                    class="job-card__progress-bar job-card__progress-bar--${attr(job.state)}${progressView.indeterminate ? " job-card__progress-bar--indeterminate" : ""}"
                    ${progressView.indeterminate ? "" : `style="width: ${attr(width)}%;"`}
                  ></div>
                </div>
                <p class="job-card__message">${escapeHtml(message)}</p>
                ${progressView.detailLabel ? `
                  <p class="job-card__detail">${escapeHtml(progressView.detailLabel)}</p>
                ` : ""}
              </article>
            `;
          }).join("")}
        </div>
      ` : ""}
    </section>
  `;
}

export function renderApp(state: WorkbenchState, ui: HtmxUiState, nowMs: number): string {
  const tabContent =
    ui.activeTab === "view"
      ? renderViewTab(state)
      : renderProcessingTab(state, ui, nowMs);

  return `
    <div class="app-shell" hx-swap="innerHTML">
      <h1 class="sr-only">XRayView</h1>
      ${renderTabs(ui.activeTab)}

      <main
        id="tabpanel-${attr(ui.activeTab)}"
        class="tab-content"
        role="tabpanel"
        aria-labelledby="tab-${attr(ui.activeTab)}"
      >
        ${tabContent}
      </main>

      <div class="status-bar" role="status" aria-live="polite">
        ${renderStatusIcon(state.workbenchStatus)}
        <span class="status-bar__text">${escapeHtml(state.workbenchStatus)}</span>
      </div>

      ${renderJobCenter(state, ui, nowMs)}
    </div>
  `;
}
