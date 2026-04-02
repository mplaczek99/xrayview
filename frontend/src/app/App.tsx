import { useState } from "react";
import { ProcessingTab } from "../components/processing/ProcessingTab";
import { ViewTab } from "../components/viewer/ViewTab";
import {
  pickDicomFile,
  runBackendPreview,
  runBackendToothMeasurement,
} from "../lib/backend";
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
  const [busyAction, setBusyAction] = useState<"opening" | "measuring" | null>(
    null,
  );

  async function handleOpenStudy() {
    const selectedPath = await pickDicomFile();
    if (!selectedPath) return;

    setBusyAction("opening");
    setSession((current) => ({
      ...current,
      status: "Loading source preview...",
    }));

    try {
      const result = await runBackendPreview(selectedPath);
      // Opening a new study clears any derived output so the processing tab
      // cannot accidentally show results from the previous file.
      setSession({
        inputPath: selectedPath,
        inputName: selectedPath.split(/[\\/]/).pop() ?? selectedPath,
        originalPreviewUrl: result.previewUrl,
        processedPreviewUrl: null,
        originalMeasurementScale: result.measurementScale,
        processedMeasurementScale: null,
        toothAnalysis: null,
        processedDicomPath: null,
        savedDestination: null,
        status: "Study loaded. Click Measure tooth to run backend analysis.",
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
      setBusyAction(null);
    }
  }

  async function handleMeasureTooth() {
    if (!session.inputPath) return;

    setBusyAction("measuring");
    setSession((current) => ({
      ...current,
      status: "Running backend tooth measurement...",
    }));

    try {
      const result = await runBackendToothMeasurement(session.inputPath);
      const toothFound = Boolean(result.analysis.tooth);

      setSession((current) => ({
        ...current,
        originalPreviewUrl: result.previewUrl,
        originalMeasurementScale:
          result.analysis.calibration.measurementScale ??
          current.originalMeasurementScale,
        toothAnalysis: result.analysis,
        runtime: result.runtime,
        status: toothFound
          ? "Tooth measurement complete."
          : "Measurement completed, but the backend could not isolate a tooth candidate.",
      }));
    } catch (error) {
      setSession((current) => ({
        ...current,
        status: describeError(error, "Tooth measurement failed."),
      }));
    } finally {
      setBusyAction(null);
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
            inputPath={session.inputPath}
            previewUrl={session.originalPreviewUrl}
            analysis={session.toothAnalysis}
            measurementScale={session.originalMeasurementScale}
            busyAction={busyAction}
            status={session.status}
            inputName={session.inputName}
            onOpenStudy={handleOpenStudy}
            onMeasureTooth={handleMeasureTooth}
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
