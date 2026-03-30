# xrayview

`xrayview` is a DICOM-first X-ray visualization project with a Tauri desktop frontend and a Go processing backend.

The primary desktop UI lives in `frontend-app/`. The Go CLI in `cmd/xrayview` is the backend processing entry point used by that desktop frontend, and it also remains usable directly from the command line for DICOM workflows.

## What It Does

- Loads a DICOM study from disk (`.dcm`, `.dicom`)
- Renders the study into a grayscale working image
- Optionally applies invert, brightness, contrast, and histogram equalization
- Optionally applies a pseudocolor palette
- Optionally builds a side-by-side comparison image
- Saves the result as a derived DICOM image

The desktop frontend renders internal PNG previews so the workstation UI can display the study, but the user-facing workflow is DICOM in and DICOM out.

## Important Notice

This tool is for image visualization only.

It is **not** a medical device and must **not** be used for medical diagnosis, clinical decisions, or treatment planning.

## Build

```bash
go build -o /tmp/xrayview ./cmd/xrayview
```

## Desktop Frontend

The primary desktop UI is the Tauri frontend:

```bash
npm --prefix frontend-app install
npm --prefix frontend-app run tauri:dev
```

To build desktop bundles with the Go backend embedded as a sidecar:

```bash
npm --prefix frontend-app run tauri:build
```

On Linux, bundle builds also require the usual Tauri system packages such as WebKitGTK and `patchelf`.

To iterate on the UI in browser-only mock mode:

```bash
npm --prefix frontend-app run dev
```

## Releases

Prebuilt desktop packages are published on GitHub Releases.

- Linux: download the `.AppImage`, run `chmod +x <asset>.AppImage`, then run it
- Windows: download the `.msi` installer and run it

## Basic Usage

The repository includes a public dental radiograph sample at `images/sample-dental-radiograph.dcm`. This DICOM file is derived from the ACTA-DIRECT dataset radiograph `001.tif` (CC BY 4.0). See `images/README.md` for provenance details.

```bash
go run ./cmd/xrayview -input images/sample-dental-radiograph.dcm
```

If `-output` is omitted, the tool writes a file next to the input using this pattern:

- `study.dcm` -> `study_processed.dcm`

## Flags

### Input

- `-input`
  - Path to the source DICOM study
  - Supports `.dcm` and `.dicom`

### Output

- `-output`
  - Output DICOM path
  - Optional
  - Must end with `.dcm` or `.dicom`
  - If omitted, `xrayview` generates `input_processed.dcm` in the same directory as the input

### Presets

- `-preset`
  - Named visualization preset
  - Supported values: `default`, `xray`, `high-contrast`
  - Presets set a combination of brightness, contrast, equalization, and palette
  - Explicit CLI flags override preset values

#### Preset summary

- `default`
  - brightness `0`
  - contrast `1.0`
  - equalize `false`
  - palette `none`

- `xray`
  - brightness `10`
  - contrast `1.4`
  - equalize `true`
  - palette `bone`

- `high-contrast`
  - brightness `0`
  - contrast `1.8`
  - equalize `true`
  - palette `none`

### Grayscale Filter Controls

- `-invert`
  - Inverts the grayscale image

- `-brightness`
  - Integer brightness delta
  - Positive values brighten the image
  - Negative values darken the image

- `-contrast`
  - Floating-point contrast factor
  - `1.0` leaves contrast unchanged
  - Values above `1.0` increase contrast
  - Values below `1.0` decrease contrast

- `-equalize`
  - Enables histogram equalization

### Comparison Output

- `-compare`
  - Writes a side-by-side comparison image into the derived DICOM output
  - Left side: original grayscale image
  - Right side: processed output image

### Pipeline Ordering

- `-pipeline`
  - Comma-separated list of grayscale processing steps
  - Lets you control the order of enabled grayscale filters
  - Enabled steps omitted from the list still run afterward in the default order
  - Supported step names:
    - `grayscale`
    - `invert`
    - `brightness`
    - `contrast`
    - `equalize`
  - Duplicate step names are rejected

If `-pipeline` is omitted, the default order is:

```text
grayscale,invert,brightness,contrast,equalize
```

Notes:

- `grayscale` is always the starting point
- The pipeline only affects grayscale filter ordering
- Pseudocolor is applied after the grayscale pipeline
- Comparison output is applied after all processing

### Pseudocolor

- `-palette`
  - Pseudocolor palette for the final image
  - Supported values:
    - `none`
    - `hot`
    - `bone`

## Examples

### Basic processing

```bash
go run ./cmd/xrayview -input images/sample-dental-radiograph.dcm
```

### Explicit output file

```bash
go run ./cmd/xrayview -input images/sample-dental-radiograph.dcm -output images/sample-dental-radiograph_processed.dcm
```

### Tone adjustments

```bash
go run ./cmd/xrayview -input images/sample-dental-radiograph.dcm -invert -brightness 15
```

```bash
go run ./cmd/xrayview -input images/sample-dental-radiograph.dcm -contrast 1.6 -equalize
```

### Presets

```bash
go run ./cmd/xrayview -input images/sample-dental-radiograph.dcm -preset xray
```

```bash
go run ./cmd/xrayview -input images/sample-dental-radiograph.dcm -preset xray -brightness 5
```

### Pipeline ordering

```bash
go run ./cmd/xrayview -input images/sample-dental-radiograph.dcm -invert -contrast 1.5 -equalize -pipeline grayscale,contrast,invert,equalize
```

### Pseudocolor

```bash
go run ./cmd/xrayview -input images/sample-dental-radiograph.dcm -palette hot
```

### Comparison output

```bash
go run ./cmd/xrayview -input images/sample-dental-radiograph.dcm -compare
```

```bash
go run ./cmd/xrayview -input images/sample-dental-radiograph.dcm -preset xray -compare
```

## Validation Rules

- `-input` is required
- Input must be a `.dcm` or `.dicom` file
- Output must be a `.dcm` or `.dicom` file
- `-palette` must be `none`, `hot`, or `bone`
- `-preset` must be `default`, `xray`, or `high-contrast`
- `-pipeline` may only contain `grayscale`, `invert`, `brightness`, `contrast`, and `equalize`

## Test

```bash
go test ./...
```
