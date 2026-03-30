import { useMemo, useState } from "react";
import { PanelCard } from "../components/common/PanelCard";
import { ProcessingLab } from "../components/controls/ProcessingLab";
import { TopBar } from "../components/shell/TopBar";
import { ViewerStage } from "../components/viewer/ViewerStage";
import { DEFAULT_CONTROLS, PROCESSING_PRESETS, matchPreset } from "../features/processing/presets";
import {
  buildOutputName,
  copyProcessedOutput,
  ensureDicomExtension,
  paletteLabel,
  pickDicomFile,
  pickSaveDicomPath,
  runBackendPreview,
  runBackendProcess,
} from "../lib/backend";
import type { ProcessingControls, StudySession, ViewerMode } from "../lib/types";

const INITIAL_SESSION: StudySession = {
  inputPath: null,
  inputName: "No study loaded",
  originalPreviewUrl: null,
  processedPreviewUrl: null,
  processedDicomPath: null,
  savedDestination: null,
  status: "Open a DICOM study to load the first preview.",
  dirty: false,
  runtime: "mock",
};

function compactPath(path: string | null): string {
  if (!path) {
    return "Waiting for a file selection";
  }

  return path.length > 72 ? `${path.slice(0, 32)}...${path.slice(-34)}` : path;
}

function describeOutput(session: StudySession): string {
  if (!session.processedDicomPath) {
    return "No processed DICOM generated yet.";
  }
  if (session.savedDestination) {
    return `Saved to ${session.savedDestination}`;
  }
  return session.dirty ? "Rendered output is stale after control changes." : "Temporary processed DICOM ready to save.";
}

export function App() {
  const [controls, setControls] = useState<ProcessingControls>(DEFAULT_CONTROLS);
  const [session, setSession] = useState<StudySession>(INITIAL_SESSION);
  const [activeMode, setActiveMode] = useState<ViewerMode>("original");
  const [busy, setBusy] = useState(false);

  const recipeName = useMemo(() => matchPreset(controls), [controls]);
  const toneLabel = `B ${controls.brightness >= 0 ? `+${controls.brightness}` : controls.brightness} | C ${controls.contrast.toFixed(1)}`;
  const canRender = Boolean(session.inputPath);
  const canSave = Boolean(session.processedDicomPath) && !session.dirty;
  const canCompare = Boolean(session.originalPreviewUrl && session.processedPreviewUrl);

  async function handleOpenStudy() {
    const selectedPath = await pickDicomFile();
    if (!selectedPath) {
      return;
    }

    setBusy(true);
    setSession((current) => ({ ...current, status: "Loading source preview..." }));

    try {
      const result = await runBackendPreview(selectedPath);
      setControls(DEFAULT_CONTROLS);
      setSession({
        inputPath: selectedPath,
        inputName: selectedPath.split(/[\\/]/).pop() ?? selectedPath,
        originalPreviewUrl: result.previewUrl,
        processedPreviewUrl: null,
        processedDicomPath: null,
        savedDestination: null,
        status: "Study loaded. Adjust the controls and render a new output when ready.",
        dirty: false,
        runtime: result.runtime,
      });
      setActiveMode("original");
    } catch (error) {
      setSession((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "Preview loading failed.",
      }));
    } finally {
      setBusy(false);
    }
  }

  async function handleRenderOutput() {
    if (!session.inputPath) {
      return;
    }

    setBusy(true);
    setSession((current) => ({ ...current, status: "Rendering processed preview..." }));

    try {
      const result = await runBackendProcess(session.inputPath, controls);
      setSession((current) => ({
        ...current,
        processedPreviewUrl: result.previewUrl,
        processedDicomPath: result.dicomPath,
        savedDestination: null,
        status: "Processed output ready. Compare it or save the derived DICOM.",
        dirty: false,
        runtime: result.runtime,
      }));
      setActiveMode("processed");
    } catch (error) {
      setSession((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "Processing failed.",
      }));
    } finally {
      setBusy(false);
    }
  }

  async function handleSaveOutput() {
    if (!session.processedDicomPath) {
      return;
    }

    const destination = await pickSaveDicomPath(buildOutputName(session.inputName));
    if (!destination) {
      return;
    }

    setBusy(true);
    setSession((current) => ({ ...current, status: "Saving processed DICOM..." }));

    try {
      const savedPath = await copyProcessedOutput(
        session.processedDicomPath,
        ensureDicomExtension(destination),
      );

      setSession((current) => ({
        ...current,
        savedDestination: savedPath,
        status: "Derived DICOM saved.",
      }));
    } catch (error) {
      setSession((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "Saving failed.",
      }));
    } finally {
      setBusy(false);
    }
  }

  function updateControls(next: ProcessingControls) {
    setControls(next);
    setSession((current) => {
      if (!current.processedDicomPath) {
        return current;
      }

      return {
        ...current,
        dirty: true,
        savedDestination: null,
        status: "Controls changed. Render again before saving the next output.",
      };
    });
  }

  function applyPreset(preset: (typeof PROCESSING_PRESETS)[number]) {
    setControls(preset.controls);
    setSession((current) => ({
      ...current,
      dirty: Boolean(current.processedDicomPath),
      savedDestination: null,
      status: current.processedDicomPath
        ? `${preset.label} applied. Render again to refresh the processed DICOM.`
        : `${preset.label} applied. Render when ready.`,
    }));
  }

  return (
    <div className="app-shell">
      <TopBar
        busy={busy}
        runtimeLabel={session.runtime === "tauri" ? "Tauri bridge live" : "Mock preview mode"}
        statusLabel={session.dirty ? "Output stale" : "Session ready"}
        onOpenStudy={handleOpenStudy}
        onRenderOutput={handleRenderOutput}
        onSaveOutput={handleSaveOutput}
        canRender={canRender}
        canSave={canSave}
      />

      <div className="workspace-grid">
        <aside className="sidebar-column">
          <PanelCard
            eyebrow="Study Deck"
            title={session.inputName}
            description="Keep the source study, runtime, and export path visible while iterating on the visual design."
          >
            <dl className="data-list">
              <div>
                <dt>Source path</dt>
                <dd className="u-mono">{compactPath(session.inputPath)}</dd>
              </div>
              <div>
                <dt>Backend mode</dt>
                <dd>{session.runtime === "tauri" ? "Live CLI bridge" : "Mock desktop fallback"}</dd>
              </div>
              <div>
                <dt>Processed output</dt>
                <dd>{describeOutput(session)}</dd>
              </div>
            </dl>
          </PanelCard>

          <PanelCard
            eyebrow="Migration"
            title="Why this shell exists"
            description="The goal is to prove the next frontend stack without changing the Go processing code or DICOM pipeline."
          >
            <div className="bullet-stack">
              <p>Large center viewer for the diagnostic image area.</p>
              <p>Right-side control density for preset, tone, and output actions.</p>
              <p>Static report-friendly structure ready for richer cards and annotations.</p>
            </div>
          </PanelCard>
        </aside>

        <main className="content-column">
          <div className="metric-grid">
            <PanelCard eyebrow="Recipe" title={recipeName} description="Preset detection stays derived from live control values." />
            <PanelCard eyebrow="Tone" title={toneLabel} description={`Invert ${controls.invert ? "on" : "off"}, equalize ${controls.equalize ? "on" : "off"}.`} />
            <PanelCard eyebrow="Palette" title={paletteLabel(controls.palette)} description={session.dirty ? "Preview needs a fresh render." : "Viewer and controls are aligned."} />
          </div>

          <ViewerStage
            activeMode={activeMode}
            canCompare={canCompare}
            originalPreviewUrl={session.originalPreviewUrl}
            processedPreviewUrl={session.processedPreviewUrl}
            recipeName={recipeName}
            toneLabel={toneLabel}
            paletteLabel={paletteLabel(controls.palette)}
            dirty={session.dirty}
            onModeChange={setActiveMode}
          />

          <div className="preview-rail">
            <button className={`preview-card ${activeMode === "original" ? "is-active" : ""}`} type="button" onClick={() => setActiveMode("original")} disabled={!session.originalPreviewUrl}>
              <span className="preview-card__label">Original DICOM</span>
              <span className="preview-card__caption">Direct source preview</span>
            </button>
            <button className={`preview-card ${activeMode === "processed" ? "is-active" : ""}`} type="button" onClick={() => setActiveMode("processed")} disabled={!session.processedPreviewUrl}>
              <span className="preview-card__label">Processed DICOM</span>
              <span className="preview-card__caption">Derived output for compare and export</span>
            </button>
            <button className={`preview-card ${activeMode === "compare" ? "is-active" : ""}`} type="button" onClick={() => setActiveMode("compare")} disabled={!canCompare}>
              <span className="preview-card__label">Compare View</span>
              <span className="preview-card__caption">Side-by-side baseline for future review tooling</span>
            </button>
          </div>
        </main>

        <aside className="sidebar-column sidebar-column--wide">
          <PanelCard
            eyebrow="Processing Lab"
            title="Controls"
            description="This baseline already preserves stale-output protection and keeps render/save flow distinct."
          >
            <ProcessingLab
              controls={controls}
              presets={PROCESSING_PRESETS}
              dirty={session.dirty}
              onPresetSelect={applyPreset}
              onChange={updateControls}
            />
          </PanelCard>

          <PanelCard
            eyebrow="Render Status"
            title={session.status}
            description="The desktop bridge and backend integration surface here first while the rest of the UI evolves."
          >
            <dl className="data-list">
              <div>
                <dt>Save path</dt>
                <dd className="u-mono">{compactPath(session.savedDestination)}</dd>
              </div>
              <div>
                <dt>Temporary output</dt>
                <dd className="u-mono">{compactPath(session.processedDicomPath)}</dd>
              </div>
            </dl>
          </PanelCard>
        </aside>
      </div>
    </div>
  );
}
