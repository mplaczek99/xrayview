import { useEffect, useMemo, useState } from "react";
import {
  loadPrototypeInfo,
  openStudy,
  pickDicomFile,
  pickPreviewArtifact,
  pickSaveDicomPath,
  type OpenStudyResult,
  type PrototypeInfo,
} from "./bindings";

function fileNameFromPath(path: string): string {
  return path.split(/[\\/]/).pop() || path;
}

function defaultOutputName(inputPath: string): string {
  const fileName = fileNameFromPath(inputPath) || "study.dcm";
  const baseName = fileName.replace(/\.(dcm|dicom)$/i, "");
  return `${baseName}_processed.dcm`;
}

function buildPreviewUrl(endpointPath: string, previewPath: string): string {
  return `${endpointPath}?path=${encodeURIComponent(previewPath)}`;
}

function formatError(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }

  return String(error);
}

function formatTimestamp(value: string | undefined): string {
  if (!value) {
    return "Unavailable";
  }

  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleString();
}

export function WailsPrototypeApp() {
  const [info, setInfo] = useState<PrototypeInfo | null>(null);
  const [loadingInfo, setLoadingInfo] = useState(true);
  const [selectedDicomPath, setSelectedDicomPath] = useState("");
  const [selectedPreviewPath, setSelectedPreviewPath] = useState("");
  const [selectedSavePath, setSelectedSavePath] = useState<string | null>(null);
  const [openStudyResult, setOpenStudyResult] = useState<OpenStudyResult | null>(null);
  const [busyAction, setBusyAction] = useState<string | null>(null);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);

  useEffect(() => {
    let active = true;

    void (async () => {
      try {
        const nextInfo = await loadPrototypeInfo();
        if (!active) {
          return;
        }

        setInfo(nextInfo);
        setSelectedDicomPath(nextInfo.sampleDicomPath);
        setSelectedPreviewPath(nextInfo.samplePreviewPath);
        setErrorMessage(nextInfo.startupError);
      } catch (error) {
        if (!active) {
          return;
        }

        setErrorMessage(formatError(error));
      } finally {
        if (active) {
          setLoadingInfo(false);
        }
      }
    })();

    return () => {
      active = false;
    };
  }, []);

  const previewUrl = useMemo(() => {
    if (!info || !selectedPreviewPath) {
      return null;
    }

    return buildPreviewUrl(info.previewEndpointPath, selectedPreviewPath);
  }, [info, selectedPreviewPath]);

  async function runAction(action: string, work: () => Promise<void>) {
    setBusyAction(action);
    setErrorMessage(null);
    try {
      await work();
    } catch (error) {
      setErrorMessage(formatError(error));
    } finally {
      setBusyAction(null);
    }
  }

  async function handlePickDicomFile() {
    await runAction("pick-dicom", async () => {
      const selected = await pickDicomFile();
      if (selected) {
        setSelectedDicomPath(selected);
      }
    });
  }

  async function handlePickPreviewArtifact() {
    await runAction("pick-preview", async () => {
      const selected = await pickPreviewArtifact();
      if (selected) {
        setSelectedPreviewPath(selected);
      }
    });
  }

  async function handlePickSavePath() {
    await runAction("pick-save", async () => {
      const selected = await pickSaveDicomPath(
        selectedDicomPath ? defaultOutputName(selectedDicomPath) : "study_processed.dcm",
      );
      if (selected) {
        setSelectedSavePath(selected);
      }
    });
  }

  async function handleOpenStudy() {
    if (!selectedDicomPath) {
      setErrorMessage("Choose a DICOM file before calling openStudy.");
      return;
    }

    await runAction("open-study", async () => {
      const result = await openStudy(selectedDicomPath);
      setOpenStudyResult(result);
      const refreshedInfo = await loadPrototypeInfo();
      setInfo(refreshedInfo);
    });
  }

  const isBusy = busyAction !== null;

  return (
    <div className="wails-prototype">
      <header className="wails-prototype__hero">
        <div>
          <p className="wails-prototype__eyebrow">Phase 39</p>
          <h1 className="wails-prototype__title">Wails Shell Prototype</h1>
          <p className="wails-prototype__copy">
            Focused shell evaluation for launch, dialogs, preview artifact access, and a
            single live Go-backend call path.
          </p>
        </div>
        <div className="wails-prototype__hero-card">
          <span className="wails-prototype__pill">
            {loadingInfo
              ? "Inspecting shell..."
              : info?.startupError
                ? "Backend retry required"
                : info?.sidecarManaged
                  ? "Prototype-managed sidecar"
                  : "Attached to existing sidecar"}
          </span>
          <div className="wails-prototype__hero-value">
            {info?.backendBaseUrl ?? "http://127.0.0.1:38181"}
          </div>
          <div className="wails-prototype__hero-label">Loopback backend target</div>
        </div>
      </header>

      {errorMessage && (
        <section className="wails-prototype__alert" role="alert">
          {errorMessage}
        </section>
      )}

      <main className="wails-prototype__grid">
        <section className="wails-prototype__panel">
          <div className="wails-prototype__panel-header">
            <div>
              <p className="wails-prototype__label">Native Dialogs</p>
              <h2 className="wails-prototype__panel-title">Open and save without Tauri</h2>
            </div>
            <span className="wails-prototype__muted">
              {busyAction === "pick-dicom" || busyAction === "pick-save" ? "Working..." : "Idle"}
            </span>
          </div>

          <div className="wails-prototype__actions">
            <button
              className="button button--primary"
              type="button"
              onClick={() => void handlePickDicomFile()}
              disabled={isBusy}
            >
              Pick DICOM
            </button>
            <button
              className="button"
              type="button"
              onClick={() => info && setSelectedDicomPath(info.sampleDicomPath)}
              disabled={isBusy || !info}
            >
              Use Sample DICOM
            </button>
            <button
              className="button"
              type="button"
              onClick={() => void handlePickSavePath()}
              disabled={isBusy}
            >
              Pick Save Path
            </button>
          </div>

          <label className="wails-prototype__field">
            <span>DICOM input path</span>
            <input
              value={selectedDicomPath}
              onChange={(event) => setSelectedDicomPath(event.target.value)}
              placeholder="/absolute/path/to/study.dcm"
            />
          </label>

          <label className="wails-prototype__field">
            <span>Save dialog result</span>
            <input value={selectedSavePath ?? ""} readOnly placeholder="No save path chosen yet" />
          </label>

          <div className="wails-prototype__hint">
            The prototype uses Wails runtime dialogs instead of the Tauri command bridge.
          </div>
        </section>

        <section className="wails-prototype__panel">
          <div className="wails-prototype__panel-header">
            <div>
              <p className="wails-prototype__label">Preview Artifacts</p>
              <h2 className="wails-prototype__panel-title">Disk image served through Wails</h2>
            </div>
            <span className="wails-prototype__muted">GET {info?.previewEndpointPath ?? "/preview"}</span>
          </div>

          <div className="wails-prototype__actions">
            <button
              className="button button--primary"
              type="button"
              onClick={() => void handlePickPreviewArtifact()}
              disabled={isBusy}
            >
              Pick Preview Image
            </button>
            <button
              className="button"
              type="button"
              onClick={() => info && setSelectedPreviewPath(info.samplePreviewPath)}
              disabled={isBusy || !info}
            >
              Use Sample Preview
            </button>
          </div>

          <label className="wails-prototype__field">
            <span>Artifact path</span>
            <input
              value={selectedPreviewPath}
              onChange={(event) => setSelectedPreviewPath(event.target.value)}
              placeholder="/absolute/path/to/render-preview.png"
            />
          </label>

          <div className="wails-prototype__preview-shell">
            {previewUrl ? (
              <img
                className="wails-prototype__preview-image"
                src={previewUrl}
                alt="Prototype preview artifact"
              />
            ) : (
              <div className="wails-prototype__empty">Choose an artifact to verify local preview access.</div>
            )}
          </div>

          <code className="wails-prototype__code">{previewUrl ?? "No preview URL resolved yet"}</code>
        </section>

        <section className="wails-prototype__panel">
          <div className="wails-prototype__panel-header">
            <div>
              <p className="wails-prototype__label">Backend Call Path</p>
              <h2 className="wails-prototype__panel-title">Live `openStudy` over the Go sidecar</h2>
            </div>
            <span className="wails-prototype__muted">
              {busyAction === "open-study" ? "Calling backend..." : "Ready"}
            </span>
          </div>

          <div className="wails-prototype__actions">
            <button
              className="button button--primary"
              type="button"
              onClick={() => void handleOpenStudy()}
              disabled={isBusy || !selectedDicomPath}
            >
              Call openStudy
            </button>
          </div>

          <div className="wails-prototype__kv">
            <span>Round trip</span>
            <strong>{openStudyResult ? `${openStudyResult.roundTripMs.toFixed(1)} ms` : "Not called yet"}</strong>
          </div>
          <div className="wails-prototype__kv">
            <span>Study ID</span>
            <strong>{openStudyResult?.studyId ?? "Unavailable"}</strong>
          </div>
          <div className="wails-prototype__kv">
            <span>Input name</span>
            <strong>{openStudyResult?.inputName ?? "Unavailable"}</strong>
          </div>
          <div className="wails-prototype__kv">
            <span>Measurement scale</span>
            <strong>
              {openStudyResult?.measurementScale
                ? `${openStudyResult.measurementScale.rowSpacingMm} x ${openStudyResult.measurementScale.columnSpacingMm} mm`
                : "Unavailable"}
            </strong>
          </div>

          <pre className="wails-prototype__json">
            {openStudyResult
              ? JSON.stringify(openStudyResult, null, 2)
              : "Run openStudy to inspect the live contract payload."}
          </pre>
        </section>

        <section className="wails-prototype__panel">
          <div className="wails-prototype__panel-header">
            <div>
              <p className="wails-prototype__label">Environment Snapshot</p>
              <h2 className="wails-prototype__panel-title">Shell and sidecar state</h2>
            </div>
            <span className="wails-prototype__muted">
              Started {formatTimestamp(info?.backendHealth?.startedAt)}
            </span>
          </div>

          <div className="wails-prototype__kv">
            <span>Managed sidecar</span>
            <strong>{info ? String(info.sidecarManaged) : "Unknown"}</strong>
          </div>
          <div className="wails-prototype__kv">
            <span>Sidecar binary</span>
            <strong>{info?.sidecarBinaryPath ?? "Unavailable"}</strong>
          </div>
          <div className="wails-prototype__kv">
            <span>Frontend assets</span>
            <strong>{info?.assetDir ?? "Unavailable"}</strong>
          </div>
          <div className="wails-prototype__kv">
            <span>Transport</span>
            <strong>{info?.backendHealth?.transport ?? "Unavailable"}</strong>
          </div>
          <div className="wails-prototype__kv">
            <span>Studies registered</span>
            <strong>{info?.backendHealth?.studyCount ?? 0}</strong>
          </div>

          <pre className="wails-prototype__json">
            {info?.backendHealth
              ? JSON.stringify(info.backendHealth, null, 2)
              : "Backend health is unavailable until the sidecar responds."}
          </pre>
        </section>
      </main>
    </div>
  );
}
