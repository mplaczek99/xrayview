package contracts

type AnalyzeStudyCommand struct {
	StudyID string `json:"studyId"`
}

type ToothAnalysis struct {
	Image       ToothImageMetadata `json:"image"`
	Calibration ToothCalibration   `json:"calibration"`
	Tooth       *ToothCandidate    `json:"tooth,omitempty"`
	Teeth       []ToothCandidate   `json:"teeth"`
	Warnings    []string           `json:"warnings"`
}

type ToothImageMetadata struct {
	Width  uint32 `json:"width"`
	Height uint32 `json:"height"`
}

type ToothCalibration struct {
	PixelUnits                     string            `json:"pixelUnits"`
	MeasurementScale               *MeasurementScale `json:"measurementScale,omitempty"`
	RealWorldMeasurementsAvailable bool              `json:"realWorldMeasurementsAvailable"`
}

type ToothCandidate struct {
	Confidence     float64                `json:"confidence"`
	MaskAreaPixels uint32                 `json:"maskAreaPixels"`
	Measurements   ToothMeasurementBundle `json:"measurements"`
	Geometry       ToothGeometry          `json:"geometry"`
}

type ToothMeasurementBundle struct {
	Pixel      ToothMeasurementValues  `json:"pixel"`
	Calibrated *ToothMeasurementValues `json:"calibrated,omitempty"`
}

type ToothMeasurementValues struct {
	ToothWidth        float64 `json:"toothWidth"`
	ToothHeight       float64 `json:"toothHeight"`
	BoundingBoxWidth  float64 `json:"boundingBoxWidth"`
	BoundingBoxHeight float64 `json:"boundingBoxHeight"`
	Units             string  `json:"units"`
}

type ToothGeometry struct {
	BoundingBox BoundingBox `json:"boundingBox"`
	WidthLine   LineSegment `json:"widthLine"`
	HeightLine  LineSegment `json:"heightLine"`
	Outline     []Point     `json:"outline"`
}

type BoundingBox struct {
	X      uint32 `json:"x"`
	Y      uint32 `json:"y"`
	Width  uint32 `json:"width"`
	Height uint32 `json:"height"`
}

type LineSegment struct {
	Start Point `json:"start"`
	End   Point `json:"end"`
}

type Point struct {
	X uint32 `json:"x"`
	Y uint32 `json:"y"`
}

type AnalyzeStudyCommandResult struct {
	StudyID              string           `json:"studyId"`
	PreviewPath          string           `json:"previewPath"`
	Analysis             ToothAnalysis    `json:"analysis"`
	SuggestedAnnotations AnnotationBundle `json:"suggestedAnnotations"`
}
