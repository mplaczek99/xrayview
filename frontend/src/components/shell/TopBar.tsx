interface TopBarProps {
  busy: boolean;
  workspaceName: string;
  recipeName: string;
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
  workspaceName,
  recipeName,
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
      <div className="topbar__workspace">
        <div className="topbar__kicker">XRAYVIEW WORKBENCH</div>
        <div className="topbar__titleline">
          <h1 className="topbar__title">{workspaceName}</h1>
          <span className="topbar__context">{recipeName}</span>
        </div>
      </div>

      <div className="topbar__command">
        <div className="topbar__command-label">COMMAND CENTER</div>
        <div className="topbar__command-bar u-mono">{`study:${workspaceName} recipe:${recipeName}`}</div>
      </div>

      <div className="topbar__status pill-row">
        <span className="pill">Rust backend</span>
        <span className="pill pill--accent">{runtimeLabel}</span>
        <span className="pill">{statusLabel}</span>
      </div>

      <div className="topbar__actions">
        <button className="button button--ghost" type="button" onClick={onOpenStudy} disabled={busy}>
          Open
        </button>
        <button className="button button--primary" type="button" onClick={onRenderOutput} disabled={!canRender || busy}>
          {busy ? "Working..." : "Render"}
        </button>
        <button className="button button--ghost" type="button" onClick={onSaveOutput} disabled={!canSave || busy}>
          Export
        </button>
      </div>
    </header>
  );
}
