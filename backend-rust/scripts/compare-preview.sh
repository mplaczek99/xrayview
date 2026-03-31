#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
input_path=${1:-"$repo_root/images/sample-dental-radiograph.dcm"}
output_dir=${2:-"$(mktemp -d)"}
shift $(( $# > 0 ? 1 : 0 )) || true
shift $(( $# > 0 ? 1 : 0 )) || true
extra_args=("$@")

mkdir -p "$output_dir"

go_output="$output_dir/go-preview.png"
rust_output="$output_dir/rust-preview.png"

go run "$repo_root/cmd/xrayview" -input "$input_path" -preview-output "$go_output" "${extra_args[@]}"
cargo run --manifest-path "$repo_root/backend-rust/Cargo.toml" -- -input "$input_path" -preview-output "$rust_output" "${extra_args[@]}"

printf 'Go preview:   %s\n' "$go_output"
printf 'Rust preview: %s\n' "$rust_output"
