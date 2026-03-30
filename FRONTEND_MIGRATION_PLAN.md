# Frontend Migration Plan

## Recommendation

Move the desktop UI to `Tauri + React + Vite + TypeScript` and keep the Go backend exactly as it is.

Why this stack:

- Better layout and interaction tooling for a high-end workstation UI
- Faster iteration on theming, animation, panels, cards, and reports
- Native desktop packaging without Electron-sized overhead
- Clean boundary with the existing CLI backend

## Backend Boundary

Do not change Go backend code.

The new frontend should call the existing backend in the same way the current desktop app does:

- Render source preview by invoking the backend with `-input` and `-preview-output`
- Render processed output by invoking the backend with `-input`, `-output`, `-preview-output`, and the current control flags
- Save the rendered DICOM by copying the generated temporary file to the user-selected destination

That keeps all image processing and DICOM handling in Go.

## Proposed Repo Shape

```text
xrayview/
  cmd/
  internal/
  java-frontend/
  frontend-app/
    package.json
    vite.config.ts
    tsconfig.json
    index.html
    src/
      main.tsx
      app/
        App.tsx
        routes.tsx
        providers.tsx
      components/
        shell/
        viewer/
        controls/
        report/
        common/
      features/
        study/
        processing/
        export/
        compare/
      lib/
        backend.ts
        files.ts
        formats.ts
        theme.ts
      styles/
        tokens.css
        base.css
        utilities.css
    src-tauri/
      tauri.conf.json
      capabilities/
      icons/
      src/
        main.rs
        backend.rs
        dialogs.rs
        filesystem.rs
```

## Frontend Responsibilities

### `src/components/shell`

- Application frame
- Top toolbar
- Left navigation or study rail
- Right-side control and status panels

### `src/components/viewer`

- Main image canvas
- Original/processed/compare modes
- Thumbnail strip
- Overlay chips and status markers

### `src/components/controls`

- Brightness and contrast controls
- Palette selector
- Quick recipe buttons
- Render and save actions

### `src/components/report`

- Future AI/report layout
- Tooth or finding cards
- Printable summary view

### `src/features/study`

- Selected file state
- Source preview loading
- File metadata and session state

### `src/features/processing`

- UI control state
- Dirty state tracking
- Render lifecycle and failure handling

### `src/features/export`

- Save destination flow
- Temporary output tracking
- Export success and stale-output protection

### `src/lib/backend.ts`

- Single adapter for invoking the Go CLI
- Builds argument lists from UI state
- Normalizes success, stderr, and failure cases

## Desktop Bridge

Use Tauri for the small native layer only.

Recommended commands:

- `open_dicom_dialog`
- `save_dicom_dialog`
- `run_backend_preview`
- `run_backend_process`
- `copy_processed_output`

The React app should never spawn processes directly. Keep all desktop and filesystem calls inside Tauri commands.

## UI Direction

Keep the new UI opinionated and workstation-like:

- Large center canvas
- Dense but readable side panels
- Strong visual hierarchy
- Warm-neutral dark palette with teal/cyan accents
- Design tokens for spacing, radius, color, and elevation
- Motion limited to panel entry, mode switch fades, and status transitions

## Migration Phases

### Phase 1

- Scaffold `frontend-app/`
- Build app shell, theme tokens, and static workstation layout
- Recreate open/render/save flow with mocked data first

### Phase 2

- Wire Tauri commands to the existing Go CLI
- Restore preview rendering and processed output generation
- Match current JavaFX behavior for stale-output protection

### Phase 3

- Improve compare mode and thumbnail rail
- Add richer status states and failure messaging
- Add report view structure inspired by the supplied references

### Phase 4

- Package backend sidecar with Tauri builds
- Validate Linux and Windows release flow
- Retire `java-frontend/` only after parity is proven

## Definition Of Done For Migration

- Open DICOM, render preview, and save derived DICOM work end to end
- No Go backend changes are required
- Desktop packaging includes the backend binary safely
- New UI clearly exceeds the JavaFX version in layout, polish, and maintainability
