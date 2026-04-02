use std::path::Path;

use anyhow::Result;

use crate::preview::PreviewImage;
use crate::save::save_dicom;
use crate::study::source_image::SourceMetadata;

pub fn export_secondary_capture(
    preview: &PreviewImage,
    source_meta: &SourceMetadata,
    output_path: &Path,
) -> Result<()> {
    let image = preview.clone().into_dynamic_image();
    save_dicom(&image, source_meta, output_path)
}
