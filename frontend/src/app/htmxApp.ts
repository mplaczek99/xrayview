import type htmx from "htmx.org";
import { startJobSync } from "../features/jobs/jobSync";
import type { JobSnapshot, ProcessingRunState } from "../features/jobs/model";
import { describeProgress } from "../features/jobs/progressFormatting";
import { clampPercent } from "../features/jobs/progressTiming";
import { buildProcessingUiState } from "../features/processing/presets";
import type { WorkbenchState, WorkbenchStudy } from "../features/study/model";
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

const TABS: readonly ActiveTab[] = ["view", "processing"] as const;
const objectIds = new WeakMap<object, number>();
let nextObjectId = 1;

function clamp(value: number, min: number, max: number): number | null {
  if (!Number.isFinite(value)) {
    return null;
  }
  return Math.min(max, Math.max(min, value));
}

function isCompareView(value: string | undefined): value is CompareView {
  return value === "original" || value === "processed" || value === "split";
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

function isTerminalJob(job: JobSnapshot): boolean {
  return job.state === "completed" || job.state === "failed" || job.state === "cancelled";
}

function activeStudy(state: WorkbenchState): WorkbenchStudy | null {
  return state.activeStudyId ? (state.studies[state.activeStudyId] ?? null) : null;
}

function visibleJobs(state: WorkbenchState, ui: HtmxUiState): JobSnapshot[] {
  return state.jobOrder
    .map((jobId) => state.jobs[jobId])
    .filter((job): job is JobSnapshot => Boolean(job))
    .filter((job) => !ui.dismissedJobIds.has(job.jobId))
    .slice(0, 6);
}

function objectRefId(value: object | null | undefined): number | null {
  if (!value) {
    return null;
  }

  const existing = objectIds.get(value);
  if (existing) {
    return existing;
  }

  const id = nextObjectId;
  nextObjectId += 1;
  objectIds.set(value, id);
  return id;
}

function stableRunStatus(runStatus: ProcessingRunState): string {
  if (runStatus.state === "running" || runStatus.state === "cancelling") {
    return [runStatus.state, runStatus.jobId, Boolean(runStatus.timing)].join(":");
  }

  return JSON.stringify(runStatus);
}

function stableStudy(study: WorkbenchStudy): string {
  return JSON.stringify({
    studyId: study.studyId,
    inputPath: study.inputPath,
    inputName: study.inputName,
    measurementScale: objectRefId(study.measurementScale),
    originalPreview: objectRefId(study.originalPreview),
    analysisPreview: objectRefId(study.analysisPreview),
    annotations: objectRefId(study.annotations),
    viewer: study.viewer,
    processing: {
      form: objectRefId(study.processing.form),
      output: objectRefId(study.processing.output),
      runStatus: stableRunStatus(study.processing.runStatus),
    },
    runtime: study.runtime,
    renderJobId: study.renderJobId,
    analysisJobId: study.analysisJobId,
  });
}

function stableJob(job: JobSnapshot): string {
  return JSON.stringify({
    jobId: job.jobId,
    jobKind: job.jobKind,
    studyId: job.studyId,
    state: job.state,
    fromCache: job.fromCache,
    result: isTerminalJob(job) ? objectRefId(job.result) : null,
    error: isTerminalJob(job) ? objectRefId(job.error) : null,
    hasTiming: Boolean(job.timing),
  });
}

function liveRenderSignature(state: WorkbenchState, ui: HtmxUiState): string {
  return JSON.stringify({
    ui: {
      activeTab: ui.activeTab,
      compareView: ui.compareView,
      jobsExpanded: ui.jobsExpanded,
      dismissedJobIds: [...ui.dismissedJobIds].sort(),
    },
    manifest: objectRefId(state.manifest),
    manifestStatus: state.manifestStatus,
    activeStudyId: state.activeStudyId,
    studies: Object.fromEntries(
      Object.entries(state.studies).map(([studyId, study]) => [studyId, stableStudy(study)]),
    ),
    studyOrder: state.studyOrder,
    jobs: Object.fromEntries(
      Object.entries(state.jobs).map(([jobId, job]) => [jobId, stableJob(job)]),
    ),
    jobOrder: state.jobOrder,
    pendingJobIds: [...state.pendingJobIds].sort(),
    isOpeningStudy: state.isOpeningStudy,
  });
}

function statusIconKind(status: string): string {
  if (/cancelled|canceled/i.test(status)) {
    return "cancelled";
  }
  if (/fail|error/i.test(status)) {
    return "error";
  }
  if (/loaded|complete|success/i.test(status)) {
    return "success";
  }
  if (/opening|running|analyzing|processing|cancelling/i.test(status)) {
    return "spinner";
  }
  return "default";
}

function currentStatusIconKind(statusBar: HTMLElement): string {
  if (statusBar.querySelector(".status-bar__spinner")) {
    return "spinner";
  }
  if (statusBar.querySelector(".status-bar__icon--error")) {
    return "error";
  }
  if (statusBar.querySelector(".status-bar__icon--success")) {
    return "success";
  }
  const text = statusBar.textContent ?? "";
  if (/cancelled|canceled/i.test(text)) {
    return "cancelled";
  }
  return "default";
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
  private renderedLiveSignature = "";

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
      this.renderFastPathOrFull();
    });
  }

  private renderFastPathOrFull() {
    const state = getWorkbenchState();
    const signature = liveRenderSignature(state, this.ui);
    if (signature === this.renderedLiveSignature && this.patchLiveProgress(state, Date.now())) {
      this.syncClock();
      return;
    }

    this.render();
  }

  private render() {
    const state = getWorkbenchState();
    const viewerModel = selectViewerRenderModel(state);
    const signature = liveRenderSignature(state, this.ui);
    const preservedViewerStage =
      this.ui.activeTab === "view" ? this.viewer.reusableStageFor(viewerModel) : null;
    const html = renderApp(state, this.ui, Date.now());

    try {
      if (!preservedViewerStage) {
        this.viewer.detach();
      }
      if (preservedViewerStage) {
        if (!this.patchAppShellPreservingViewer(html, preservedViewerStage)) {
          this.viewer.detach();
          this.htmxApi.swap(this.root, html, {
            swapStyle: "innerHTML",
            swapDelay: 0,
            settleDelay: 0,
          });
        }
      } else {
        this.htmxApi.swap(this.root, html, {
          swapStyle: "innerHTML",
          swapDelay: 0,
          settleDelay: 0,
        });
      }
      this.htmxApi.process(this.root);
      this.viewer.mount(this.root, viewerModel);
      this.renderedLiveSignature = signature;
      this.syncClock();
    } catch (error) {
      console.error("xrayview htmx render error", error);
      this.renderedLiveSignature = "";
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

  private patchAppShellPreservingViewer(html: string, viewerStage: HTMLElement): boolean {
    const template = document.createElement("template");
    template.innerHTML = html.trim();

    const currentShell = this.root.querySelector<HTMLElement>(".app-shell");
    const nextShell = template.content.querySelector<HTMLElement>(".app-shell");
    const currentMain = currentShell?.querySelector<HTMLElement>(".tab-content") ?? null;
    const nextMain = nextShell?.querySelector<HTMLElement>(".tab-content") ?? null;
    const currentViewTab = currentMain?.querySelector<HTMLElement>(".view-tab") ?? null;
    const nextViewTab = nextMain?.querySelector<HTMLElement>(".view-tab") ?? null;
    const currentViewerHost =
      currentViewTab?.querySelector<HTMLElement>(".study-layout__viewer") ?? null;
    const nextViewerHost = nextViewTab?.querySelector<HTMLElement>(".study-layout__viewer") ?? null;

    if (
      !currentShell ||
      !nextShell ||
      !currentMain ||
      !nextMain ||
      !currentViewTab ||
      !nextViewTab ||
      !currentViewerHost ||
      !nextViewerHost ||
      !currentViewerHost.contains(viewerStage)
    ) {
      return false;
    }

    this.replaceRequiredChild(currentShell, nextShell, ".tab-bar");
    this.copyAttributes(currentMain, nextMain);
    this.copyAttributes(currentViewTab, nextViewTab);
    this.copyAttributes(currentViewerHost, nextViewerHost);

    this.patchViewToolbar(currentViewTab, nextViewTab);
    this.patchAnalysisProgress(currentViewerHost, nextViewerHost);
    this.replaceRequiredChild(currentViewTab, nextViewTab, ".study-layout__sidebar");
    this.patchStatusBar(currentShell, nextShell);
    this.patchOptionalShellChild(currentShell, nextShell, ".job-center");

    return true;
  }

  private copyAttributes(target: HTMLElement, source: HTMLElement) {
    for (const attribute of [...target.attributes]) {
      target.removeAttribute(attribute.name);
    }
    for (const attribute of [...source.attributes]) {
      target.setAttribute(attribute.name, attribute.value);
    }
  }

  private replaceRequiredChild(parent: HTMLElement, sourceParent: HTMLElement, selector: string) {
    const current = parent.querySelector<HTMLElement>(selector);
    const next = sourceParent.querySelector<HTMLElement>(selector);
    if (current && next) {
      current.replaceWith(next);
    }
  }

  private patchViewToolbar(currentViewTab: HTMLElement, nextViewTab: HTMLElement) {
    const current = currentViewTab.querySelector<HTMLElement>(".view-panel__toolbar");
    const next = nextViewTab.querySelector<HTMLElement>(".view-panel__toolbar");
    if (!current || !next) {
      return;
    }

    this.copyAttributes(current, next);
    const currentChildren = [...current.children];
    const nextChildren = [...next.children];
    for (let index = 0; index < nextChildren.length; index += 1) {
      const currentChild = currentChildren[index];
      const nextChild = nextChildren[index];
      if (!(nextChild instanceof HTMLElement)) {
        continue;
      }
      if (
        currentChild instanceof HTMLElement &&
        currentChild.querySelector("[data-action='analyze-study']") &&
        nextChild.querySelector("[data-action='analyze-study']")
      ) {
        this.patchAnalysisToolbarGroup(currentChild, nextChild);
      } else if (currentChild) {
        currentChild.replaceWith(nextChild);
      } else {
        current.append(nextChild);
      }
    }
    for (const extra of currentChildren.slice(nextChildren.length)) {
      extra.remove();
    }
  }

  private patchAnalysisToolbarGroup(currentGroup: HTMLElement, nextGroup: HTMLElement) {
    const currentButton = currentGroup.querySelector<HTMLButtonElement>(
      "[data-action='analyze-study']",
    );
    const nextButton = nextGroup.querySelector<HTMLButtonElement>("[data-action='analyze-study']");
    const currentSpinner = currentButton?.querySelector<HTMLElement>(".analysis-button__spinner");
    const nextSpinner = nextButton?.querySelector<HTMLElement>(".analysis-button__spinner");

    if (!currentButton || !nextButton || !currentSpinner || !nextSpinner) {
      currentGroup.replaceWith(nextGroup);
      return;
    }

    this.copyAttributes(currentGroup, nextGroup);
    this.copyAttributes(currentButton, nextButton);
    const currentLabel = currentButton.querySelector<HTMLElement>("span:not(.spinner)");
    const nextLabel = nextButton.querySelector<HTMLElement>("span:not(.spinner)");
    if (currentLabel && nextLabel) {
      currentLabel.textContent = nextLabel.textContent;
      this.copyAttributes(currentLabel, nextLabel);
    }

    for (const child of [...currentGroup.children]) {
      if (child !== currentButton) {
        child.remove();
      }
    }
    for (const child of [...nextGroup.children]) {
      if (child !== nextButton) {
        currentGroup.append(child);
      }
    }
  }

  private patchStatusBar(currentShell: HTMLElement, nextShell: HTMLElement) {
    const current = currentShell.querySelector<HTMLElement>(".status-bar");
    const next = nextShell.querySelector<HTMLElement>(".status-bar");
    const currentSpinner = current?.querySelector<HTMLElement>(".status-bar__spinner");
    const nextSpinner = next?.querySelector<HTMLElement>(".status-bar__spinner");

    if (!current || !next) {
      return;
    }
    if (!currentSpinner || !nextSpinner) {
      current.replaceWith(next);
      return;
    }

    this.copyAttributes(current, next);
    this.copyAttributes(currentSpinner, nextSpinner);
    const currentText = current.querySelector<HTMLElement>(".status-bar__text");
    const nextText = next.querySelector<HTMLElement>(".status-bar__text");
    if (currentText && nextText) {
      currentText.textContent = nextText.textContent;
      this.copyAttributes(currentText, nextText);
    }
  }

  private patchOptionalShellChild(
    parent: HTMLElement,
    sourceParent: HTMLElement,
    selector: string,
  ) {
    const current = parent.querySelector<HTMLElement>(selector);
    const next = sourceParent.querySelector<HTMLElement>(selector);
    if (current && next) {
      current.replaceWith(next);
    } else if (current) {
      current.remove();
    } else if (next) {
      parent.append(next);
    }
  }

  private patchAnalysisProgress(currentViewerHost: HTMLElement, nextViewerHost: HTMLElement) {
    const current = this.directChildByClass(currentViewerHost, "analysis-progress");
    const next = this.directChildByClass(nextViewerHost, "analysis-progress");
    if (current && next) {
      this.copyAttributes(current, next);
      this.patchAnalysisProgressBadge(current, next);
    } else if (current) {
      current.remove();
    } else if (next) {
      currentViewerHost.append(next);
    }
  }

  private patchAnalysisProgressBadge(currentProgress: HTMLElement, nextProgress: HTMLElement) {
    const currentBadge = currentProgress.querySelector<HTMLElement>(".analysis-progress__badge");
    const nextBadge = nextProgress.querySelector<HTMLElement>(".analysis-progress__badge");
    if (!currentBadge || !nextBadge) {
      currentProgress.replaceChildren(...nextProgress.childNodes);
      return;
    }

    this.copyAttributes(currentBadge, nextBadge);
    const currentText = currentBadge.querySelector<HTMLElement>(".analysis-progress__text");
    const nextText = nextBadge.querySelector<HTMLElement>(".analysis-progress__text");
    if (currentText && nextText) {
      currentText.textContent = nextText.textContent;
      this.copyAttributes(currentText, nextText);
    }

    const currentDetail = currentBadge.querySelector<HTMLElement>(".analysis-progress__detail");
    const nextDetail = nextBadge.querySelector<HTMLElement>(".analysis-progress__detail");
    if (currentDetail && nextDetail) {
      currentDetail.textContent = nextDetail.textContent;
      this.copyAttributes(currentDetail, nextDetail);
    } else if (currentDetail) {
      currentDetail.remove();
    } else if (nextDetail) {
      currentBadge.append(nextDetail);
    }
  }

  private directChildByClass(parent: HTMLElement, className: string): HTMLElement | null {
    return (
      Array.from(parent.children).find(
        (child): child is HTMLElement =>
          child instanceof HTMLElement && child.classList.contains(className),
      ) ?? null
    );
  }

  private patchLiveProgress(state: WorkbenchState, nowMs: number): boolean {
    const shell = this.root.querySelector<HTMLElement>(".app-shell");
    if (!shell) {
      return false;
    }

    return (
      this.patchLiveStatusBar(shell, state.workbenchStatus) &&
      this.patchLiveAnalysisProgress(state) &&
      this.patchLiveProcessingRun(state, nowMs) &&
      this.patchLiveJobCenter(shell, state, nowMs)
    );
  }

  private patchLiveStatusBar(shell: HTMLElement, status: string): boolean {
    const statusBar = shell.querySelector<HTMLElement>(".status-bar");
    const statusText = statusBar?.querySelector<HTMLElement>(".status-bar__text");
    if (!statusBar || !statusText) {
      return false;
    }
    if (currentStatusIconKind(statusBar) !== statusIconKind(status)) {
      return false;
    }

    statusText.textContent = status;
    return true;
  }

  private patchLiveAnalysisProgress(state: WorkbenchState): boolean {
    if (this.ui.activeTab !== "view") {
      return true;
    }

    const study = activeStudy(state);
    const job = study?.analysisJobId ? (state.jobs[study.analysisJobId] ?? null) : null;
    const isAnalyzing =
      job?.state === "queued" || job?.state === "running" || job?.state === "cancelling";
    const current = this.root.querySelector<HTMLElement>(
      ".study-layout__viewer > .analysis-progress",
    );

    if (!isAnalyzing || !job) {
      return current === null;
    }
    if (!current) {
      return false;
    }

    const label = job.state === "cancelling" ? "Cancelling analysis..." : "Analyzing...";
    const percent = clampPercent(job.progress.percent);
    const detail = percent > 0 && percent < 100 ? `${Math.round(percent)}%` : null;
    const text = current.querySelector<HTMLElement>(".analysis-progress__text");
    const detailNode = current.querySelector<HTMLElement>(".analysis-progress__detail");
    if (!text || Boolean(detailNode) !== Boolean(detail)) {
      return false;
    }

    current.setAttribute("aria-label", detail ? `${label} ${detail}` : label);
    text.textContent = label;
    if (detailNode && detail) {
      detailNode.textContent = detail;
    }
    return true;
  }

  private patchLiveProcessingRun(state: WorkbenchState, nowMs: number): boolean {
    if (this.ui.activeTab !== "processing") {
      return true;
    }

    const runStatus = activeStudy(state)?.processing.runStatus ?? { state: "idle" as const };
    const busy = runStatus.state === "running" || runStatus.state === "cancelling";
    const message = this.root.querySelector<HTMLElement>("[data-processing-run-message]");

    if (!busy) {
      return message === null;
    }
    if (!message) {
      return false;
    }

    const progressView = describeProgress(
      {
        state: runStatus.state,
        progress: runStatus.progress,
        timing: runStatus.timing,
        fromCache: false,
      },
      nowMs,
    );
    const detail = progressView.detailLabel;
    const detailNode = this.root.querySelector<HTMLElement>("[data-processing-run-detail]");
    if (Boolean(detailNode) !== Boolean(detail)) {
      return false;
    }

    message.textContent = runStatus.progress.message;
    if (detailNode && detail) {
      detailNode.textContent = detail;
    }
    return true;
  }

  private patchLiveJobCenter(shell: HTMLElement, state: WorkbenchState, nowMs: number): boolean {
    const jobs = visibleJobs(state, this.ui);
    const current = shell.querySelector<HTMLElement>(".job-center");
    if (!jobs.length) {
      return current === null;
    }
    if (!current) {
      return false;
    }

    const activeCount = jobs.filter((job) => !isTerminalJob(job)).length;
    const badge = current.querySelector<HTMLElement>(".job-center__badge");
    if (activeCount > 0) {
      if (!badge) {
        return false;
      }
      badge.textContent = String(activeCount);
    } else if (badge) {
      return false;
    }

    if (!this.ui.jobsExpanded) {
      return current.querySelector(".job-center__list") === null;
    }

    const cards = [...current.querySelectorAll<HTMLElement>(".job-card")];
    if (cards.length !== jobs.length) {
      return false;
    }

    for (let index = 0; index < jobs.length; index += 1) {
      if (!this.patchLiveJobCard(cards[index], jobs[index], nowMs)) {
        return false;
      }
    }
    return true;
  }

  private patchLiveJobCard(
    card: HTMLElement | undefined,
    job: JobSnapshot,
    nowMs: number,
  ): boolean {
    if (!card || card.dataset.jobId !== job.jobId || card.dataset.jobState !== job.state) {
      return false;
    }
    if (isTerminalJob(job)) {
      return true;
    }

    const progressView = describeProgress(job, nowMs);
    const progressBar = card.querySelector<HTMLElement>("[data-job-progress-bar]");
    const message = card.querySelector<HTMLElement>("[data-job-message]");
    const detail = card.querySelector<HTMLElement>("[data-job-detail]");
    if (!progressBar || !message || Boolean(detail) !== Boolean(progressView.detailLabel)) {
      return false;
    }

    progressBar.className = `job-card__progress-bar job-card__progress-bar--${job.state}${
      progressView.indeterminate ? " job-card__progress-bar--indeterminate" : ""
    }`;
    if (progressView.indeterminate) {
      progressBar.removeAttribute("style");
    } else {
      const width = Math.max(clampPercent(job.progress.percent), 4);
      progressBar.style.width = `${width}%`;
    }

    message.textContent = job.progress.message;
    if (detail && progressView.detailLabel) {
      detail.textContent = progressView.detailLabel;
    }
    return true;
  }

  private syncClock() {
    const shouldTick = isLiveWorkbench();
    if (shouldTick && !this.clockTimer) {
      this.clockTimer = window.setInterval(() => {
        if (isLiveWorkbench()) {
          const state = getWorkbenchState();
          if (!this.patchLiveProgress(state, Date.now())) {
            this.render();
          }
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
    if (!isCompareView(value)) {
      return;
    }
    this.ui.compareView = value;
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
        const value = clamp(Math.round(Number(target.value)), -100, 100);
        if (value !== null) {
          workbenchActions.setProcessingControl(control, value);
        }
        break;
      }
      case "contrast": {
        const value = clamp(Number(target.value), 0.1, 3);
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
