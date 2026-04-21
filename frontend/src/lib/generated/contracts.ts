// This file is generated from `contracts/backend-contract-v1.schema.json`.
// Run `npm run contracts:generate` after changing the schema.
// Backend contract version: v1

export const BACKEND_CONTRACT_VERSION = 1 as const;

export type PaletteName = "none" | "hot" | "bone";

export interface ProcessingControls {
  brightness: number;
  contrast: number;
  invert: boolean;
  equalize: boolean;
  palette: PaletteName;
}

export interface ProcessingPreset {
  id: string;
  controls: ProcessingControls;
}

export interface ProcessingManifest {
  defaultPresetId: string;
  presets: ProcessingPreset[];
}

export interface MeasurementScale {
  rowSpacingMm: number;
  columnSpacingMm: number;
  source: string;
}

export type AnnotationSource = "manual" | "autoTooth";

export interface AnnotationPoint {
  x: number;
  y: number;
}

export interface LineMeasurement {
  pixelLength: number;
  calibratedLengthMm?: number | null;
}

export interface LineAnnotation {
  id: string;
  label: string;
  source: AnnotationSource;
  start: AnnotationPoint;
  end: AnnotationPoint;
  editable: boolean;
  confidence?: number | null;
  measurement?: LineMeasurement | null;
}

export interface RectangleAnnotation {
  id: string;
  label: string;
  source: AnnotationSource;
  x: number;
  y: number;
  width: number;
  height: number;
  editable: boolean;
  confidence?: number | null;
}

export interface PolylineAnnotation {
  id: string;
  label: string;
  source: AnnotationSource;
  points: AnnotationPoint[];
  closed: boolean;
  editable: boolean;
  confidence?: number | null;
}

export interface AnnotationBundle {
  lines: LineAnnotation[];
  rectangles: RectangleAnnotation[];
  polylines: PolylineAnnotation[];
}

export type BackendErrorCode = "invalidInput" | "notFound" | "cancelled" | "conflict" | "cacheCorrupted" | "internal";

export interface BackendError {
  code: BackendErrorCode;
  message: string;
  details?: string[];
  recoverable: boolean;
}

export type JobKind = "renderStudy" | "processStudy" | "analyzeStudy";

export type JobState = "queued" | "running" | "cancelling" | "completed" | "failed" | "cancelled";

export interface JobProgress {
  percent: number;
  stage: string;
  message: string;
}

export interface StartedJob {
  jobId: string;
}

export interface JobCommand {
  jobId: string;
}

export interface OpenStudyCommand {
  inputPath: string;
}

export interface StudyRecord {
  studyId: string;
  inputPath: string;
  inputName: string;
  measurementScale?: MeasurementScale | null;
}

export interface OpenStudyCommandResult {
  study: StudyRecord;
}

export interface RenderStudyCommand {
  studyId: string;
}

export interface RenderStudyCommandResult {
  studyId: string;
  previewPath: string;
  loadedWidth: number;
  loadedHeight: number;
  measurementScale?: MeasurementScale | null;
}

export interface ProcessStudyCommand {
  studyId: string;
  outputPath?: string | null;
  presetId: string;
  invert: boolean;
  brightness?: number | null;
  contrast?: number | null;
  equalize: boolean;
  compare: boolean;
  palette?: PaletteName | null;
}

export interface ProcessStudyCommandResult {
  studyId: string;
  previewPath: string;
  dicomPath: string;
  loadedWidth: number;
  loadedHeight: number;
  mode: string;
  measurementScale?: MeasurementScale | null;
}

export interface AnalyzeStudyCommand {
  studyId: string;
}

export interface MeasureLineAnnotationCommand {
  studyId: string;
  annotation: LineAnnotation;
}

export interface ToothAnalysis {
  image: ToothImageMetadata;
  calibration: ToothCalibration;
  tooth?: ToothCandidate | null;
  teeth: ToothCandidate[];
  warnings: string[];
}

export interface ToothImageMetadata {
  width: number;
  height: number;
}

export interface ToothCalibration {
  pixelUnits: string;
  measurementScale?: MeasurementScale | null;
  realWorldMeasurementsAvailable: boolean;
}

export interface ToothCandidate {
  confidence: number;
  maskAreaPixels: number;
  measurements: ToothMeasurementBundle;
  geometry: ToothGeometry;
}

export interface ToothMeasurementBundle {
  pixel: ToothMeasurementValues;
  calibrated?: ToothMeasurementValues | null;
}

export interface ToothMeasurementValues {
  toothWidth: number;
  toothHeight: number;
  boundingBoxWidth: number;
  boundingBoxHeight: number;
  units: string;
}

export interface ToothGeometry {
  boundingBox: BoundingBox;
  widthLine: LineSegment;
  heightLine: LineSegment;
  outline: Point[];
}

export interface BoundingBox {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface LineSegment {
  start: Point;
  end: Point;
}

export interface Point {
  x: number;
  y: number;
}

export interface AnalyzeStudyCommandResult {
  studyId: string;
  previewPath: string;
  analysis: ToothAnalysis;
  suggestedAnnotations: AnnotationBundle;
}

export interface MeasureLineAnnotationCommandResult {
  studyId: string;
  annotation: LineAnnotation;
}

export type JobResult =
  | { kind: "renderStudy"; payload: RenderStudyCommandResult; }
  | { kind: "processStudy"; payload: ProcessStudyCommandResult; }
  | { kind: "analyzeStudy"; payload: AnalyzeStudyCommandResult; };

export interface JobSnapshot {
  jobId: string;
  jobKind: JobKind;
  studyId?: string | null;
  state: JobState;
  progress: JobProgress;
  fromCache: boolean;
  result?: JobResult | null;
  error?: BackendError | null;
}
