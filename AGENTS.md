# Repository Guidelines

## Project Structure & Module Organization
`backend/` contains the Rust CLI and imaging pipeline. Core processing lives in `backend/src/` (`processing.rs`, `preview.rs`, `save.rs`, etc.). `frontend/` contains the Vite + React UI in `frontend/src/`, plus the Tauri desktop shell in `frontend/src-tauri/`. Sample assets for demos and manual validation live in `images/`. Use the root `package.json` as the entry point for frontend and Tauri scripts.

## Build, Test, and Development Commands
Run `npm install` from the repo root to install the frontend workspace; the root `postinstall` handles `frontend/`.

- `npm run dev`: starts the browser-only Vite UI on port `1420`.
- `npm run tauri:dev`: launches the desktop app with the Tauri shell.
- `npm run tauri:build`: builds desktop bundles and prepares the Rust backend sidecar.
- `npm --prefix frontend run build`: type-checks TypeScript and builds the web assets.
- `cargo build --release --manifest-path backend/Cargo.toml`: builds the Rust backend CLI.
- `cargo test --manifest-path backend/Cargo.toml`: runs backend unit tests.

## Coding Style & Naming Conventions
Match the existing file-local style. TypeScript/TSX uses 2-space indentation, PascalCase for components (`ProcessingTab.tsx`), camelCase for helpers, and BEM-like CSS class names such as `tab-bar__tab--active`. Rust follows standard `rustfmt` conventions: 4-space indentation, `snake_case` for modules/functions, and `CamelCase` for types. Keep comments short and explanatory; prefer small, focused functions over large multi-purpose blocks.

## Testing Guidelines
Backend tests live next to the implementation in `backend/src/*` under `#[test]` blocks. Add new Rust tests beside the code they cover. There is no dedicated frontend test suite yet, so UI changes should be validated with `npm --prefix frontend run build`, `npm run dev`, and `npm run tauri:dev`. Use `images/sample-dental-radiograph.dcm` for repeatable manual checks.

## Commit & Pull Request Guidelines
Recent commits favor short imperative subjects, often with conventional prefixes like `fix:`, `perf:`, or `chore:`. Keep each commit scoped to one behavior or layer. PRs should explain the user-visible change, list the commands used for validation, link related issues, and include screenshots or short recordings for viewer or processing UI changes.

## Security & Data Handling
Treat DICOM inputs as sensitive. Do not commit patient data; use the provided sample asset or sanitized files only. Keep the README warning intact: this project is for visualization, not diagnosis.
