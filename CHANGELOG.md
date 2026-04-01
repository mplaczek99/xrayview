# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.2.0] - 2026-04-01

### Added

- Rust backend replacing the Go backend for DICOM processing, image preview, comparison, and export
- Tauri desktop application replacing the Java frontend
- Two-tab View/Processing UI layout with React and Vite
- DICOM-first workflow with physical measurement display in viewer
- Processing presets driven by a backend manifest
- Sample dental panoramic DICOM study
- Linux release names now include CPU architecture

### Changed

- Renamed `backend-rust` directory to `backend`
- Refactored duplicated preview and DICOM export flows into shared paths
- Optimized uint16 DICOM render path and processing hot paths
- Updated frontend Vite toolchain for audit fixes
- Renamed Tauri frontend workspace to `frontend`
- Improved CI caching for Rust build artifacts
- Updated build/release workflow for new Tauri-based architecture

### Fixed

- DICOM study open flow reliability
- Stale renders and DICOM metadata parsing
- Tauri preview loading and root npm workflows
- Tauri window identity on Linux
- Linux Tauri packaging after frontend rename
- Tauri dev target cleanup after workspace rename
- Small-screen desktop window sizing
- UI handling of double-slash paths

### Removed

- Go backend (`cmd/xrayview`, `internal/`, `go.mod`)
- Java frontend (`java-frontend/`)
- Legacy sample image assets (bone.png, equalized.png, hot.png, teeth-test.jpg)
