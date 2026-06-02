// Dumps the tooth/bone overlay output to BMP files for visual inspection.
// Usage: cargo run --release --example overlay_dump -- <input.bmp> [out_dir]

use std::{env, path::PathBuf};

use xrayview_backend_rs::render::save_preview_bmp;

fn main() {
    let mut args = env::args().skip(1);
    let input = args.next().expect("usage: overlay_dump <input.bmp> [out_dir]");
    let out_dir = args.next().map(PathBuf::from).unwrap_or_else(|| PathBuf::from("."));

    let preview = xrayview_backend_rs::bmp::render_grayscale_preview_file_for_tooth_analysis(&input)
        .expect("render analysis preview");
    let preview = xrayview_backend_rs::render::PreviewImage::gray(
        preview.width,
        preview.height,
        Vec::from(preview.pixels.as_ref()),
    );

    let result =
        xrayview_backend_rs::analysis::generate_tooth_overlay(&preview).expect("generate overlay");

    let stem = PathBuf::from(&input)
        .file_stem()
        .map(|s| s.to_string_lossy().into_owned())
        .unwrap_or_else(|| "image".to_string());
    let outline_path = out_dir.join(format!("{stem}.outline.bmp"));
    let filled_path = out_dir.join(format!("{stem}.filled.bmp"));

    save_preview_bmp(&outline_path, &result.preview).expect("write outline bmp");
    save_preview_bmp(&filled_path, &result.filled_preview).expect("write filled bmp");

    println!(
        "wrote {} and {} ({}x{}) tooth_pixels={} bone_pixels={} mode={:?}",
        outline_path.display(),
        filled_path.display(),
        preview.width,
        preview.height,
        result.tooth_pixels,
        result.bone_pixels,
        result.mode,
    );
}
