import { useState } from "react";
import { copyProcessedOutput, runBackendProcess } from "../../lib/backend";
import type { Palette, ProcessingControls } from "../../lib/types";
import { DicomViewer } from "../viewer/DicomViewer";

type PresetId = "default" | "xray" | "high-contrast";
type PipelineStep = "grayscale" | "invert" | "brightness" | "contrast" | "equalize";

interface ProcessingTabProps {
  inputPath: string | null;
  previewUrl: string | null;
}

interface ProcessingForm {
  outputPath: string;
  preset: PresetId;
  invert: boolean;
  brightness: number;
  contrast: number;
  equalize: boolean;
  palette: Palette;
  compare: boolean;
  pipelineOrder: PipelineStep[];
}

type RunStatus =
  | { state: "idle" }
  | { state: "running" }
  | { state: "success"; outputPath: string }
  | { state: "error"; message: string };

const DEFAULT_PIPELINE: PipelineStep[] = [
  "grayscale",
  "invert",
  "brightness",
  "contrast",
  "equalize",
];

const PRESET_HINTS: Record<
  PresetId,
  { brightness: number; contrast: number; equalize: boolean; palette: Palette }
> = {
  default: { brightness: 0, contrast: 1.0, equalize: false, palette: "none" },
  xray: { brightness: 10, contrast: 1.4, equalize: true, palette: "bone" },
  "high-contrast": {
    brightness: 0,
    contrast: 1.8,
    equalize: true,
    palette: "none",
  },
};

const INITIAL_FORM: ProcessingForm = {
  outputPath: "",
  preset: "default",
  invert: false,
  brightness: 0,
  contrast: 1.0,
  equalize: false,
  palette: "none",
  compare: false,
  pipelineOrder: [...DEFAULT_PIPELINE],
};

function isValidOutputPath(path: string): boolean {
  return /\.(dcm|dicom)$/i.test(path);
}

function pipelinesEqual(a: PipelineStep[], b: PipelineStep[]): boolean {
  return a.length === b.length && a.every((step, i) => step === b[i]);
}

function buildArgs(inputPath: string, form: ProcessingForm): string[] {
  const args: string[] = ["--input", inputPath];

  if (form.outputPath && isValidOutputPath(form.outputPath)) {
    args.push("--output", form.outputPath);
  }

  args.push("--preset", form.preset);

  if (form.invert) args.push("--invert");
  if (form.brightness !== 0)
    args.push("--brightness", String(form.brightness));
  if (form.contrast !== 1.0) args.push("--contrast", String(form.contrast));
  if (form.equalize) args.push("--equalize");
  if (form.palette !== "none") args.push("--palette", form.palette);
  if (form.compare) args.push("--compare");

  if (!pipelinesEqual(form.pipelineOrder, DEFAULT_PIPELINE)) {
    args.push("--pipeline", form.pipelineOrder.join(","));
  }

  return args;
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

export function ProcessingTab({ inputPath, previewUrl }: ProcessingTabProps) {
  const [form, setForm] = useState<ProcessingForm>(INITIAL_FORM);
  const [runStatus, setRunStatus] = useState<RunStatus>({ state: "idle" });
  const [processedPreviewUrl, setProcessedPreviewUrl] = useState<string | null>(
    null,
  );
  const [pipelineOpen, setPipelineOpen] = useState(false);

  const outputPathError =
    form.outputPath.length > 0 && !isValidOutputPath(form.outputPath)
      ? "Output path must end with .dcm or .dicom"
      : null;

  const hint = PRESET_HINTS[form.preset];
  const isRunning = runStatus.state === "running";
  const canRun = Boolean(inputPath) && !isRunning && !outputPathError;

  function updateForm<K extends keyof ProcessingForm>(
    key: K,
    value: ProcessingForm[K],
  ) {
    setForm((prev) => ({ ...prev, [key]: value }));
  }

  function movePipelineStep(index: number, direction: -1 | 1) {
    const target = index + direction;
    if (target < 0 || target >= form.pipelineOrder.length) return;
    const next = [...form.pipelineOrder];
    [next[index], next[target]] = [next[target], next[index]];
    updateForm("pipelineOrder", next);
  }

  async function handleRun() {
    if (!inputPath || !canRun) return;

    setRunStatus({ state: "running" });

    try {
      const controls: ProcessingControls = {
        brightness: form.brightness,
        contrast: form.contrast,
        invert: form.invert,
        equalize: form.equalize,
        palette: form.palette,
      };

      const result = await runBackendProcess(inputPath, controls);
      let finalOutputPath = result.dicomPath;

      if (form.outputPath && isValidOutputPath(form.outputPath)) {
        finalOutputPath = await copyProcessedOutput(
          result.dicomPath,
          form.outputPath,
        );
      }

      setProcessedPreviewUrl(result.previewUrl);
      setRunStatus({ state: "success", outputPath: finalOutputPath });
    } catch (error) {
      setRunStatus({ state: "error", message: describeError(error) });
    }
  }

  const args = inputPath ? buildArgs(inputPath, form) : [];

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
        {/* Output Path */}
        <section className="form-section">
          <label className="form-label" htmlFor="proc-output">
            Output Path
          </label>
          <input
            id="proc-output"
            className={`form-input${outputPathError ? " form-input--error" : ""}`}
            type="text"
            value={form.outputPath}
            placeholder="Leave blank to auto-generate (input_processed.dcm)"
            onChange={(e) => updateForm("outputPath", e.target.value)}
            disabled={isRunning}
          />
          {outputPathError && <p className="form-error">{outputPathError}</p>}
        </section>

        {/* Preset Selector */}
        <section className="form-section">
          <label className="form-label" htmlFor="proc-preset">
            Preset
          </label>
          <select
            id="proc-preset"
            className="form-select"
            value={form.preset}
            onChange={(e) => updateForm("preset", e.target.value as PresetId)}
            disabled={isRunning}
          >
            <option value="default">default</option>
            <option value="xray">xray</option>
            <option value="high-contrast">high-contrast</option>
          </select>
          <p className="form-hint">
            B {hint.brightness}, C {hint.contrast}, equalize{" "}
            {hint.equalize ? "on" : "off"}, palette {hint.palette}
          </p>
        </section>

        {/* Grayscale Controls */}
        <section className="form-section">
          <div className="form-label">Grayscale Controls</div>

          <label className="form-toggle">
            <input
              type="checkbox"
              checked={form.invert}
              onChange={(e) => updateForm("invert", e.target.checked)}
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
              value={form.brightness}
              step={1}
              onChange={(e) =>
                updateForm("brightness", parseInt(e.target.value, 10) || 0)
              }
              disabled={isRunning}
            />
            <span className="form-preset-hint">preset: {hint.brightness}</span>
          </div>

          <div className="form-field">
            <label className="form-field__label" htmlFor="proc-contrast">
              Contrast
            </label>
            <input
              id="proc-contrast"
              className="form-input form-input--number"
              type="number"
              value={form.contrast}
              step={0.1}
              min={0}
              onChange={(e) =>
                updateForm("contrast", parseFloat(e.target.value) || 0)
              }
              disabled={isRunning}
            />
            <span className="form-preset-hint">preset: {hint.contrast}</span>
          </div>

          <label className="form-toggle">
            <input
              type="checkbox"
              checked={form.equalize}
              onChange={(e) => updateForm("equalize", e.target.checked)}
              disabled={isRunning}
            />
            <span>Equalize</span>
            <span className="form-preset-hint">
              preset: {hint.equalize ? "on" : "off"}
            </span>
          </label>
        </section>

        {/* Palette */}
        <section className="form-section">
          <label className="form-label" htmlFor="proc-palette">
            Palette
            <span className="form-preset-hint"> preset: {hint.palette}</span>
          </label>
          <select
            id="proc-palette"
            className="form-select"
            value={form.palette}
            onChange={(e) => updateForm("palette", e.target.value as Palette)}
            disabled={isRunning}
          >
            <option value="none">none</option>
            <option value="hot">hot</option>
            <option value="bone">bone</option>
          </select>
        </section>

        {/* Comparison Output */}
        <section className="form-section">
          <label className="form-toggle">
            <input
              type="checkbox"
              checked={form.compare}
              onChange={(e) => updateForm("compare", e.target.checked)}
              disabled={isRunning}
            />
            <span>Compare</span>
          </label>
          <p className="form-hint">
            Write side-by-side comparison into output DICOM
          </p>
        </section>

        {/* Pipeline Order (Advanced) */}
        <section className="form-section">
          <button
            className="form-collapse-toggle"
            type="button"
            onClick={() => setPipelineOpen((prev) => !prev)}
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
                {form.pipelineOrder.map((step, index) => (
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
                        disabled={
                          index === form.pipelineOrder.length - 1 || isRunning
                        }
                        aria-label={`Move ${step} down`}
                      >
                        &#9660;
                      </button>
                    </div>
                  </li>
                ))}
              </ul>
              <p className="form-hint">
                grayscale is always the starting point. Pseudocolor is applied
                after the pipeline.
              </p>
              {!pipelinesEqual(form.pipelineOrder, DEFAULT_PIPELINE) && (
                <button
                  className="button button--ghost pipeline-reset"
                  type="button"
                  onClick={() =>
                    updateForm("pipelineOrder", [...DEFAULT_PIPELINE])
                  }
                  disabled={isRunning}
                >
                  Reset to default order
                </button>
              )}
            </div>
          )}
        </section>

        {/* Command Preview */}
        {inputPath && args.length > 0 && (
          <section className="form-section">
            <div className="form-label">Command Preview</div>
            <pre className="args-preview u-mono">
              xrayview {args.join(" ")}
            </pre>
          </section>
        )}

        {/* Run Button & Status */}
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

        {/* Processed Output */}
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
