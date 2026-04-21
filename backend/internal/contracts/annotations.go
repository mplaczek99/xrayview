package contracts

type AnnotationSource string

const (
	AnnotationSourceManual    AnnotationSource = "manual"
	AnnotationSourceAutoTooth AnnotationSource = "autoTooth"
)

type AnnotationPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type LineMeasurement struct {
	PixelLength        float64  `json:"pixelLength"`
	CalibratedLengthMM *float64 `json:"calibratedLengthMm,omitempty"`
}

type LineAnnotation struct {
	ID          string           `json:"id"`
	Label       string           `json:"label"`
	Source      AnnotationSource `json:"source"`
	Start       AnnotationPoint  `json:"start"`
	End         AnnotationPoint  `json:"end"`
	Editable    bool             `json:"editable"`
	Confidence  *float64         `json:"confidence,omitempty"`
	Measurement *LineMeasurement `json:"measurement,omitempty"`
}

type RectangleAnnotation struct {
	ID         string           `json:"id"`
	Label      string           `json:"label"`
	Source     AnnotationSource `json:"source"`
	X          float64          `json:"x"`
	Y          float64          `json:"y"`
	Width      float64          `json:"width"`
	Height     float64          `json:"height"`
	Editable   bool             `json:"editable"`
	Confidence *float64         `json:"confidence,omitempty"`
}

type PolylineAnnotation struct {
	ID         string            `json:"id"`
	Label      string            `json:"label"`
	Source     AnnotationSource  `json:"source"`
	Points     []AnnotationPoint `json:"points"`
	Closed     bool              `json:"closed"`
	Editable   bool              `json:"editable"`
	Confidence *float64          `json:"confidence,omitempty"`
}

type AnnotationBundle struct {
	Lines      []LineAnnotation      `json:"lines"`
	Rectangles []RectangleAnnotation `json:"rectangles"`
	Polylines  []PolylineAnnotation  `json:"polylines"`
}

type MeasureLineAnnotationCommand struct {
	StudyID    string         `json:"studyId"`
	Annotation LineAnnotation `json:"annotation"`
}

type MeasureLineAnnotationCommandResult struct {
	StudyID    string         `json:"studyId"`
	Annotation LineAnnotation `json:"annotation"`
}
