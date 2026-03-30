import { useEffect, useMemo, useState } from "react";
import { ProcessingLab } from "../components/controls/ProcessingLab";
import { TopBar } from "../components/shell/TopBar";
import { ViewerStage } from "../components/viewer/ViewerStage";
import {
  buildProcessingUiState,
  matchPreset,
  processingControlsEqual,
} from "../features/processing/presets";
import {
  FALLBACK_PROCESSING_MANIFEST,
  buildOutputName,
  copyProcessedOutput,
  ensureDicomExtension,
  loadProcessingManifest,
  paletteLabel,
  pickDicomFile,
  pickSaveDicomPath,
  runBackendPreview,
  runBackendProcess,
} from "../lib/backend";
import type {
  ProcessingControls,
  ProcessingPreset,
  StudySession,
  ViewerMode,
} from "../lib/types";

const INITIAL_SESSION: StudySession = {
  inputPath: null,
  inputName: "No study loaded",
  originalPreviewUrl: null,
  processedPreviewUrl: null,
  originalMeasurementScale: null,
  processedMeasurementScale: null,
  processedDicomPath: null,
  savedDestination: null,
  status: "Open a DICOM study to load the first preview.",
  dirty: false,
  runtime: "mock",
};

const ACTIVITY_ITEMS = [
  { label: "EX", title: "Explorer", active: true },
  { label: "VI", title: "Viewer" },
  { label: "FX", title: "Filters" },
  { label: "IO", title: "Export" },
];

interface SessionTask {
  pendingStatus: string;
  failureStatus: string;
  run: () => Promise<void>;
}

const INITIAL_PROCESSING_UI_STATE = buildProcessingUiState(FALLBACK_PROCESSING_MANIFEST);

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
  const [processingState, setProcessingState] = useState(INITIAL_PROCESSING_UI_STATE);
  const [controls, setControls] = useState<ProcessingControls>(
    INITIAL_PROCESSING_UI_STATE.defaultControls,
  );
  const [session, setSession] = useState<StudySession>(INITIAL_SESSION);
  const [activeMode, setActiveMode] = useState<ViewerMode>("original");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let cancelled = false;

    void loadProcessingManifest()
      .then((manifest) => {
        if (cancelled) {
          return;
        }

        const nextProcessingState = buildProcessingUiState(manifest);
        setProcessingState(nextProcessingState);
        setControls((current) =>
          processingControlsEqual(current, INITIAL_PROCESSING_UI_STATE.defaultControls)
            ? { ...nextProcessingState.defaultControls }
            : current,
        );
      })
      .catch((error) => {
        if (cancelled) {
          return;
        }

        setSession((current) => ({
          ...current,
          status: describeError(error, "Processing preset loading failed."),
        }));
      });

    return () => {
      cancelled = true;
    };
  }, []);

  const recipeName = useMemo(
    () => matchPreset(controls, processingState.presets),
    [controls, processingState.presets],
  );
  const toneLabel = `B ${controls.brightness >= 0 ? `+${controls.brightness}` : controls.brightness} | C ${controls.contrast.toFixed(1)}`;
  const canRender = Boolean(session.inputPath);
  const canSave = Boolean(session.processedDicomPath) && !session.dirty;
  const canCompare = Boolean(session.originalPreviewUrl && session.processedPreviewUrl);
  const canViewProcessed = Boolean(session.processedPreviewUrl);

  async function runSessionTask({ pendingStatus, failureStatus, run }: SessionTask) {
    setBusy(true);
    setSession((current) => ({ ...current, status: pendingStatus }));

    try {
      await run();
    } catch (error) {
      setSession((current) => ({
        ...current,
        status: describeError(error, failureStatus),
      }));
    } finally {
      setBusy(false);
    }
  }

  const viewerTabs: Array<{
    mode: ViewerMode;
    label: string;
    caption: string;
    disabled: boolean;
  }> = [
    {
      mode: "original",
      label: "source.dcm",
      caption: session.originalPreviewUrl ? "Loaded" : "Waiting",
      disabled: !session.originalPreviewUrl,
    },
    {
      mode: "processed",
      label: "processed.dcm",
      caption: canViewProcessed ? "Rendered" : "Locked",
      disabled: !canViewProcessed,
    },
    {
      mode: "compare",
      label: "compare.diff",
      caption: canCompare ? "Ready" : "Locked",
      disabled: !canCompare,
    },
  ];

  async function handleOpenStudy() {
    const selectedPath = await pickDicomFile();
    if (!selectedPath) {
      return;
    }

    await runSessionTask({
      pendingStatus: "Loading source preview...",
      failureStatus: "Preview loading failed.",
      run: async () => {
        const result = await runBackendPreview(selectedPath);
        setControls({ ...processingState.defaultControls });
        setSession({
          inputPath: selectedPath,
          inputName: selectedPath.split(/[\\/]/).pop() ?? selectedPath,
          originalPreviewUrl: result.previewUrl,
          processedPreviewUrl: null,
          originalMeasurementScale: result.measurementScale,
          processedMeasurementScale: null,
          processedDicomPath: null,
          savedDestination: null,
          status: "Study loaded. Adjust the controls and render a new output when ready.",
          dirty: false,
          runtime: result.runtime,
        });
        setActiveMode("original");
      },
    });
  }

  async function handleRenderOutput() {
    const inputPath = session.inputPath;
    if (!inputPath) {
      return;
    }

    await runSessionTask({
      pendingStatus: "Rendering processed preview...",
      failureStatus: "Processing failed.",
      run: async () => {
        const result = await runBackendProcess(inputPath, controls);
        setSession((current) => ({
          ...current,
          processedPreviewUrl: result.previewUrl,
          processedMeasurementScale: result.measurementScale,
          processedDicomPath: result.dicomPath,
          savedDestination: null,
          status: "Processed output ready. Compare it or save the derived DICOM.",
          dirty: false,
          runtime: result.runtime,
        }));
        setActiveMode("processed");
      },
    });
  }

  async function handleSaveOutput() {
    const processedDicomPath = session.processedDicomPath;
    if (!processedDicomPath) {
      return;
    }

    const destination = await pickSaveDicomPath(buildOutputName(session.inputName));
    if (!destination) {
      return;
    }

    await runSessionTask({
      pendingStatus: "Saving processed DICOM...",
      failureStatus: "Saving failed.",
      run: async () => {
        const savedPath = await copyProcessedOutput(
          processedDicomPath,
          ensureDicomExtension(destination),
        );

        setSession((current) => ({
          ...current,
          savedDestination: savedPath,
          status: "Derived DICOM saved.",
        }));
      },
    });
  }

  function updateControls(next: ProcessingControls) {
    if (busy) {
      return;
    }

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

  function applyPreset(preset: ProcessingPreset) {
    if (busy) {
      return;
    }

    setControls({ ...preset.controls });
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
        workspaceName={session.inputName}
        recipeName={recipeName}
        runtimeLabel={session.runtime === "tauri" ? "Tauri bridge live" : "Mock preview mode"}
        statusLabel={session.dirty ? "Output stale" : "Session ready"}
        onOpenStudy={handleOpenStudy}
        onRenderOutput={handleRenderOutput}
        onSaveOutput={handleSaveOutput}
        canRender={canRender}
        canSave={canSave}
      />

      <div className="ide-layout">
        <nav className="activity-rail" aria-label="Workbench sections">
          {ACTIVITY_ITEMS.map((item) => (
            <div key={item.label} className={`activity-rail__item ${item.active ? "is-active" : ""}`}>
              <span className="activity-rail__shortcut">{item.label}</span>
              <span className="activity-rail__label">{item.title}</span>
            </div>
          ))}
        </nav>

        <aside className="explorer-sidebar">
          <section className="sidebar-panel">
            <div className="sidebar-panel__heading">EXPLORER</div>

            <div className="sidebar-section">
              <div className="sidebar-section__label">OPEN EDITORS</div>
              <div className="tree-list" role="tablist" aria-label="Open editors">
                {viewerTabs.map((tab) => (
                  <button
                    key={tab.mode}
                    className={`tree-item ${activeMode === tab.mode ? "is-active" : ""}`}
                    type="button"
                    onClick={() => setActiveMode(tab.mode)}
                    disabled={tab.disabled}
                  >
                    <span className="tree-item__name">{tab.label}</span>
                    <span className="tree-item__meta">{tab.caption}</span>
                  </button>
                ))}
              </div>
            </div>

            <div className="sidebar-section">
              <div className="sidebar-section__label">WORKSPACE</div>
              <dl className="data-list data-list--compact">
                <div>
                  <dt>Study</dt>
                  <dd>{session.inputName}</dd>
                </div>
                <div>
                  <dt>Source path</dt>
                  <dd className="u-mono">{compactPath(session.inputPath)}</dd>
                </div>
                <div>
                  <dt>Backend</dt>
                  <dd>{session.runtime === "tauri" ? "Live CLI bridge" : "Mock browser bridge"}</dd>
                </div>
              </dl>
            </div>
          </section>

          <section className="sidebar-panel sidebar-panel--muted">
            <div className="sidebar-panel__heading">WORKFLOW</div>
            <div className="bullet-stack">
              <p>Open a study, shape the tone response, then render a derived DICOM.</p>
              <p>The center canvas stays editor-first while the right inspector carries the controls.</p>
              <p>Compare mode unlocks once both previews exist, just like opening a diff view.</p>
            </div>
          </section>
        </aside>

        <main className="editor-column">
          <div className="editor-tabs" role="tablist" aria-label="Viewer tabs">
            {viewerTabs.map((tab) => (
              <button
                key={tab.mode}
                className={`editor-tab ${activeMode === tab.mode ? "is-active" : ""}`}
                type="button"
                onClick={() => setActiveMode(tab.mode)}
                disabled={tab.disabled}
              >
                <span className="editor-tab__name">{tab.label}</span>
                <span className="editor-tab__state">{tab.caption}</span>
              </button>
            ))}
          </div>

          <ViewerStage
            activeMode={activeMode}
            canCompare={canCompare}
            originalPreviewUrl={session.originalPreviewUrl}
            processedPreviewUrl={session.processedPreviewUrl}
            originalMeasurementScale={session.originalMeasurementScale}
            processedMeasurementScale={session.processedMeasurementScale}
            recipeName={recipeName}
            toneLabel={toneLabel}
            paletteLabel={paletteLabel(controls.palette)}
            dirty={session.dirty}
          />

          <section className="bottom-panel">
            <div className="bottom-panel__tabs">
              <span className="bottom-panel__tab is-active">OUTPUT</span>
              <span className="bottom-panel__tab">PIPELINE</span>
              <span className="bottom-panel__tab">EXPORT</span>
            </div>

            <div className="bottom-panel__body">
              <div className="terminal-log">
                <div className="terminal-log__label">{busy ? "TASK RUNNING" : "SESSION OUTPUT"}</div>
                <p className="terminal-log__line">{session.status}</p>
                <p className="terminal-log__line u-mono">source {compactPath(session.inputPath)}</p>
                <p className="terminal-log__line u-mono">derived {compactPath(session.processedDicomPath)}</p>
              </div>

              <dl className="data-list data-list--inline">
                <div>
                  <dt>Recipe</dt>
                  <dd>{recipeName}</dd>
                </div>
                <div>
                  <dt>Tone</dt>
                  <dd>{toneLabel}</dd>
                </div>
                <div>
                  <dt>Palette</dt>
                  <dd>{paletteLabel(controls.palette)}</dd>
                </div>
                <div>
                  <dt>Output</dt>
                  <dd>{describeOutput(session)}</dd>
                </div>
              </dl>
            </div>
          </section>
        </main>

        <aside className="inspector-sidebar">
          <section className="inspector-panel">
            <div className="sidebar-panel__heading">INSPECTOR</div>
            <h2 className="inspector-panel__title">Processing Controls</h2>
            <p className="inspector-panel__description">
              VSCode-style right rail for presets, tone controls, and render safety state.
            </p>

              <ProcessingLab
                controls={controls}
                presets={processingState.presets}
                busy={busy}
                dirty={session.dirty}
                onPresetSelect={applyPreset}
              onChange={updateControls}
            />
          </section>

          <section className="inspector-panel inspector-panel--muted">
            <div className="sidebar-panel__heading">EXPORT STATE</div>
            <dl className="data-list data-list--compact">
              <div>
                <dt>Save path</dt>
                <dd className="u-mono">{compactPath(session.savedDestination)}</dd>
              </div>
              <div>
                <dt>Temporary output</dt>
                <dd className="u-mono">{compactPath(session.processedDicomPath)}</dd>
              </div>
              <div>
                <dt>Save lock</dt>
                <dd>{canSave ? "Ready to export" : "Render required before save"}</dd>
              </div>
            </dl>
          </section>
        </aside>
      </div>
    </div>
  );
}
