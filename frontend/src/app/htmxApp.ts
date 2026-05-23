import type htmx from "htmx.org";
import { startJobSync } from "../features/jobs/jobSync";
import { buildProcessingUiState } from "../features/processing/presets";
import { ViewerController } from "../features/viewer/ViewerController";
import type { ProcessingControls } from "../lib/generated/contracts";
import type { ActiveTab } from "../lib/types";
import {
  type CompareView,
  escapeHtml,
  type HtmxUiState,
  renderApp,
  selectViewerRenderModel,
} from "./htmxView";
import {
  getWorkbenchState,
  subscribeWorkbenchStore,
  workbenchActions,
} from "./store/workbenchStore";

type HtmxApi = typeof htmx;

const TABS: ActiveTab[] = ["view", "processing"];

function clamp(value: number, min: number, max: number): number | null {
  if (Number.isNaN(value)) {
    return null;
  }
  return Math.min(max, Math.max(min, value));
}

function isButtonDisabled(element: HTMLElement): boolean {
  return element instanceof HTMLButtonElement && element.disabled;
}

function isLiveWorkbench() {
  const state = getWorkbenchState();
  if (state.pendingJobIds.size > 0) {
    return true;
  }

  const activeStudy = state.activeStudyId ? (state.studies[state.activeStudyId] ?? null) : null;
  const runStatus = activeStudy?.processing.runStatus;
  return runStatus?.state === "running" || runStatus?.state === "cancelling";
}

class HtmxWorkbenchApp {
  private readonly viewer = new ViewerController();
  private readonly ui: HtmxUiState = {
    activeTab: "view",
    compareView: "processed",
    jobsExpanded: false,
    dismissedJobIds: new Set<string>(),
  };
  private unsubscribeStore: (() => void) | null = null;
  private stopJobSync: (() => void) | null = null;
  private renderQueued = false;
  private clockTimer = 0;

  constructor(
    private readonly root: HTMLElement,
    private readonly htmxApi: HtmxApi,
  ) {}

  start() {
    this.root.addEventListener("click", this.handleClick);
    this.root.addEventListener("input", this.handleInput);
    this.root.addEventListener("change", this.handleChange);
    this.root.addEventListener("keydown", this.handleKeyDown);
    this.unsubscribeStore = subscribeWorkbenchStore(() => this.scheduleRender());
    this.stopJobSync = startJobSync();
    this.render();
    void workbenchActions.ensureManifest();
  }

  destroy() {
    this.root.removeEventListener("click", this.handleClick);
    this.root.removeEventListener("input", this.handleInput);
    this.root.removeEventListener("change", this.handleChange);
    this.root.removeEventListener("keydown", this.handleKeyDown);
    this.unsubscribeStore?.();
    this.stopJobSync?.();
    this.viewer.detach();
    if (this.clockTimer) {
      window.clearInterval(this.clockTimer);
      this.clockTimer = 0;
    }
  }

  private scheduleRender() {
    if (this.renderQueued) {
      return;
    }

    this.renderQueued = true;
    queueMicrotask(() => {
      this.renderQueued = false;
      this.render();
    });
  }

  private render() {
    const state = getWorkbenchState();
    try {
      this.viewer.detach();
      this.htmxApi.swap(this.root, renderApp(state, this.ui, Date.now()), {
        swapStyle: "innerHTML",
        swapDelay: 0,
        settleDelay: 0,
      });
      this.htmxApi.process(this.root);
      this.viewer.mount(this.root, selectViewerRenderModel(state));
      this.syncClock();
    } catch (error) {
      console.error("xrayview htmx render error", error);
      this.root.innerHTML = `
        <div class="viewer-stage">
          <div class="viewer-placeholder">
            <div class="viewer-placeholder__title">Frontend Error</div>
            <p class="viewer-placeholder__copy">${escapeHtml(
              error instanceof Error ? error.message : String(error),
            )}</p>
            <button class="button button--primary" type="button" data-action="reload">
              Reload
            </button>
          </div>
        </div>
      `;
    }
  }

  private syncClock() {
    const shouldTick = isLiveWorkbench();
    if (shouldTick && !this.clockTimer) {
      this.clockTimer = window.setInterval(() => {
        if (isLiveWorkbench()) {
          this.render();
        } else if (this.clockTimer) {
          window.clearInterval(this.clockTimer);
          this.clockTimer = 0;
        }
      }, 1000);
    } else if (!shouldTick && this.clockTimer) {
      window.clearInterval(this.clockTimer);
      this.clockTimer = 0;
    }
  }

  private handleClick = (event: MouseEvent) => {
    const target = event.target instanceof Element ? event.target : null;
    const actionElement = target?.closest<HTMLElement>("[data-action]");
    if (!actionElement || !this.root.contains(actionElement)) {
      return;
    }
    if (isButtonDisabled(actionElement)) {
      return;
    }

    const action = actionElement.dataset.action;
    switch (action) {
      case "reload":
        window.location.reload();
        break;
      case "set-tab":
        this.setTab(actionElement.dataset.tab);
        break;
      case "open-study":
        void workbenchActions.openStudy();
        break;
      case "set-viewer-tool":
        if (actionElement.dataset.tool === "pan" || actionElement.dataset.tool === "measureLine") {
          workbenchActions.setViewerTool(actionElement.dataset.tool);
        }
        break;
      case "analyze-study":
        void workbenchActions.runActiveStudyAnalysis();
        break;
      case "set-analysis-overlay":
        if (actionElement.dataset.mode === "outline" || actionElement.dataset.mode === "sections") {
          workbenchActions.setAnalysisOverlayMode(actionElement.dataset.mode);
        }
        break;
      case "remove-annotation":
        workbenchActions.deleteSelectedAnnotation();
        break;
      case "reset-viewer":
        this.viewer.resetViewport();
        break;
      case "select-annotation":
        workbenchActions.selectAnnotation(actionElement.dataset.annotationId ?? null);
        break;
      case "set-compare-view":
        this.setCompareView(actionElement.dataset.compareView);
        break;
      case "run-processing":
        void workbenchActions.runActiveStudyProcessing();
        break;
      case "cancel-job":
        if (actionElement.dataset.jobId) {
          void workbenchActions.cancelJob(actionElement.dataset.jobId);
        }
        break;
      case "toggle-jobs":
        this.ui.jobsExpanded = !this.ui.jobsExpanded;
        this.render();
        break;
      case "clear-terminal-jobs":
        this.clearTerminalJobs();
        break;
      case "dismiss-job":
        if (actionElement.dataset.jobId) {
          this.ui.dismissedJobIds = new Set([
            ...this.ui.dismissedJobIds,
            actionElement.dataset.jobId,
          ]);
          this.render();
        }
        break;
    }
  };

  private handleInput = (event: Event) => {
    const target = event.target;
    if (!(target instanceof HTMLInputElement)) {
      return;
    }
    if (target.dataset.action !== "set-processing-control") {
      return;
    }
    if (target.type !== "range" && target.type !== "number") {
      return;
    }
    this.updateProcessingControl(target);
  };

  private handleChange = (event: Event) => {
    const target = event.target;
    if (!(target instanceof HTMLInputElement) && !(target instanceof HTMLSelectElement)) {
      return;
    }

    switch (target.dataset.action) {
      case "set-processing-control":
        this.updateProcessingControl(target);
        break;
      case "set-processing-preset":
        if (target instanceof HTMLSelectElement) {
          this.applyProcessingPreset(target.value);
        }
        break;
      case "set-processing-compare":
        if (target instanceof HTMLInputElement) {
          workbenchActions.setProcessingCompare(target.checked);
        }
        break;
    }
  };

  private handleKeyDown = (event: KeyboardEvent) => {
    const target = event.target instanceof HTMLElement ? event.target : null;
    if (!target?.matches('[role="tab"]')) {
      return;
    }

    const currentIndex = TABS.indexOf(this.ui.activeTab);
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

    if (nextIndex === null) {
      return;
    }

    event.preventDefault();
    const nextTab = TABS[nextIndex];
    this.ui.activeTab = nextTab;
    this.render();
    window.requestAnimationFrame(() => {
      this.root.querySelector<HTMLElement>(`#tab-${nextTab}`)?.focus();
    });
  };

  private setTab(value: string | undefined) {
    if (value !== "view" && value !== "processing") {
      return;
    }
    if (this.ui.activeTab === value) {
      return;
    }
    this.ui.activeTab = value;
    this.render();
  }

  private setCompareView(value: string | undefined) {
    if (value !== "original" && value !== "processed" && value !== "split") {
      return;
    }
    this.ui.compareView = value as CompareView;
    this.render();
  }

  private applyProcessingPreset(presetId: string) {
    const processingUi = buildProcessingUiState(getWorkbenchState().manifest);
    const preset = processingUi.presets.find((candidate) => candidate.id === presetId);
    if (!preset) {
      return;
    }
    workbenchActions.setProcessingControls({ ...preset.controls });
  }

  private updateProcessingControl(target: HTMLInputElement | HTMLSelectElement) {
    const control = target.dataset.control as keyof ProcessingControls | undefined;
    if (!control) {
      return;
    }

    switch (control) {
      case "invert":
      case "equalize":
        if (target instanceof HTMLInputElement) {
          workbenchActions.setProcessingControl(control, target.checked);
        }
        break;
      case "brightness": {
        const value = clamp(parseInt(target.value, 10), -100, 100);
        if (value !== null) {
          workbenchActions.setProcessingControl(control, value);
        }
        break;
      }
      case "contrast": {
        const value = clamp(parseFloat(target.value), 0.1, 3);
        if (value !== null) {
          workbenchActions.setProcessingControl(control, value);
        }
        break;
      }
      case "palette":
        if (target.value === "none" || target.value === "hot" || target.value === "bone") {
          workbenchActions.setProcessingControl(control, target.value);
        }
        break;
    }
  }

  private clearTerminalJobs() {
    const state = getWorkbenchState();
    const next = new Set(this.ui.dismissedJobIds);
    for (const job of Object.values(state.jobs)) {
      if (job.state === "completed" || job.state === "failed" || job.state === "cancelled") {
        next.add(job.jobId);
      }
    }
    this.ui.dismissedJobIds = next;
    this.render();
  }
}

export function mountHtmxApp(root: HTMLElement, htmxApi: HtmxApi): () => void {
  const app = new HtmxWorkbenchApp(root, htmxApi);
  app.start();
  return () => app.destroy();
}
