# Sample Assets

This directory contains the public sample DICOM used by the repository.

- `sample-dental-radiograph.dcm`
  - Type: derived DICOM sample for local testing, demos, and README examples
  - Image content source: ACTA-DIRECT dental radiograph `001.tif`
  - Source dataset DOI: `https://doi.org/10.48338/VU01-WK8SQN`
  - Source file URL: `https://data.yoda.vu.nl:9443/vault-acta-ozi-2023/RICARDO_Dataset%5B1727095741%5D/original/3_Radiographs/001.tif`
  - Source dataset license: `CC BY 4.0`

Notes:

- The upstream ACTA-DIRECT dataset publishes these dental radiographs as TIFF, not DICOM.
- `xrayview` wraps the public `001.tif` radiograph into a derived DICOM so the repository keeps a DICOM-first sample workflow.
- ACTA-DIRECT contains radiographs of extracted teeth with accompanying annotations; this sample is dental, but it is not a routine in-vivo patient study.
