import { useEffect, useState } from "react";
import { ProcessingTab } from "../components/processing/ProcessingTab";
import { ViewTab } from "../components/viewer/ViewTab";
import { workbenchActions } from "./store/workbenchStore";
import type { ActiveTab } from "../lib/types";

export function App() {
  const [activeTab, setActiveTab] = useState<ActiveTab>("view");

  useEffect(() => {
    void workbenchActions.ensureManifest();
  }, []);

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
        {activeTab === "view" ? <ViewTab /> : <ProcessingTab />}
      </main>
    </div>
  );
}
