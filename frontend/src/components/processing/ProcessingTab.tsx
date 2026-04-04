import { useMemo, useState } from "react";
import { workbenchActions, useWorkbenchStore, selectActiveStudy, selectManifest } from "../../app/store/workbenchStore";
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
} from "../../features/study/model";
import { DicomViewer } from "../viewer/DicomViewer";
import { GrayscaleControls } from "./GrayscaleControls";
import { PipelineEditor } from "./PipelineEditor";

const CUSTOM_PRESET_ID = "__custom";

function formatArgPreview(args: readonly string[]): string {
  return args
    .map((arg) => (/[\s"'\\]/.test(arg) ? JSON.stringify(arg) : arg))
    .join(" ");
}

export function ProcessingTab() {
  const study = useWorkbenchStore(selectActiveStudy);
  const manifest = useWorkbenchStore(selectManifest);
  const processingUi = useMemo(() => buildProcessingUiState(manifest), [manifest]);
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
  const [compareView, setCompareView] = useState<"original" | "processed" | "split">("processed");
  const busy = runStatus.state === "running" || runStatus.state === "cancelling";
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

  return (
    <>
    <h2 className="sr-only">Processing</h2>
    <div className="processing-tab">
      <div className="processing-tab__preview">
        {processedPreviewUrl ? (
          <>
            <div className="compare-toggle">
              <button
                className={`compare-toggle__btn${compareView === "original" ? " compare-toggle__btn--active" : ""}`}
                type="button"
                onClick={() => setCompareView("original")}
              >
                Original
              </button>
              <button
                className={`compare-toggle__btn${compareView === "processed" ? " compare-toggle__btn--active" : ""}`}
                type="button"
                onClick={() => setCompareView("processed")}
              >
                Processed
              </button>
              <button
                className={`compare-toggle__btn${compareView === "split" ? " compare-toggle__btn--active" : ""}`}
                type="button"
                onClick={() => setCompareView("split")}
              >
                Split
              </button>
            </div>
            {compareView === "split" ? (
              <div className="compare-split">
                <div className="compare-split__pane">
                  <div className="compare-split__label">Original</div>
                  <DicomViewer previewUrl={previewUrl} />
                </div>
                <div className="compare-split__pane">
                  <div className="compare-split__label">Processed</div>
                  <DicomViewer previewUrl={processedPreviewUrl} />
                </div>
              </div>
            ) : (
              <DicomViewer
                previewUrl={compareView === "processed" ? processedPreviewUrl : previewUrl}
              />
            )}
          </>
        ) : (
          <DicomViewer
            previewUrl={previewUrl}
            emptyTitle="No image loaded"
            emptyDescription="Load a DICOM file in the View tab first."
          />
        )}
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

        <GrayscaleControls
          controls={form.controls}
          busy={busy}
          onUpdateControl={updateControl}
        />

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

        <PipelineEditor pipeline={form.pipeline} busy={busy} />

        {study && args.length > 0 && (
          <section className="form-section">
            <div className="form-label">Command Preview</div>
            <pre className="args-preview u-mono">{`xrayview ${formatArgPreview(args)}`}</pre>
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

        {runStatus.state === "success" && runStatus.outputPath && (
          <div className="processing-tab__output">
            <p className="processing-tab__output-path u-mono">
              {runStatus.outputPath}
            </p>
          </div>
        )}
      </div>
    </div>
    </>
  );
}
