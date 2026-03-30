interface TopBarProps {
  busy: boolean;
  runtimeLabel: string;
  statusLabel: string;
  onOpenStudy: () => void;
  onRenderOutput: () => void;
  onSaveOutput: () => void;
  canRender: boolean;
  canSave: boolean;
}

export function TopBar({
  busy,
  runtimeLabel,
  statusLabel,
  onOpenStudy,
  onRenderOutput,
  onSaveOutput,
  canRender,
  canSave,
}: TopBarProps) {
  return (
    <header className="topbar">
      <div className="topbar__copy">
        <div className="topbar__kicker">XRAYVIEW NEXT</div>
        <h1 className="topbar__title">Tauri Workstation Baseline</h1>
        <p className="topbar__subtitle">
          A new desktop shell aimed at richer viewer layouts, report design, and faster iteration without
          changing the Go image-processing backend.
        </p>
        <div className="pill-row">
          <span className="pill">Go backend intact</span>
          <span className="pill pill--accent">{runtimeLabel}</span>
          <span className="pill">{statusLabel}</span>
        </div>
      </div>

      <div className="topbar__actions">
        <button className="button button--ghost" type="button" onClick={onOpenStudy} disabled={busy}>
          Open Study
        </button>
        <button className="button button--primary" type="button" onClick={onRenderOutput} disabled={!canRender || busy}>
          {busy ? "Working..." : "Render Output"}
        </button>
        <button className="button button--ghost" type="button" onClick={onSaveOutput} disabled={!canSave || busy}>
          Save DICOM
        </button>
      </div>
    </header>
  );
}
