#!/usr/bin/env bash
# Migration validation: compares Go and Rust binary outputs on the same input DICOM.
# Usage: ./scripts/validate_migration.sh <path-to-test.dcm>
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GO_BIN="$REPO_ROOT/xrayview"
RUST_BIN="$REPO_ROOT/backend-rust/target/release/xrayview-backend-rust"
MAE_THRESHOLD="0.001"

passed=0
failed=0

# --- Prerequisites -----------------------------------------------------------

if [[ $# -lt 1 ]]; then
    echo "Usage: $0 <path-to-test.dcm>"
    exit 1
fi

DCM="$(realpath "$1")"
if [[ ! -f "$DCM" ]]; then
    echo "ERROR: DICOM file not found: $DCM"
    exit 1
fi

if [[ ! -x "$GO_BIN" ]]; then
    echo "ERROR: Go binary not found at $GO_BIN"
    echo "  Build it with: cd $REPO_ROOT && go build -o xrayview ./cmd/xrayview"
    exit 1
fi

if [[ ! -x "$RUST_BIN" ]]; then
    echo "ERROR: Rust binary not found at $RUST_BIN"
    echo "  Build it with: cd $REPO_ROOT/backend-rust && cargo build --release"
    exit 1
fi

for cmd in jq compare; do
    if ! command -v "$cmd" &>/dev/null; then
        echo "ERROR: required command '$cmd' not found in PATH"
        exit 1
    fi
done

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

echo "=== Migration Validation ==="
echo "Input DICOM: $DCM"
echo "Go binary:   $GO_BIN"
echo "Rust binary: $RUST_BIN"
echo ""

# --- Helpers ------------------------------------------------------------------

compare_json() {
    local label="$1" go_json="$2" rust_json="$3"

    # Normalize: sort keys and convert integer floats (1.0 -> 1) for semantic comparison
    local go_norm rust_norm
    go_norm="$(echo "$go_json" | jq -S 'walk(if type == "number" then . * 1000000 | round / 1000000 else . end)' 2>/dev/null)" || {
        echo "  FAIL [$label]: Go JSON is invalid"
        failed=$((failed + 1))
        return
    }
    rust_norm="$(echo "$rust_json" | jq -S 'walk(if type == "number" then . * 1000000 | round / 1000000 else . end)' 2>/dev/null)" || {
        echo "  FAIL [$label]: Rust JSON is invalid"
        failed=$((failed + 1))
        return
    }

    if [[ "$go_norm" == "$rust_norm" ]]; then
        echo "  PASS [$label]"
        passed=$((passed + 1))
    else
        echo "  FAIL [$label]: JSON outputs differ"
        echo "    Go:   $go_norm"
        echo "    Rust: $rust_norm"
        failed=$((failed + 1))
    fi
}

compare_png() {
    local label="$1" go_png="$2" rust_png="$3"

    if [[ ! -f "$go_png" ]]; then
        echo "  FAIL [$label]: Go PNG not produced: $go_png"
        failed=$((failed + 1))
        return
    fi
    if [[ ! -f "$rust_png" ]]; then
        echo "  FAIL [$label]: Rust PNG not produced: $rust_png"
        failed=$((failed + 1))
        return
    fi

    local mae_output
    mae_output="$(compare -metric MAE "$go_png" "$rust_png" /dev/null 2>&1)" || true
    # compare outputs "value (normalized)" on stderr; extract the normalized value in parens
    local mae
    mae="$(echo "$mae_output" | grep -oP '\(([^)]+)\)' | tr -d '()' | head -1)"
    if [[ -z "$mae" ]]; then
        # Fallback: some versions output just the raw value
        mae="$(echo "$mae_output" | awk '{print $1}')"
    fi

    local pass
    pass="$(echo "$mae $MAE_THRESHOLD" | awk '{print ($1 <= $2) ? "yes" : "no"}')"

    if [[ "$pass" == "yes" ]]; then
        echo "  PASS [$label] (MAE=$mae)"
        passed=$((passed + 1))
    else
        echo "  FAIL [$label] (MAE=$mae > threshold $MAE_THRESHOLD)"
        failed=$((failed + 1))
    fi
}

# --- Test cases ---------------------------------------------------------------

echo "--- JSON output tests ---"

# Test 1: --describe-presets
go_presets="$("$GO_BIN" -describe-presets 2>/dev/null)"
rust_presets="$("$RUST_BIN" --describe-presets 2>/dev/null)"
compare_json "describe-presets" "$go_presets" "$rust_presets"

# Test 2: --describe-study
go_study="$("$GO_BIN" -describe-study -input "$DCM" 2>/dev/null)"
rust_study="$("$RUST_BIN" --describe-study --input "$DCM" 2>/dev/null)"
compare_json "describe-study" "$go_study" "$rust_study"

echo ""
echo "--- PNG output tests ---"

# Test 3: plain preview
"$GO_BIN" -input "$DCM" -preview-output "$TMPDIR/go_plain.png" >/dev/null 2>&1
"$RUST_BIN" --input "$DCM" --preview-output "$TMPDIR/rust_plain.png" >/dev/null 2>&1
compare_png "plain preview" "$TMPDIR/go_plain.png" "$TMPDIR/rust_plain.png"

# Test 4: invert
"$GO_BIN" -input "$DCM" -preview-output "$TMPDIR/go_invert.png" -invert >/dev/null 2>&1
"$RUST_BIN" --input "$DCM" --preview-output "$TMPDIR/rust_invert.png" --invert >/dev/null 2>&1
compare_png "invert" "$TMPDIR/go_invert.png" "$TMPDIR/rust_invert.png"

# Test 5: equalize
"$GO_BIN" -input "$DCM" -preview-output "$TMPDIR/go_eq.png" -equalize >/dev/null 2>&1
"$RUST_BIN" --input "$DCM" --preview-output "$TMPDIR/rust_eq.png" --equalize >/dev/null 2>&1
compare_png "equalize" "$TMPDIR/go_eq.png" "$TMPDIR/rust_eq.png"

# Test 6: brightness + contrast + palette
"$GO_BIN" -input "$DCM" -preview-output "$TMPDIR/go_combo.png" -brightness 30 -contrast 1.4 -palette bone >/dev/null 2>&1
"$RUST_BIN" --input "$DCM" --preview-output "$TMPDIR/rust_combo.png" --brightness 30 --contrast 1.4 --palette bone >/dev/null 2>&1
compare_png "brightness+contrast+bone" "$TMPDIR/go_combo.png" "$TMPDIR/rust_combo.png"

# --- Summary ------------------------------------------------------------------

echo ""
echo "=== Summary ==="
total=$((passed + failed))
echo "$passed/$total passed, $failed failed"

if [[ $failed -gt 0 ]]; then
    exit 1
fi
echo "All tests passed."
exit 0
