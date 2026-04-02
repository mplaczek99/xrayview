import { useState } from "react";
import { ProcessingTab } from "../components/processing/ProcessingTab";
import { ViewTab } from "../components/viewer/ViewTab";
import { pickDicomFile, runBackendToothMeasurement } from "../lib/backend";
import type { ActiveTab, StudySession } from "../lib/types";

const INITIAL_SESSION: StudySession = {
  inputPath: null,
  inputName: "No study loaded",
  originalPreviewUrl: null,
  processedPreviewUrl: null,
  originalMeasurementScale: null,
  processedMeasurementScale: null,
  toothAnalysis: null,
  processedDicomPath: null,
  savedDestination: null,
  status: "Open a DICOM study to begin.",
  dirty: false,
  runtime: "mock",
};

function describeError(error: unknown, fallback: string): string {
  if (error instanceof Error && error.message.trim()) {
    return error.message;
  }
  if (typeof error === "string" && error.trim()) {
    return error;
  }
  if (error && typeof error === "object" && "message" in error) {
    const message = error.message;
    if (typeof message === "string" && message.trim()) {
      return message;
    }
  }
  return fallback;
}

export function App() {
  const [activeTab, setActiveTab] = useState<ActiveTab>("view");
  const [session, setSession] = useState<StudySession>(INITIAL_SESSION);
  const [busy, setBusy] = useState(false);

  async function handleOpenStudy() {
    const selectedPath = await pickDicomFile();
    if (!selectedPath) return;

    setBusy(true);
    setSession((current) => ({
      ...current,
      status: "Loading preview and running backend tooth analysis...",
    }));

    try {
      const result = await runBackendToothMeasurement(selectedPath);
      const toothFound = Boolean(result.analysis.tooth);
      // Opening a new study clears any derived output so the processing tab
      // cannot accidentally show results from the previous file.
      setSession({
        inputPath: selectedPath,
        inputName: selectedPath.split(/[\\/]/).pop() ?? selectedPath,
        originalPreviewUrl: result.previewUrl,
        processedPreviewUrl: null,
        originalMeasurementScale: result.analysis.calibration.measurementScale,
        processedMeasurementScale: null,
        toothAnalysis: result.analysis,
        processedDicomPath: null,
        savedDestination: null,
        status: toothFound
          ? "Study analyzed. Automatic tooth measurement is ready."
          : "Study loaded, but the backend could not isolate a tooth candidate.",
        dirty: false,
        runtime: result.runtime,
      });
    } catch (error) {
      setSession((current) => ({
        ...current,
        toothAnalysis: null,
        status: describeError(error, "Preview loading failed."),
      }));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="app-shell">
      <nav className="tab-bar" role="tablist" aria-label="Main tabs">
        <button
          className={`tab-bar__tab${activeTab === "view" ? " tab-bar__tab--active" : ""}`}
          type="button"
          role="tab"
          aria-selected={activeTab === "view"}
          onClick={() => setActiveTab("view")}
        >
          View
        </button>
        <button
          className={`tab-bar__tab${activeTab === "processing" ? " tab-bar__tab--active" : ""}`}
          type="button"
          role="tab"
          aria-selected={activeTab === "processing"}
          onClick={() => setActiveTab("processing")}
        >
          Processing
        </button>
      </nav>

      <main className="tab-content" role="tabpanel">
        {activeTab === "view" ? (
          <ViewTab
            previewUrl={session.originalPreviewUrl}
            analysis={session.toothAnalysis}
            busy={busy}
            status={session.status}
            inputName={session.inputName}
            onOpenStudy={handleOpenStudy}
          />
        ) : (
          <ProcessingTab
            inputPath={session.inputPath}
            previewUrl={session.originalPreviewUrl}
          />
        )}
      </main>
    </div>
  );
}
