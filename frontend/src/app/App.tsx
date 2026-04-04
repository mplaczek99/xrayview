import { useEffect, useState } from "react";
import { ProcessingTab } from "../components/processing/ProcessingTab";
import { ViewTab } from "../components/viewer/ViewTab";
import { JobCenter } from "../features/jobs/JobCenter";
import { useJobs } from "../features/jobs/useJobs";
import { workbenchActions } from "./store/workbenchStore";
import type { ActiveTab } from "../lib/types";

export function App() {
  const [activeTab, setActiveTab] = useState<ActiveTab>("view");
  useJobs();

  useEffect(() => {
    void workbenchActions.ensureManifest();
  }, []);

  return (
    <div className="app-shell">
      <h1 className="sr-only">XRayView</h1>
      <nav className="tab-bar" role="tablist" aria-label="Main tabs">
        <button
          id="tab-view"
          className={`tab-bar__tab${activeTab === "view" ? " tab-bar__tab--active" : ""}`}
          type="button"
          role="tab"
          aria-selected={activeTab === "view"}
          aria-controls="tabpanel-view"
          onClick={() => setActiveTab("view")}
        >
          View
        </button>
        <button
          id="tab-processing"
          className={`tab-bar__tab${activeTab === "processing" ? " tab-bar__tab--active" : ""}`}
          type="button"
          role="tab"
          aria-selected={activeTab === "processing"}
          aria-controls="tabpanel-processing"
          onClick={() => setActiveTab("processing")}
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
