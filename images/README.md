# Sample Assets

This directory contains the public sample DICOM used by the repository.

- `sample-dental-radiograph.dcm`
  - Type: derived DICOM sample for local testing, demos, and README examples
  - Image content source: Wikimedia Commons `Dental Panorama X-ray.jpg`
  - Source page: `https://commons.wikimedia.org/wiki/File:Dental_Panorama_X-ray.jpg`
  - Original file URL: `https://upload.wikimedia.org/wikipedia/commons/a/ac/Dental_Panorama_X-ray.jpg`
  - Author: `Farhang Amini` (`Frankisnotfunny`)
  - Source image license: `CC BY 4.0`

Notes:

- The upstream source image is a panoramic jaw x-ray with visible teeth, not a DICOM file.
- `xrayview` wraps the public Wikimedia Commons JPEG into a derived DICOM so the repository keeps a DICOM-first sample workflow.
- The sample image shows a routine panoramic dental study and is a better fit for jaw-and-teeth demos than the previous extracted-tooth sample.
