import { useEffect, useMemo, useState } from "react";
import {
  FALLBACK_PROCESSING_MANIFEST,
  buildOutputName,
  buildProcessingArgs,
  ensureDicomExtension,
  loadProcessingManifest,
  pickSaveDicomPath,
  runBackendProcess,
} from "../../lib/backend";
import type {
  Palette,
  ProcessingControls,
  ProcessingPipelineStep,
  ProcessingRequest,
} from "../../lib/types";
import { buildProcessingUiState } from "../../features/processing/presets";
import { DicomViewer } from "../viewer/DicomViewer";

const DEFAULT_PIPELINE: ProcessingPipelineStep[] = [
  "grayscale",
  "invert",
  "brightness",
  "contrast",
  "equalize",
];
const CUSTOM_PRESET_ID = "__custom";

interface ProcessingTabProps {
  inputPath: string | null;
  previewUrl: string | null;
}

interface ProcessingForm {
  controls: ProcessingControls;
  outputPath: string | null;
  compare: boolean;
  pipeline: ProcessingPipelineStep[];
}

type RunStatus =
  | { state: "idle" }
  | { state: "running" }
  | { state: "success"; outputPath: string }
  | { state: "error"; message: string };

function createInitialForm(defaultControls: ProcessingControls): ProcessingForm {
  return {
    controls: { ...defaultControls },
    outputPath: null,
    compare: false,
    pipeline: [...DEFAULT_PIPELINE],
  };
}

function describeError(error: unknown): string {
  if (error instanceof Error && error.message.trim()) return error.message;
  if (typeof error === "string" && error.trim()) return error;
  if (error && typeof error === "object" && "message" in error) {
    const message = error.message;
    if (typeof message === "string" && message.trim()) return message;
  }
  return "Processing failed.";
}

function controlsExactlyEqual(
  left: ProcessingControls,
  right: ProcessingControls,
): boolean {
  return (
    left.brightness === right.brightness &&
    left.contrast === right.contrast &&
    left.invert === right.invert &&
    left.equalize === right.equalize &&
    left.palette === right.palette
  );
}

function formatArgPreview(args: readonly string[]): string {
  return args
    .map((arg) => (/[\s"'\\]/.test(arg) ? JSON.stringify(arg) : arg))
    .join(" ");
}

export function ProcessingTab({ inputPath, previewUrl }: ProcessingTabProps) {
  const [manifest, setManifest] = useState(FALLBACK_PROCESSING_MANIFEST);
  const initialUiState = useMemo(
    () => buildProcessingUiState(FALLBACK_PROCESSING_MANIFEST),
    [],
  );
  const [form, setForm] = useState<ProcessingForm>(() =>
    createInitialForm(initialUiState.defaultControls),
  );
  const [runStatus, setRunStatus] = useState<RunStatus>({ state: "idle" });
  const [processedPreviewUrl, setProcessedPreviewUrl] = useState<string | null>(
    null,
  );
  const [pipelineOpen, setPipelineOpen] = useState(false);

  const processingUi = useMemo(() => buildProcessingUiState(manifest), [manifest]);
  const defaultPreset =
    manifest.presets.find((preset) => preset.id === manifest.defaultPresetId) ??
    manifest.presets[0] ??
    FALLBACK_PROCESSING_MANIFEST.presets[0];
  const activePreset =
    processingUi.presets.find((preset) =>
      controlsExactlyEqual(preset.controls, form.controls),
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
  const isRunning = runStatus.state === "running";
  const canRun = Boolean(inputPath) && !isRunning;

  useEffect(() => {
    let cancelled = false;

    void loadProcessingManifest()
      .then((nextManifest) => {
        if (!cancelled) {
          setManifest(nextManifest);
        }
      })
      .catch(() => {
        // Keep the fallback manifest active so the processing UI stays usable
        // even if the desktop bridge cannot describe the backend presets.
      });

    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    setForm(createInitialForm(defaultPreset.controls));
    setRunStatus({ state: "idle" });
    setProcessedPreviewUrl(null);
    setPipelineOpen(false);
  }, [inputPath]);

  function updateControl<K extends keyof ProcessingControls>(
    key: K,
    value: ProcessingControls[K],
  ) {
    setForm((current) => ({
      ...current,
      controls: {
        ...current.controls,
        [key]: value,
      },
    }));
  }

  function movePipelineStep(index: number, direction: -1 | 1) {
    const target = index + direction;
    if (target < 0 || target >= form.pipeline.length) return;

    const next = [...form.pipeline];
    [next[index], next[target]] = [next[target], next[index]];

    setForm((current) => ({
      ...current,
      pipeline: next,
    }));
  }

  async function handlePickOutputPath() {
    if (!inputPath || isRunning) return;

    const selectedPath = await pickSaveDicomPath(buildOutputName(inputPath));
    if (!selectedPath) return;

    setForm((current) => ({
      ...current,
      outputPath: ensureDicomExtension(selectedPath),
    }));
  }

  async function handleRun() {
    if (!inputPath || !canRun) return;

    setRunStatus({ state: "running" });

    try {
      const result = await runBackendProcess(inputPath, request);
      setProcessedPreviewUrl(result.previewUrl);
      setRunStatus({ state: "success", outputPath: result.dicomPath });
    } catch (error) {
      setRunStatus({ state: "error", message: describeError(error) });
    }
  }

  const args = inputPath ? buildProcessingArgs(inputPath, request) : [];

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
              onClick={handlePickOutputPath}
              disabled={!inputPath || isRunning}
            >
              {form.outputPath ? "Change Save Location" : "Choose Save Location"}
            </button>
            {form.outputPath && (
              <button
                className="button button--ghost"
                type="button"
                onClick={() =>
                  setForm((current) => ({ ...current, outputPath: null }))
                }
                disabled={isRunning}
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
              if (!preset) return;

              setForm((current) => ({
                ...current,
                controls: { ...preset.controls },
              }));
            }}
            disabled={isRunning}
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
              disabled={isRunning}
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
              disabled={isRunning}
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
              disabled={isRunning}
            />
          </div>

          <label className="form-toggle">
            <input
              type="checkbox"
              checked={form.controls.equalize}
              onChange={(event) =>
                updateControl("equalize", event.target.checked)
              }
              disabled={isRunning}
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
            disabled={isRunning}
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
                setForm((current) => ({
                  ...current,
                  compare: event.target.checked,
                }))
              }
              disabled={isRunning}
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
                        disabled={index === 0 || isRunning}
                        aria-label={`Move ${step} up`}
                      >
                        &#9650;
                      </button>
                      <button
                        className="pipeline-btn"
                        type="button"
                        onClick={() => movePipelineStep(index, 1)}
                        disabled={index === form.pipeline.length - 1 || isRunning}
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
                    setForm((current) => ({
                      ...current,
                      pipeline: [...DEFAULT_PIPELINE],
                    }))
                  }
                  disabled={isRunning}
                >
                  Reset to default order
                </button>
              )}
            </div>
          )}
        </section>

        {inputPath && args.length > 0 && (
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
            onClick={handleRun}
            disabled={!canRun}
          >
            {isRunning ? (
              <>
                <span className="spinner" aria-hidden="true" />
                Processing...
              </>
            ) : (
              "Run Processing"
            )}
          </button>

          {runStatus.state === "success" && (
            <div className="run-status run-status--success">
              Processing complete.
            </div>
          )}
          {runStatus.state === "error" && (
            <div className="run-status run-status--error">
              {runStatus.message}
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
