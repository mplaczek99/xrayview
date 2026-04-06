export const MOCK_STUDY_DIRECTORY = "mock-data";
export const MOCK_EXPORT_DIRECTORY = "mock-exports";
export const MOCK_DICOM_PATH = `${MOCK_STUDY_DIRECTORY}/mock-dental-study.dcm`;
export const MOCK_PROCESSED_DICOM_PATH =
  `${MOCK_STUDY_DIRECTORY}/mock-dental-study_processed.dcm`;

export function buildMockPath(directory: string, fileName: string): string {
  return `${directory}/${fileName}`;
}
