# XRayView UI Audit

**Date:** 2026-04-02
**Scope:** Full read-only audit of `frontend/src/` (React 18 + Vite + Tauri, plain CSS with BEM)

---

## High Severity

### H1. Tab panel has no `id`/`aria-controls` linkage
- **File:** `frontend/src/app/App.tsx:19-41`
- **Problem:** The `<nav role="tablist">` buttons use `role="tab"` and `aria-selected`, but the `<main role="tabpanel">` has no `id`, and the tabs have no `aria-controls` pointing to it. Screen readers cannot associate a tab with its panel. The tabpanel also lacks `aria-labelledby` back to the active tab.
- **Severity:** High
- **Fix:** Add `id="tabpanel-view"` / `id="tabpanel-processing"` on the `<main>`, and `aria-controls="tabpanel-..."` + `id="tab-view"` / `id="tab-processing"` on each button. Add `aria-labelledby` on the panel.

### H2. Tab bar is not keyboard-navigable per WAI-ARIA pattern
- **File:** `frontend/src/app/App.tsx:19-38`
- **Problem:** WAI-ARIA tabs require arrow-key navigation between tabs and `tabindex="0"` only on the active tab (`tabindex="-1"` on inactive). Currently all tab buttons have default tabindex, requiring two Tab presses to traverse. No `onKeyDown` handler for left/right arrow movement.
- **Severity:** High
- **Fix:** Implement the [WAI-ARIA Tabs pattern](https://www.w3.org/WAI/ARIA/apg/patterns/tabs/): arrow keys move focus, only the active tab is in tab order.

### H3. Annotation SVG layer has no accessible name
- **File:** `frontend/src/features/annotations/AnnotationLayer.tsx:44-48`
- **Problem:** The `<svg>` element lacks `role="img"` and `aria-label` (or `aria-hidden="true"` if decorative). Screen readers will attempt to traverse every SVG child element. The `DicomViewer` SVG (line 42 in `DicomViewer.tsx`) correctly uses `aria-hidden="true"`, but `AnnotationLayer` does not.
- **Severity:** High
- **Fix:** Add `aria-label="Annotation overlay"` and `role="img"` to the SVG, or `aria-hidden="true"` if the annotation list sidebar is the canonical accessible representation.

### H4. Viewer canvas `onWheel` calls `preventDefault()` with no passive opt-out
- **File:** `frontend/src/features/viewer/ViewerCanvas.tsx:355-366`
- **Problem:** React attaches wheel listeners as passive by default since React 17. Calling `event.preventDefault()` inside a passive listener is a no-op in Chrome and logs a console warning. The zoom gesture silently breaks. This requires a non-passive listener registered via `useEffect` + `addEventListener(..., { passive: false })`.
- **Severity:** High
- **Fix:** Replace the JSX `onWheel` with a `useEffect` that attaches a native non-passive wheel listener to the container element.

### H5. `backdrop-filter: blur(10px)` on `.job-center` causes compositing cost
- **File:** `frontend/src/styles/base.css:95`
- **Problem:** `backdrop-filter: blur()` forces the browser to create an offscreen compositing layer and re-blur every frame that the underlying content changes (e.g., canvas panning). On lower-end GPUs or Linux/WebKitGTK (Tauri's webview), this can cause visible frame drops during interaction. The job center is always docked at the bottom, so the blur is unnecessary.
- **Severity:** High
- **Fix:** Remove `backdrop-filter: blur(10px)` and use a solid or near-solid `background` color instead (`rgba(7, 11, 16, 0.98)` or `var(--bg-deep)`).

### H6. `drop-shadow` filter on selected annotation line
- **File:** `frontend/src/styles/base.css:824`
- **Problem:** `.annotation-layer__line--selected { filter: drop-shadow(0 0 8px ...) }` applies an SVG filter on every frame the selected annotation is visible. Combined with pan/zoom transforms, this is an expensive per-frame repaint. SVG filters do not GPU-accelerate well in WebKitGTK.
- **Severity:** High
- **Fix:** Replace `filter: drop-shadow(...)` with a wider stroke or a duplicate `<line>` underneath with a thicker, semi-transparent stroke to simulate glow without a filter.

---

## Medium Severity

### M1. `useWorkbenchStore` selector creates new closure every render
- **File:** `frontend/src/app/store/workbenchStore.ts:703-708`
- **Problem:** Every call like `useWorkbenchStore((state) => state.jobs)` creates a new inline arrow function each render. `useSyncExternalStore` compares the selector by reference; a new function means it re-runs the selector (and re-subscribes) every render. This doesn't cause infinite loops but defeats memoization. Multiple selectors in `ViewTab.tsx` (lines 68-98) compound this.
- **Severity:** Medium
- **Fix:** Either memoize selectors with `useCallback` at call sites, or provide pre-defined selector functions (e.g., `const selectJobs = (s: WorkbenchState) => s.jobs`).

### M2. `useJobs` polling interval is aggressive (450ms)
- **File:** `frontend/src/features/jobs/useJobs.ts:5`
- **Problem:** Polling every 450ms fires ~133 requests/minute when any job is pending. Each poll iterates all pending jobs in parallel. On a slow backend or under load this can queue up stale requests. The Tauri event system could deliver updates push-style, but currently polling is the only mechanism.
- **Severity:** Medium
- **Fix:** Increase to 1000-2000ms, or switch to Tauri event listeners (`listen("job:progress", ...)`) which the backend already emits per the CLAUDE.md architecture notes.

### M3. No focus-visible styles on interactive elements
- **File:** `frontend/src/styles/base.css` (global)
- **Problem:** There are no `:focus-visible` or `:focus` outline styles defined anywhere. The global `button { border: 0 }` reset (line 29) removes the default focus ring. Keyboard users cannot see which element is focused.
- **Severity:** Medium
- **Fix:** Add a global `:focus-visible` rule, e.g., `*:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }`.

### M4. Body background uses two `radial-gradient` layers
- **File:** `frontend/src/styles/base.css:15-18`
- **Problem:** The body background stacks two radial gradients + one linear gradient. Because body extends the full viewport and gradients are recalculated on resize, this adds a small but measurable paint cost. More importantly, on the Tauri/WebKitGTK renderer, complex body backgrounds can cause full-page repaints when the window resizes.
- **Severity:** Medium
- **Fix:** Simplify to a single linear gradient, or apply the radial gradients to a fixed pseudo-element so they don't trigger body-level repaints.

### M5. No `:disabled` visual style on `.form-select`
- **File:** `frontend/src/styles/base.css:422-424`
- **Problem:** `.form-select:disabled` only sets `opacity: 0.5`. The element still looks clickable (no cursor change). Compare with `.button:disabled` which sets `cursor: not-allowed`.
- **Severity:** Medium
- **Fix:** Add `cursor: not-allowed` to `.form-select:disabled` and `.form-input:disabled`.

### M6. `viewer-canvas__image` uses `opacity: 0` -> `opacity: 1` without `will-change` or transition
- **File:** `frontend/src/styles/base.css:783-794`
- **Problem:** The image starts at `opacity: 0` and jumps to `opacity: 1` when `--ready` class is added. This is a binary toggle, not animated, so it causes a single full-repaint flash. Adding a short `transition: opacity 150ms` would smooth the appearance and signal readiness better.
- **Severity:** Medium
- **Fix:** Add `transition: opacity 150ms ease` to `.viewer-canvas__image`.

### M7. Hard-coded `border-top-color: #007ACC` on active tab
- **File:** `frontend/src/styles/base.css:72`
- **Problem:** Every other color in the UI uses CSS custom properties from `tokens.css`, but the active tab indicator is hard-coded to `#007ACC`. This breaks the design token system and makes theming harder.
- **Severity:** Medium
- **Fix:** Replace with `var(--accent)` or introduce a `--tab-active-indicator` token.

### M8. `processing-tab__form` uses `max-height: calc(100vh - 80px)` with its own `overflow-y: auto`
- **File:** `frontend/src/styles/base.css:347-349`
- **Problem:** The form panel uses a viewport-relative max-height while the parent `.tab-content` already has `overflow-y: auto` (line 83). This creates nested scroll containers. On mobile (after the media query collapses to single column), `max-height: none` is set, but on desktop both scrollbars can appear.
- **Severity:** Medium
- **Fix:** Remove `max-height` and `overflow-y` from `.processing-tab__form`; let the parent `.tab-content` handle scrolling. Or use `flex: 1; min-height: 0; overflow-y: auto` pattern properly.

### M9. Fixed sidebar width `340px` / `380px` doesn't scale
- **File:** `frontend/src/styles/base.css:230,330`
- **Problem:** `.study-analysis` uses `grid-template-columns: minmax(0, 1fr) 340px` and `.processing-tab` uses `1fr 380px`. On narrow desktop windows (800-1024px), the viewer gets squeezed while the sidebar is fixed. No intermediate breakpoint handles this.
- **Severity:** Medium
- **Fix:** Use `minmax(280px, 380px)` for the sidebar column, or add a breakpoint at ~1024px to reduce the sidebar width.

### M10. Annotation list items are `<button>` but lack `role` description
- **File:** `frontend/src/components/viewer/ViewTab.tsx:203-229`
- **Problem:** Each annotation is a `<button>` with nested spans. There's no `aria-label` or `aria-describedby` summarizing the annotation. A screen reader would read all child text nodes concatenated. Also no `aria-current` or `aria-pressed` for the selected state.
- **Severity:** Medium
- **Fix:** Add `aria-label={annotation.label}` and `aria-pressed={isSelected}` to each button.

### M11. `ViewerCanvas` registers global `pointermove`/`pointerup` listeners during every interaction
- **File:** `frontend/src/features/viewer/ViewerCanvas.tsx:232-239`
- **Problem:** The `useEffect` that attaches `pointermove`/`pointerup` to `window` has `draftLine` in its dependency array. Every mouse move during drawing updates `draftLine` state, which re-runs the effect, removing and re-adding the listeners each frame. This is functionally correct but creates unnecessary GC pressure.
- **Severity:** Medium
- **Fix:** Use `useRef` for `draftLine` inside the interaction effect, or use `setPointerCapture` on the container element to avoid window-level listeners entirely.

### M12. Spinner animation runs at 600ms with hard-coded dark colors
- **File:** `frontend/src/styles/base.css:612-620`
- **Problem:** The spinner border colors (`rgba(6, 17, 31, 0.3)` and `#06111f`) are hard-coded dark values that won't be visible against the dark `.button--primary` background. The spinner appears inside the primary button (ProcessingTab.tsx:357) where the button background is a blue gradient -- the dark spinner borders have almost no contrast.
- **Severity:** Medium
- **Fix:** Use `border: 2px solid rgba(255,255,255,0.3); border-top-color: #fff` for the spinner, so it's visible on both light and dark backgrounds.

---

## Low Severity

### L1. ViewTab is ~320 lines with inline data derivation
- **File:** `frontend/src/components/viewer/ViewTab.tsx:66-322`
- **Problem:** The `ViewTab` component does significant data derivation in its render body (lines 67-100) without memoization, and renders both the viewer and a large sidebar with measurement cards. This makes it harder to maintain and means all sidebar cards re-render on any state change.
- **Severity:** Low
- **Fix:** Extract the sidebar into a `ViewSidebar` component. Memoize derived values with `useMemo`.

### L2. ProcessingTab is ~410 lines
- **File:** `frontend/src/components/processing/ProcessingTab.tsx:1-412`
- **Problem:** Similar to ViewTab, this is a large single component handling form state, command preview, and output display.
- **Severity:** Low
- **Fix:** Extract form sections into smaller components (e.g., `GrayscaleControls`, `PipelineEditor`).

### L3. No loading/skeleton state when opening a study
- **File:** `frontend/src/components/viewer/ViewTab.tsx:107-111`
- **Problem:** When `isOpeningStudy` is true, the button text changes to "Opening..." but the viewer area still shows the empty placeholder. There's no skeleton or spinner to indicate work is happening in the main content area.
- **Severity:** Low
- **Fix:** Show a centered spinner or skeleton placeholder in the viewer stage when `isOpeningStudy` is true.

### L4. No transition on tab switch -- content pops in
- **File:** `frontend/src/app/App.tsx:41`
- **Problem:** Switching between View and Processing tabs does a hard conditional render (`activeTab === "view" ? <ViewTab /> : <ProcessingTab />`). There's no fade or slide transition, which can feel abrupt.
- **Severity:** Low
- **Fix:** Use CSS `opacity` transitions with both panels rendered but one hidden, or accept the current behavior as appropriate for a professional tool.

### L5. Font stack references "IBM Plex Sans" but no font loading
- **File:** `frontend/src/styles/tokens.css:21-22`
- **Problem:** `--font-ui: "IBM Plex Sans", Aptos, ...` and `--font-mono: "IBM Plex Mono", ...` are declared but there's no `@font-face` or Google Fonts import. The fonts will only render if pre-installed on the user's OS. Most users will fall through to `Aptos` or `Segoe UI`.
- **Severity:** Low
- **Fix:** Either add `@font-face` declarations / a font CDN import, or remove IBM Plex from the stack to avoid confusion.

### L6. Inconsistent use of hardcoded vs. token-based spacing
- **File:** `frontend/src/styles/base.css` (various)
- **Problem:** Tokens define `--space-1` through `--space-6`, but many rules use raw pixel values: `gap: 4px` (line 509), `gap: 8px` (line 325, 449, 689, 849), `gap: 10px` (line 126), `gap: 12px` (line 297), `padding: 28px` (line 755). This undermines the spacing scale's consistency.
- **Severity:** Low
- **Fix:** Replace hardcoded pixel values with the nearest spacing token for consistency.

### L7. No `<h1>` or heading hierarchy in the app
- **File:** `frontend/src/app/App.tsx`, `ViewTab.tsx`, `ProcessingTab.tsx`
- **Problem:** The app has no heading elements at all. Section titles use `<div class="...eyebrow">` and `<div class="...title">`. Screen readers and document outline depend on headings to navigate.
- **Severity:** Low
- **Fix:** Use `<h1>` for the app name (or visually hidden), `<h2>` for the active tab title, `<h3>` for sidebar section titles.

### L8. `job-center__list` grid `minmax(220px, 1fr)` can overflow on very small viewports
- **File:** `frontend/src/styles/base.css:118-119`
- **Problem:** `grid-template-columns: repeat(auto-fit, minmax(220px, 1fr))` has a `220px` minimum. On viewports narrower than 220px + padding (unlikely but possible in split-screen), the grid overflows.
- **Severity:** Low
- **Fix:** Use `minmax(min(220px, 100%), 1fr)` to prevent overflow.

### L9. `RootErrorBoundary` shows error but no recovery action
- **File:** `frontend/src/main.tsx:28-39`
- **Problem:** The error boundary renders the error message in a placeholder but provides no "Retry" or "Reload" button. The user is stuck.
- **Severity:** Low
- **Fix:** Add a "Reload" button that calls `window.location.reload()`.

### L10. `<pre class="args-preview">` has inconsistent leading whitespace
- **File:** `frontend/src/components/processing/ProcessingTab.tsx:342-344`
- **Problem:** The JSX indentation means the `<pre>` content starts with whitespace: `              xrayview {formatArgPreview(args)}`. This renders as visible leading spaces in the monospace block.
- **Severity:** Low
- **Fix:** Trim the content or use `{`xrayview ${formatArgPreview(args)}`}` on its own line.

### L11. No `aria-live` region for status updates
- **File:** `frontend/src/components/viewer/ViewTab.tsx:155`
- **Problem:** `<p className="view-panel__status">{status}</p>` dynamically updates with job progress, but has no `aria-live` attribute. Screen readers won't announce status changes.
- **Severity:** Low
- **Fix:** Add `aria-live="polite"` to the status paragraph.

### L12. Color contrast on `--text-dim` (#758395) against `--bg` (#0c1117)
- **File:** `frontend/src/styles/tokens.css:14`
- **Problem:** `#758395` on `#0c1117` yields a contrast ratio of ~4.1:1. This passes WCAG AA for normal text (4.5:1 threshold) only marginally for large text. Several UI elements use `--text-dim` at 11px font size (labels, eyebrows), which is below the large-text threshold.
- **Severity:** Low
- **Fix:** Lighten `--text-dim` to ~`#8a9aac` (contrast ratio ~5.2:1) to meet WCAG AA for small text.

### L13. Dual `ResizeObserver` + `window.resize` listener
- **File:** `frontend/src/features/viewer/ViewerCanvas.tsx:103-111`
- **Problem:** The viewer registers both a `ResizeObserver` on the container *and* a `window.resize` event listener. `ResizeObserver` already covers window resizes that affect the element's dimensions. The `window.resize` listener is redundant and adds a small overhead.
- **Severity:** Low
- **Fix:** Remove the `window.addEventListener("resize", ...)` call.

### L14. `touch-action: none` on viewer canvas blocks all touch gestures
- **File:** `frontend/src/styles/base.css:776`
- **Problem:** `touch-action: none` on `.viewer-canvas` prevents the browser from handling any touch gestures (scroll, pinch-zoom, etc.). While this is needed for custom pan/zoom, it also prevents the user from scrolling the page on touch devices if they touch the canvas area.
- **Severity:** Low
- **Fix:** Acceptable for the desktop-first Tauri use case, but add a note. If mobile support is ever needed, use `touch-action: pinch-zoom` to allow native pinch while capturing pan.

---

## Design & UX Recommendations

These go beyond fixing bugs -- they address layout, workflow, and information design choices that would make the app feel more like a professional workstation and less like a developer prototype.

### D1. Toolbar needs visual grouping and icons
- **File:** `frontend/src/components/viewer/ViewTab.tsx:103-153`
- **Problem:** The View toolbar is a flat `flex-wrap` row of 5+ identically-styled buttons: a file action (Open DICOM), an analysis action (Measure tooth), two viewer mode toggles (Pan, Measure line), a destructive action (Remove selected), and a filename label. Nothing visually separates these concerns. Users must read every label to find the right button. The text-only buttons are also slow to scan at the 13px size used.
- **Recommended change:** Group buttons into logical clusters separated by a subtle divider or extra gap: **File** (Open DICOM) | **Tools** (Pan, Measure line) | **Analysis** (Measure tooth, Remove selected). Add small inline SVG icons (a folder, a hand, a ruler, a crosshair, a trash can) alongside the text labels. The `button--ghost` + `viewer-tool--active` pattern already works well for toggles -- just give them a visual home.
- **Impact:** High -- this is the primary control surface and is used every session.

### D2. Empty state should be a full-viewport onboarding CTA
- **File:** `frontend/src/components/viewer/ViewTab.tsx:157-178`, `frontend/src/styles/base.css:751-769`
- **Problem:** When no study is loaded, the user sees a full two-column layout: a viewer area with a small dashed-border placeholder ("No study loaded") and a sidebar with 5 measurement cards that all say some variant of "no data." The grid, the cards, the eyebrow labels -- all of this is structural noise around a single message: "open a file."
- **Recommended change:** When `!study`, render a single centered empty state that fills the `.tab-content` area instead of the two-column grid. Show a large icon (or the app logo), the "Open DICOM" button as a prominent primary CTA, and a one-liner of help text. Hide the sidebar entirely. The `.study-analysis` grid should only render once a study exists.
- **Impact:** High -- this is every user's first impression of the app.

### D3. Sidebar cards should hide when empty instead of showing placeholder text
- **File:** `frontend/src/components/viewer/ViewTab.tsx:179-318`
- **Problem:** The sidebar always renders cards for Viewer Tools, Line Annotations, Automatic Measurement, Pixel Measurements, Calibrated Measurements, Calibration, and Backend Notes. Before any analysis runs, most of these display "No line annotations yet", "Load a study, then click Measure tooth...", "No calibration metadata was available...". This is 4-5 cards of grey instructional text pushing actual results below the fold once they arrive.
- **Recommended change:** Only render cards that have data. Keep the "Viewer Tools" help card and the "Line Annotations" list (which doubles as the tool output). Hide Automatic Measurement, Pixel/Calibrated Measurements, Calibration, and Backend Notes until their data is populated. When measurement runs, animate the new cards in.
- **Impact:** Medium -- reduces cognitive load and makes results more prominent.

### D4. Processing tab needs side-by-side before/after comparison
- **File:** `frontend/src/components/processing/ProcessingTab.tsx:99-411`
- **Problem:** The Processing tab has a two-column layout: original preview on the left, form on the right. The processed output appears at the bottom of the form column (`processing-tab__output`, line 399-408), below the fold, rendered by a second `DicomViewer`. Comparing before and after requires scrolling up and down. This defeats the purpose of visual comparison.
- **Recommended change:** After processing completes, switch the left column from a single preview to a side-by-side or slider-split comparison. A simple approach: render both images in the left column with a CSS `grid-template-columns: 1fr 1fr` or a draggable split divider. The "Compare" checkbox already exists in the form -- tie it to this split view. An even simpler first step: show the processed preview *replacing* the original in the left column, with a toggle to flip between them.
- **Impact:** High -- before/after comparison is the core workflow of this tab.

### D5. Status text should be a proper status bar
- **File:** `frontend/src/components/viewer/ViewTab.tsx:155`, `frontend/src/styles/base.css:221-226`
- **Problem:** `<p class="view-panel__status">` is a 12px dim text paragraph sitting between the toolbar and the viewer. It carries important information (job progress messages, error states, "Study loaded" confirmation) but is visually insignificant -- easy to miss, especially during multi-step operations. It also wastes a full row of vertical space for a single line of text.
- **Recommended change:** Move the status to a fixed-position bar at the bottom of the `.app-shell` (above or merged with the job center). Use the same `--text-dim` color but at a slightly larger size, with an icon prefix for the current state (checkmark for success, spinner for loading, warning triangle for errors). This is a well-established desktop app pattern (VS Code status bar, Photoshop info bar).
- **Impact:** Medium -- improves discoverability of state changes without adding visual noise.

### D6. Job center should be a collapsible drawer or toast pattern
- **File:** `frontend/src/features/jobs/JobCenter.tsx:37-107`, `frontend/src/styles/base.css:87-183`
- **Problem:** The job center renders as a full-width docked panel at the bottom of the screen whenever any job exists (including completed ones). It has a header ("Jobs / Background work"), a grid of cards, and `backdrop-filter`. For the common case of 1-2 jobs, this is a lot of persistent chrome. Completed jobs linger with no dismiss mechanism (the list is capped at 6 by `.slice(0, 6)` but jobs are never removed). The panel steals viewport height from the viewer.
- **Recommended change:** Two options depending on how prominent jobs should be:
  - **Option A (minimal):** Collapse completed/failed/cancelled jobs after a few seconds, show only active jobs. Add a small expand toggle ("2 jobs") that opens the full panel.
  - **Option B (toast):** Show active jobs as floating toast notifications in the bottom-right corner. Completed jobs auto-dismiss after 3-5 seconds. A small badge in the status bar shows the count of active jobs. Click to open a popover with the full job list.
  - Either way, add a dismiss/clear mechanism for terminal job states.
- **Impact:** Medium -- reclaims viewport space for the viewer, which is the primary content.

### D7. Viewer HUD should show image coordinates on hover
- **File:** `frontend/src/features/viewer/ViewerCanvas.tsx:336-349`
- **Problem:** The viewer HUD (top-right corner) shows only zoom percentage and a "Reset view" button. When the user is drawing measurements or inspecting specific pixel regions, there's no coordinate readout. Medical imaging tools conventionally show the cursor position in image space (and optionally the pixel value) as a persistent HUD element.
- **Recommended change:** Add a coordinate chip next to the zoom chip that displays `x, y` in source image pixel coordinates as the cursor moves over the canvas. Use the existing `screenToImage` transform. Only show when the image is loaded and the cursor is over the canvas. For the measurement tool, also show the live distance from the start point while drawing.
- **Impact:** Low-Medium -- power-user feature, but expected in imaging tools.

### D8. Form controls lack range constraints and visual affordance
- **File:** `frontend/src/components/processing/ProcessingTab.tsx:186-225`
- **Problem:** Brightness and contrast are plain `<input type="number">` fields. There's no indication of valid ranges, no slider for quick adjustment, and no visual preview of the effect. The contrast field allows negative values (the only constraint is `min={0}` on the HTML attribute). Brightness has no min/max at all. Users don't know if brightness 50 is a small or large change.
- **Recommended change:** Add `<input type="range">` sliders alongside the number inputs. Brightness: range -100 to 100, default 0. Contrast: range 0.1 to 3.0, default 1.0. Show the numeric value next to the slider. This is the standard pattern in every image editor. Keep the number input as an override for precise values.
- **Impact:** Medium -- makes the processing form feel interactive rather than data-entry.

---

## Summary Table

| Category               | Count |
|------------------------|-------|
| High Severity          | 6     |
| Medium Severity        | 12    |
| Low Severity           | 14    |
| Design Recommendations | 8     |
| **Total**              | **40** |

---

## Top 5 Quick Wins

These are ordered by impact-to-effort ratio -- each can be done in under 30 minutes.

1. **Add `:focus-visible` outline** (M3) -- Single CSS rule, immediately fixes keyboard navigation visibility for all interactive elements.

2. **Remove `backdrop-filter: blur(10px)` from `.job-center`** (H5) -- One-line CSS change, eliminates the most expensive compositing operation in the app.

3. **Replace `filter: drop-shadow` with stroke-based glow on selected annotation** (H6) -- Small CSS + SVG change, eliminates per-frame SVG filter during pan/zoom.

4. **Fix spinner contrast on primary button** (M12) -- Change two color values in the `.spinner` rule so the loading indicator is actually visible.

5. **Add `aria-controls`/`id` to tab bar and tabpanel** (H1) -- Add 4 HTML attributes, fixes the most basic screen reader navigation.

---

## Top 5 Design Changes

These are larger efforts but have the most visible impact on how the app feels.

1. **Full-viewport empty state** (D2) -- Replace the hollow two-column layout with a single centered CTA. Touches `ViewTab.tsx` and `base.css` only. Half-day effort, completely changes the first impression.

2. **Toolbar grouping + icons** (D1) -- Add icon SVGs and visual separators to the View toolbar. Moderate effort (need to create/source ~5 icons), but the toolbar is the most-used surface.

3. **Side-by-side processing comparison** (D4) -- Even a simple "swap original/processed in the left column" toggle would be a big UX improvement. Full slider-split is more work but high-payoff.

4. **Hide empty sidebar cards** (D3) -- Conditional rendering in `ViewTab.tsx`. Small code change, immediately declutters the sidebar.

5. **Range sliders for brightness/contrast** (D8) -- Add `<input type="range">` next to the existing number inputs. Standard pattern, makes the form feel like an image editor instead of a data entry form.
