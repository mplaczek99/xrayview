import { useMemo, useState } from "react";
import { workbenchActions, useWorkbenchStore } from "../../app/store/workbenchStore";
import {
  buildProcessingArgs,
  FALLBACK_PROCESSING_MANIFEST,
  formatBackendError,
} from "../../lib/backend";
import type {
  PaletteName as Palette,
  ProcessingControls,
} from "../../lib/generated/contracts";
import type { ProcessingRequest } from "../../lib/types";
import {
  buildProcessingUiState,
  processingControlsEqual,
} from "../../features/processing/presets";
import {
  createProcessingForm,
  DEFAULT_PIPELINE,
} from "../../features/study/model";
import { DicomViewer } from "../viewer/DicomViewer";

const CUSTOM_PRESET_ID = "__custom";

function formatArgPreview(args: readonly string[]): string {
  return args
    .map((arg) => (/[\s"'\\]/.test(arg) ? JSON.stringify(arg) : arg))
    .join(" ");
}

function useActiveStudy() {
  return useWorkbenchStore((state) =>
    state.activeStudyId ? state.studies[state.activeStudyId] ?? null : null,
  );
}

export function ProcessingTab() {
  const study = useActiveStudy();
  const manifest = useWorkbenchStore((state) => state.manifest);
  const processingUi = useMemo(() => buildProcessingUiState(manifest), [manifest]);
  const [pipelineOpen, setPipelineOpen] = useState(false);
  const defaultPreset =
    manifest.presets.find((preset) => preset.id === manifest.defaultPresetId) ??
    manifest.presets[0] ??
    FALLBACK_PROCESSING_MANIFEST.presets[0];
  const form = study?.processing.form ?? createProcessingForm(processingUi.defaultControls);
  const runStatus = study?.processing.runStatus ?? { state: "idle" as const };
  const activePreset =
    processingUi.presets.find((preset) =>
      processingControlsEqual(preset.controls, form.controls),
    ) ?? null;
  const commandPreset = activePreset
    ? {
        id: activePreset.id,
        controls: activePreset.controls,
      }
    : defaultPreset;
  const request: ProcessingRequest = {
    controls: form.controls,
    compare: form.compare,
    outputPath: form.outputPath,
    pipeline: form.pipeline,
    preset: commandPreset,
  };
  const previewUrl = study?.originalPreview?.previewUrl ?? null;
  const processedPreviewUrl = study?.processing.output?.previewUrl ?? null;
  const busy = runStatus.state === "running" || runStatus.state === "cancelling";
  const isRunning = runStatus.state === "running";
  const isCancelling = runStatus.state === "cancelling";
  const canRun = Boolean(study) && !busy;
  const args = study ? buildProcessingArgs(study.inputPath, request) : [];

  function updateControls(nextControls: ProcessingControls) {
    workbenchActions.setProcessingControls(nextControls);
  }

  function updateControl<K extends keyof ProcessingControls>(
    key: K,
    value: ProcessingControls[K],
  ) {
    updateControls({
      ...form.controls,
      [key]: value,
    });
  }

  function movePipelineStep(index: number, direction: -1 | 1) {
    const target = index + direction;
    if (target < 0 || target >= form.pipeline.length) {
      return;
    }

    const next = [...form.pipeline];
    [next[index], next[target]] = [next[target], next[index]];
    workbenchActions.setProcessingPipeline(next);
  }

  return (
    <div className="processing-tab">
      <div className="processing-tab__preview">
        <DicomViewer
          previewUrl={previewUrl}
          emptyTitle="No image loaded"
          emptyDescription="Load a DICOM file in the View tab first."
        />
      </div>

      <div className="processing-tab__form">
        <section className="form-section">
          <label className="form-label">Save Destination</label>
          <p className="form-hint u-mono">
            {form.outputPath ??
              "No save destination selected. Processing will keep the DICOM in an app-managed temp path until you choose one."}
          </p>
          <div className="form-field">
            <button
              className="button button--ghost"
              type="button"
              onClick={() => void workbenchActions.pickProcessingOutputPath()}
              disabled={!study || busy}
            >
              {form.outputPath ? "Change Save Location" : "Choose Save Location"}
            </button>
            {form.outputPath && (
              <button
                className="button button--ghost"
                type="button"
                onClick={() => workbenchActions.setProcessingOutputPath(null)}
                disabled={busy}
              >
                Clear
              </button>
            )}
          </div>
        </section>

        <section className="form-section">
          <label className="form-label" htmlFor="proc-preset">
            Preset
          </label>
          <select
            id="proc-preset"
            className="form-select"
            value={activePreset?.id ?? CUSTOM_PRESET_ID}
            onChange={(event) => {
              const preset = processingUi.presets.find(
                (candidate) => candidate.id === event.target.value,
              );
              if (!preset) {
                return;
              }

              updateControls({ ...preset.controls });
            }}
            disabled={busy}
          >
            {processingUi.presets.map((preset) => (
              <option key={preset.id} value={preset.id}>
                {preset.label}
              </option>
            ))}
            {!activePreset && (
              <option value={CUSTOM_PRESET_ID}>Custom</option>
            )}
          </select>
          <p className="form-hint">
            {activePreset
              ? activePreset.description
              : "Custom controls. The backend will use the default preset plus the explicit overrides shown below."}
          </p>
        </section>

        <section className="form-section">
          <div className="form-label">Grayscale Controls</div>

          <label className="form-toggle">
            <input
              type="checkbox"
              checked={form.controls.invert}
              onChange={(event) => updateControl("invert", event.target.checked)}
              disabled={busy}
            />
            <span>Invert</span>
          </label>

          <div className="form-field">
            <label className="form-field__label" htmlFor="proc-brightness">
              Brightness
            </label>
            <input
              id="proc-brightness"
              className="form-input form-input--number"
              type="number"
              value={form.controls.brightness}
              step={1}
              onChange={(event) =>
                updateControl(
                  "brightness",
                  parseInt(event.target.value, 10) || 0,
                )
              }
              disabled={busy}
            />
          </div>

          <div className="form-field">
            <label className="form-field__label" htmlFor="proc-contrast">
              Contrast
            </label>
            <input
              id="proc-contrast"
              className="form-input form-input--number"
              type="number"
              value={form.controls.contrast}
              step={0.1}
              min={0}
              onChange={(event) =>
                updateControl(
                  "contrast",
                  parseFloat(event.target.value) || 0,
                )
              }
              disabled={busy}
            />
          </div>

          <label className="form-toggle">
            <input
              type="checkbox"
              checked={form.controls.equalize}
              onChange={(event) =>
                updateControl("equalize", event.target.checked)
              }
              disabled={busy}
            />
            <span>Equalize</span>
          </label>
        </section>

        <section className="form-section">
          <label className="form-label" htmlFor="proc-palette">
            Palette
          </label>
          <select
            id="proc-palette"
            className="form-select"
            value={form.controls.palette}
            onChange={(event) =>
              updateControl("palette", event.target.value as Palette)
            }
            disabled={busy}
          >
            <option value="none">none</option>
            <option value="hot">hot</option>
            <option value="bone">bone</option>
          </select>
        </section>

        <section className="form-section">
          <label className="form-toggle">
            <input
              type="checkbox"
              checked={form.compare}
              onChange={(event) =>
                workbenchActions.setProcessingCompare(event.target.checked)
              }
              disabled={busy}
            />
            <span>Compare</span>
          </label>
          <p className="form-hint">
            Write side-by-side comparison output into the processed DICOM.
          </p>
        </section>

        <section className="form-section">
          <button
            className="form-collapse-toggle"
            type="button"
            onClick={() => setPipelineOpen((current) => !current)}
          >
            <span
              className={`form-collapse-arrow${pipelineOpen ? " form-collapse-arrow--open" : ""}`}
            >
              &#9654;
            </span>
            Advanced: Pipeline Order
          </button>

          {pipelineOpen && (
            <div className="pipeline-editor">
              <ul className="pipeline-list">
                {form.pipeline.map((step, index) => (
                  <li key={step} className="pipeline-item">
                    <span className="pipeline-item__name">{step}</span>
                    <div className="pipeline-item__actions">
                      <button
                        className="pipeline-btn"
                        type="button"
                        onClick={() => movePipelineStep(index, -1)}
                        disabled={index === 0 || busy}
                        aria-label={`Move ${step} up`}
                      >
                        &#9650;
                      </button>
                      <button
                        className="pipeline-btn"
                        type="button"
                        onClick={() => movePipelineStep(index, 1)}
                        disabled={index === form.pipeline.length - 1 || busy}
                        aria-label={`Move ${step} down`}
                      >
                        &#9660;
                      </button>
                    </div>
                  </li>
                ))}
              </ul>
              <p className="form-hint">
                `grayscale` is always the starting point. Pseudocolor runs after
                the grayscale pipeline.
              </p>
              {form.pipeline.some((step, index) => step !== DEFAULT_PIPELINE[index]) && (
                <button
                  className="button button--ghost pipeline-reset"
                  type="button"
                  onClick={() =>
                    workbenchActions.setProcessingPipeline([...DEFAULT_PIPELINE])
                  }
                  disabled={busy}
                >
                  Reset to default order
                </button>
              )}
            </div>
          )}
        </section>

        {study && args.length > 0 && (
          <section className="form-section">
            <div className="form-label">Command Preview</div>
            <pre className="args-preview u-mono">
              xrayview {formatArgPreview(args)}
            </pre>
          </section>
        )}

        <div className="processing-tab__actions">
          <button
            className="button button--primary processing-run-btn"
            type="button"
            onClick={() => void workbenchActions.runActiveStudyProcessing(request)}
            disabled={!canRun}
          >
            {busy ? (
              <>
                <span className="spinner" aria-hidden="true" />
                {isCancelling ? "Cancelling..." : "Processing..."}
              </>
            ) : (
              "Run Processing"
            )}
          </button>

          {runStatus.state === "running" && (
            <button
              className="button button--ghost"
              type="button"
              onClick={() => void workbenchActions.cancelJob(runStatus.jobId)}
            >
              Cancel Job
            </button>
          )}

          {runStatus.state === "success" && (
            <div className="run-status run-status--success">
              {runStatus.fromCache
                ? "Processing loaded from cache."
                : "Processing complete."}
            </div>
          )}
          {runStatus.state === "error" && (
            <div className="run-status run-status--error">
              {formatBackendError(runStatus.error, "Processing failed.")}
            </div>
          )}
          {runStatus.state === "cancelled" && (
            <div className="run-status run-status--error">
              Processing cancelled.
            </div>
          )}
          {busy && (
            <div className="run-status">
              {runStatus.progress.message} ({runStatus.progress.percent}%)
            </div>
          )}
        </div>

        {processedPreviewUrl && (
          <div className="processing-tab__output">
            <DicomViewer previewUrl={processedPreviewUrl} />
            {runStatus.state === "success" && (
              <p className="processing-tab__output-path u-mono">
                {runStatus.outputPath}
              </p>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
