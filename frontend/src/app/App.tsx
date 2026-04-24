import { lazy, Suspense, useEffect, useRef, useState } from "react";

const ProcessingTab = lazy(() =>
  import("../components/processing/ProcessingTab").then((m) => ({ default: m.ProcessingTab }))
);
import { ViewTab } from "../components/viewer/ViewTab";
import { JobCenter } from "../features/jobs/JobCenter";
import { useJobs } from "../features/jobs/useJobs";
import { workbenchActions, useWorkbenchStore, selectActiveStudy, selectWorkbenchStatus } from "./store/workbenchStore";
import type { ActiveTab } from "../lib/types";

const TABS: ActiveTab[] = ["view", "processing"];

function StatusIcon({ status }: { status: string }) {
  if (/cancelled|canceled/i.test(status)) {
    return (
      <svg className="status-bar__icon" viewBox="0 0 16 16" fill="none" aria-hidden="true">
        <circle cx="8" cy="8" r="5.5" stroke="currentColor" strokeWidth="1.5" />
        <path d="M5.5 5.5l5 5M10.5 5.5l-5 5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
      </svg>
    );
  }
  if (/fail|error/i.test(status)) {
    return (
      <svg className="status-bar__icon status-bar__icon--error" viewBox="0 0 16 16" fill="none" aria-hidden="true">
        <path d="M8 1.5l6.5 12H1.5L8 1.5z" stroke="currentColor" strokeWidth="1.5" strokeLinejoin="round" />
        <path d="M8 6.5v3M8 11.5v.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
      </svg>
    );
  }
  if (/loaded|complete|success/i.test(status)) {
    return (
      <svg className="status-bar__icon status-bar__icon--success" viewBox="0 0 16 16" fill="none" aria-hidden="true">
        <path d="M3.5 8.5l3 3 6-7" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
    );
  }
  if (/opening|running|analyzing|processing|cancelling/i.test(status)) {
    return <span className="status-bar__spinner" aria-hidden="true" />;
  }
  return (
    <svg className="status-bar__icon" viewBox="0 0 16 16" fill="none" aria-hidden="true">
      <circle cx="8" cy="8" r="5.5" stroke="currentColor" strokeWidth="1.5" />
      <path d="M8 5v3l2 2" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  );
}

export function App() {
  const [activeTab, setActiveTab] = useState<ActiveTab>("view");
  const tabRefs = useRef<Record<ActiveTab, HTMLButtonElement | null>>({
    view: null,
    processing: null,
  });
  const study = useWorkbenchStore(selectActiveStudy);
  const workbenchStatus = useWorkbenchStore(selectWorkbenchStatus);
  const status = study?.status ?? workbenchStatus;
  useJobs();

  useEffect(() => {
    void workbenchActions.ensureManifest();
  }, []);

  function handleTabKeyDown(event: React.KeyboardEvent<HTMLButtonElement>) {
    const currentIndex = TABS.indexOf(activeTab);
    let nextIndex: number | null = null;

    if (event.key === "ArrowRight" || event.key === "ArrowDown") {
      nextIndex = (currentIndex + 1) % TABS.length;
    } else if (event.key === "ArrowLeft" || event.key === "ArrowUp") {
      nextIndex = (currentIndex - 1 + TABS.length) % TABS.length;
    } else if (event.key === "Home") {
      nextIndex = 0;
    } else if (event.key === "End") {
      nextIndex = TABS.length - 1;
    }

    if (nextIndex !== null) {
      event.preventDefault();
      const nextTab = TABS[nextIndex];
      setActiveTab(nextTab);
      tabRefs.current[nextTab]?.focus();
    }
  }

  return (
    <div className="app-shell">
      <h1 className="sr-only">XRayView</h1>
      <nav className="tab-bar" role="tablist" aria-label="Main tabs">
        <button
          ref={(el) => { tabRefs.current.view = el; }}
          id="tab-view"
          className={`tab-bar__tab${activeTab === "view" ? " tab-bar__tab--active" : ""}`}
          type="button"
          role="tab"
          tabIndex={activeTab === "view" ? 0 : -1}
          aria-selected={activeTab === "view"}
          aria-controls="tabpanel-view"
          onClick={() => setActiveTab("view")}
          onKeyDown={handleTabKeyDown}
        >
          View
        </button>
        <button
          ref={(el) => { tabRefs.current.processing = el; }}
          id="tab-processing"
          className={`tab-bar__tab${activeTab === "processing" ? " tab-bar__tab--active" : ""}`}
          type="button"
          role="tab"
          tabIndex={activeTab === "processing" ? 0 : -1}
          aria-selected={activeTab === "processing"}
          aria-controls="tabpanel-processing"
          onClick={() => setActiveTab("processing")}
          onKeyDown={handleTabKeyDown}
        >
          Processing
        </button>
      </nav>

      <main
        id={`tabpanel-${activeTab}`}
        className="tab-content"
        role="tabpanel"
        aria-labelledby={`tab-${activeTab}`}
      >
        {activeTab === "view" ? <ViewTab /> : <Suspense><ProcessingTab /></Suspense>}
      </main>

      <div className="status-bar" role="status" aria-live="polite">
        <StatusIcon status={status} />
        <span className="status-bar__text">{status}</span>
      </div>

      <JobCenter />
    </div>
  );
}
