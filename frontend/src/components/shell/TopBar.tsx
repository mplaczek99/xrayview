interface TopBarProps {
  workspaceName: string;
  runtimeLabel: string;
}

export function TopBar({ workspaceName, runtimeLabel }: TopBarProps) {
  return (
    <header className="topbar">
      <div className="topbar__workspace">
        <div className="topbar__kicker">XRAYVIEW</div>
        <h1 className="topbar__title">{workspaceName}</h1>
      </div>

      <div className="topbar__status pill-row">
        <span className="pill pill--accent">{runtimeLabel}</span>
      </div>
    </header>
  );
}
