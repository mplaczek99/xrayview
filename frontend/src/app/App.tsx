import { useEffect, useRef, useState } from "react";
import { ProcessingTab } from "../components/processing/ProcessingTab";
import { ViewTab } from "../components/viewer/ViewTab";
import { JobCenter } from "../features/jobs/JobCenter";
import { useJobs } from "../features/jobs/useJobs";
import { workbenchActions } from "./store/workbenchStore";
import type { ActiveTab } from "../lib/types";

const TABS: ActiveTab[] = ["view", "processing"];

export function App() {
  const [activeTab, setActiveTab] = useState<ActiveTab>("view");
  const tabRefs = useRef<Record<ActiveTab, HTMLButtonElement | null>>({
    view: null,
    processing: null,
  });
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
        {activeTab === "view" ? <ViewTab /> : <ProcessingTab />}
      </main>

      <JobCenter />
    </div>
  );
}
