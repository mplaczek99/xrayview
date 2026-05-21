// Code generated from contracts/backend-contract-v1.schema.json. DO NOT EDIT.

package contractv1

const BackendContractVersion = 1

const BackendContractSchemaID = "https://xrayview.local/contracts/backend-contract-v1.schema.json"

// BackendContractSchemaJSON is the authoritative contract schema for future Go validation.
const BackendContractSchemaJSON = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://xrayview.local/contracts/backend-contract-v1.schema.json",
  "title": "XRayView Backend Contract v1",
  "description": "Language-neutral schema for the xrayview desktop/backend contract.",
  "x-contract-version": 1,
  "x-go-package": "contractv1",
  "x-generated-targets": {
    "typescript": "frontend/src/lib/generated/contracts.ts",
    "goValidationBindings": "contracts/contractv1/bindings.go"
  },
  "x-export-order": [
    "PaletteName",
    "ProcessingControls",
    "ProcessingPreset",
    "ProcessingManifest",
    "MeasurementScale",
    "AnnotationSource",
    "AnnotationPoint",
    "LineMeasurement",
    "LineAnnotation",
    "RectangleAnnotation",
    "PolylineAnnotation",
    "AnnotationBundle",
    "BackendErrorCode",
    "BackendError",
    "JobKind",
    "JobState",
    "JobProgress",
    "StartedJob",
    "JobCommand",
    "GetJobsCommand",
    "OpenStudyCommand",
    "StudyRecord",
    "OpenStudyCommandResult",
    "RenderStudyCommand",
    "RenderStudyCommandResult",
    "AnalyzeStudyCommand",
    "AnalyzeStudyCommandResult",
    "ProcessStudyCommand",
    "ProcessStudyCommandResult",
    "MeasureLineAnnotationCommand",
    "MeasureLineAnnotationCommandResult",
    "JobResult",
    "JobSnapshot"
  ],
  "$defs": {
    "PaletteName": {
      "enum": ["none", "hot", "bone"]
    },
    "ProcessingControls": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "brightness": { "type": "number" },
        "contrast": { "type": "number" },
        "invert": { "type": "boolean" },
        "equalize": { "type": "boolean" },
        "palette": { "$ref": "#/$defs/PaletteName" }
      },
      "required": ["brightness", "contrast", "invert", "equalize", "palette"]
    },
    "ProcessingPreset": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "id": { "type": "string" },
        "controls": { "$ref": "#/$defs/ProcessingControls" }
      },
      "required": ["id", "controls"]
    },
    "ProcessingManifest": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "defaultPresetId": { "type": "string" },
        "presets": {
          "type": "array",
          "items": { "$ref": "#/$defs/ProcessingPreset" }
        }
      },
      "required": ["defaultPresetId", "presets"]
    },
    "MeasurementScale": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "rowSpacingMm": { "type": "number" },
        "columnSpacingMm": { "type": "number" },
        "source": { "type": "string" }
      },
      "required": ["rowSpacingMm", "columnSpacingMm", "source"]
    },
    "AnnotationSource": {
      "enum": ["manual"]
    },
    "AnnotationPoint": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "x": { "type": "number" },
        "y": { "type": "number" }
      },
      "required": ["x", "y"]
    },
    "LineMeasurement": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "pixelLength": { "type": "number" },
        "calibratedLengthMm": {
          "type": ["number", "null"]
        }
      },
      "required": ["pixelLength"]
    },
    "LineAnnotation": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "id": { "type": "string" },
        "label": { "type": "string" },
        "source": { "$ref": "#/$defs/AnnotationSource" },
        "start": { "$ref": "#/$defs/AnnotationPoint" },
        "end": { "$ref": "#/$defs/AnnotationPoint" },
        "editable": { "type": "boolean" },
        "confidence": {
          "type": ["number", "null"]
        },
        "measurement": {
          "anyOf": [
            { "$ref": "#/$defs/LineMeasurement" },
            { "type": "null" }
          ]
        }
      },
      "required": ["id", "label", "source", "start", "end", "editable"]
    },
    "RectangleAnnotation": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "id": { "type": "string" },
        "label": { "type": "string" },
        "source": { "$ref": "#/$defs/AnnotationSource" },
        "x": { "type": "number" },
        "y": { "type": "number" },
        "width": { "type": "number" },
        "height": { "type": "number" },
        "editable": { "type": "boolean" },
        "confidence": {
          "type": ["number", "null"]
        }
      },
      "required": ["id", "label", "source", "x", "y", "width", "height", "editable"]
    },
    "PolylineAnnotation": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "id": { "type": "string" },
        "label": { "type": "string" },
        "source": { "$ref": "#/$defs/AnnotationSource" },
        "points": {
          "type": "array",
          "items": { "$ref": "#/$defs/AnnotationPoint" }
        },
        "closed": { "type": "boolean" },
        "editable": { "type": "boolean" },
        "confidence": {
          "type": ["number", "null"]
        }
      },
      "required": ["id", "label", "source", "points", "closed", "editable"]
    },
    "AnnotationBundle": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "lines": {
          "type": "array",
          "items": { "$ref": "#/$defs/LineAnnotation" }
        },
        "rectangles": {
          "type": "array",
          "items": { "$ref": "#/$defs/RectangleAnnotation" }
        },
        "polylines": {
          "type": "array",
          "items": { "$ref": "#/$defs/PolylineAnnotation" }
        }
      },
      "required": ["lines", "rectangles", "polylines"]
    },
    "BackendErrorCode": {
      "enum": [
        "invalidInput",
        "notFound",
        "cancelled",
        "conflict",
        "cacheCorrupted",
        "internal"
      ]
    },
    "BackendError": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "code": { "$ref": "#/$defs/BackendErrorCode" },
        "message": { "type": "string" },
        "details": {
          "type": "array",
          "items": { "type": "string" }
        },
        "recoverable": { "type": "boolean" }
      },
      "required": ["code", "message", "recoverable"]
    },
    "JobKind": {
      "enum": ["renderStudy", "analyzeStudy", "processStudy"]
    },
    "JobState": {
      "enum": ["queued", "running", "cancelling", "completed", "failed", "cancelled"]
    },
    "JobProgress": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "percent": { "type": "number" },
        "stage": { "type": "string" },
        "message": { "type": "string" }
      },
      "required": ["percent", "stage", "message"]
    },
    "StartedJob": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "jobId": { "type": "string" }
      },
      "required": ["jobId"]
    },
    "JobCommand": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "jobId": { "type": "string" }
      },
      "required": ["jobId"]
    },
    "GetJobsCommand": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "jobIds": {
          "type": "array",
          "items": { "type": "string" }
        }
      },
      "required": ["jobIds"]
    },
    "OpenStudyCommand": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "inputPath": { "type": "string" }
      },
      "required": ["inputPath"]
    },
    "StudyRecord": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "studyId": { "type": "string" },
        "inputPath": { "type": "string" },
        "inputName": { "type": "string" },
        "measurementScale": {
          "anyOf": [
            { "$ref": "#/$defs/MeasurementScale" },
            { "type": "null" }
          ]
        }
      },
      "required": ["studyId", "inputPath", "inputName"]
    },
    "OpenStudyCommandResult": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "study": { "$ref": "#/$defs/StudyRecord" }
      },
      "required": ["study"]
    },
    "RenderStudyCommand": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "studyId": { "type": "string" }
      },
      "required": ["studyId"]
    },
    "RenderStudyCommandResult": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "studyId": { "type": "string" },
        "previewPath": { "type": "string" },
        "loadedWidth": { "type": "number" },
        "loadedHeight": { "type": "number" },
        "measurementScale": {
          "anyOf": [
            { "$ref": "#/$defs/MeasurementScale" },
            { "type": "null" }
          ]
        }
      },
      "required": ["studyId", "previewPath", "loadedWidth", "loadedHeight"]
    },
    "AnalyzeStudyCommand": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "studyId": { "type": "string" }
      },
      "required": ["studyId"]
    },
    "AnalyzeStudyCommandResult": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "studyId": { "type": "string" },
        "previewPath": { "type": "string" },
        "filledPreviewPath": { "type": "string" },
        "loadedWidth": { "type": "number" },
        "loadedHeight": { "type": "number" },
        "mode": { "type": "string" },
        "measurementScale": {
          "anyOf": [
            { "$ref": "#/$defs/MeasurementScale" },
            { "type": "null" }
          ]
        }
      },
      "required": ["studyId", "previewPath", "filledPreviewPath", "loadedWidth", "loadedHeight", "mode"]
    },
    "ProcessStudyCommand": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "studyId": { "type": "string" },
        "outputPath": {
          "type": ["string", "null"]
        },
        "presetId": { "type": "string" },
        "invert": { "type": "boolean" },
        "brightness": {
          "type": ["number", "null"]
        },
        "contrast": {
          "type": ["number", "null"]
        },
        "equalize": { "type": "boolean" },
        "compare": { "type": "boolean" },
        "palette": {
          "anyOf": [
            { "$ref": "#/$defs/PaletteName" },
            { "type": "null" }
          ]
        }
      },
      "required": ["studyId", "presetId", "invert", "equalize", "compare"]
    },
    "ProcessStudyCommandResult": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "studyId": { "type": "string" },
        "previewPath": { "type": "string" },
        "dicomPath": { "type": "string" },
        "loadedWidth": { "type": "number" },
        "loadedHeight": { "type": "number" },
        "mode": { "type": "string" },
        "measurementScale": {
          "anyOf": [
            { "$ref": "#/$defs/MeasurementScale" },
            { "type": "null" }
          ]
        }
      },
      "required": [
        "studyId",
        "previewPath",
        "dicomPath",
        "loadedWidth",
        "loadedHeight",
        "mode"
      ]
    },
    "MeasureLineAnnotationCommand": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "studyId": { "type": "string" },
        "annotation": { "$ref": "#/$defs/LineAnnotation" }
      },
      "required": ["studyId", "annotation"]
    },
    "MeasureLineAnnotationCommandResult": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "studyId": { "type": "string" },
        "annotation": { "$ref": "#/$defs/LineAnnotation" }
      },
      "required": ["studyId", "annotation"]
    },
    "JobResult": {
      "oneOf": [
        {
          "type": "object",
          "additionalProperties": false,
          "properties": {
            "kind": { "const": "renderStudy" },
            "payload": { "$ref": "#/$defs/RenderStudyCommandResult" }
          },
          "required": ["kind", "payload"]
        },
        {
          "type": "object",
          "additionalProperties": false,
          "properties": {
            "kind": { "const": "analyzeStudy" },
            "payload": { "$ref": "#/$defs/AnalyzeStudyCommandResult" }
          },
          "required": ["kind", "payload"]
        },
        {
          "type": "object",
          "additionalProperties": false,
          "properties": {
            "kind": { "const": "processStudy" },
            "payload": { "$ref": "#/$defs/ProcessStudyCommandResult" }
          },
          "required": ["kind", "payload"]
        }
      ]
    },
    "JobSnapshot": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "jobId": { "type": "string" },
        "jobKind": { "$ref": "#/$defs/JobKind" },
        "studyId": {
          "type": ["string", "null"]
        },
        "state": { "$ref": "#/$defs/JobState" },
        "progress": { "$ref": "#/$defs/JobProgress" },
        "fromCache": { "type": "boolean" },
        "result": {
          "anyOf": [
            { "$ref": "#/$defs/JobResult" },
            { "type": "null" }
          ]
        },
        "error": {
          "anyOf": [
            { "$ref": "#/$defs/BackendError" },
            { "type": "null" }
          ]
        }
      },
      "required": ["jobId", "jobKind", "state", "progress", "fromCache"]
    }
  }
}`

type PaletteName string

const (
	PaletteNone PaletteName = "none"
	PaletteHot PaletteName = "hot"
	PaletteBone PaletteName = "bone"
)

type ProcessingControls struct {
	Brightness int `json:"brightness"`
	Contrast float64 `json:"contrast"`
	Invert bool `json:"invert"`
	Equalize bool `json:"equalize"`
	Palette PaletteName `json:"palette"`
}

type ProcessingPreset struct {
	ID string `json:"id"`
	Controls ProcessingControls `json:"controls"`
}

type ProcessingManifest struct {
	DefaultPresetID string `json:"defaultPresetId"`
	Presets []ProcessingPreset `json:"presets"`
}

type MeasurementScale struct {
	RowSpacingMM float64 `json:"rowSpacingMm"`
	ColumnSpacingMM float64 `json:"columnSpacingMm"`
	Source string `json:"source"`
}

type AnnotationSource string

const (
	AnnotationSourceManual AnnotationSource = "manual"
)

type AnnotationPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type LineMeasurement struct {
	PixelLength float64 `json:"pixelLength"`
	CalibratedLengthMM *float64 `json:"calibratedLengthMm,omitempty"`
}

type LineAnnotation struct {
	ID string `json:"id"`
	Label string `json:"label"`
	Source AnnotationSource `json:"source"`
	Start AnnotationPoint `json:"start"`
	End AnnotationPoint `json:"end"`
	Editable bool `json:"editable"`
	Confidence *float64 `json:"confidence,omitempty"`
	Measurement *LineMeasurement `json:"measurement,omitempty"`
}

type RectangleAnnotation struct {
	ID string `json:"id"`
	Label string `json:"label"`
	Source AnnotationSource `json:"source"`
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Width float64 `json:"width"`
	Height float64 `json:"height"`
	Editable bool `json:"editable"`
	Confidence *float64 `json:"confidence,omitempty"`
}

type PolylineAnnotation struct {
	ID string `json:"id"`
	Label string `json:"label"`
	Source AnnotationSource `json:"source"`
	Points []AnnotationPoint `json:"points"`
	Closed bool `json:"closed"`
	Editable bool `json:"editable"`
	Confidence *float64 `json:"confidence,omitempty"`
}

type AnnotationBundle struct {
	Lines []LineAnnotation `json:"lines"`
	Rectangles []RectangleAnnotation `json:"rectangles"`
	Polylines []PolylineAnnotation `json:"polylines"`
}

type BackendErrorCode string

const (
	BackendErrorCodeInvalidInput BackendErrorCode = "invalidInput"
	BackendErrorCodeNotFound BackendErrorCode = "notFound"
	BackendErrorCodeCancelled BackendErrorCode = "cancelled"
	BackendErrorCodeConflict BackendErrorCode = "conflict"
	BackendErrorCodeCacheCorrupted BackendErrorCode = "cacheCorrupted"
	BackendErrorCodeInternal BackendErrorCode = "internal"
)

type BackendError struct {
	Code BackendErrorCode `json:"code"`
	Message string `json:"message"`
	Details []string `json:"details,omitempty"`
	Recoverable bool `json:"recoverable"`
}

type JobKind string

const (
	JobKindRenderStudy JobKind = "renderStudy"
	JobKindAnalyzeStudy JobKind = "analyzeStudy"
	JobKindProcessStudy JobKind = "processStudy"
)

type JobState string

const (
	JobStateQueued JobState = "queued"
	JobStateRunning JobState = "running"
	JobStateCancelling JobState = "cancelling"
	JobStateCompleted JobState = "completed"
	JobStateFailed JobState = "failed"
	JobStateCancelled JobState = "cancelled"
)

type JobProgress struct {
	Percent int `json:"percent"`
	Stage string `json:"stage"`
	Message string `json:"message"`
}

type StartedJob struct {
	JobID string `json:"jobId"`
}

type JobCommand struct {
	JobID string `json:"jobId"`
}

type GetJobsCommand struct {
	JobIDs []string `json:"jobIds"`
}

type OpenStudyCommand struct {
	InputPath string `json:"inputPath"`
}

type StudyRecord struct {
	StudyID string `json:"studyId"`
	InputPath string `json:"inputPath"`
	InputName string `json:"inputName"`
	MeasurementScale *MeasurementScale `json:"measurementScale,omitempty"`
}

type OpenStudyCommandResult struct {
	Study StudyRecord `json:"study"`
}

type RenderStudyCommand struct {
	StudyID string `json:"studyId"`
}

type RenderStudyCommandResult struct {
	StudyID string `json:"studyId"`
	PreviewPath string `json:"previewPath"`
	LoadedWidth uint32 `json:"loadedWidth"`
	LoadedHeight uint32 `json:"loadedHeight"`
	MeasurementScale *MeasurementScale `json:"measurementScale,omitempty"`
}

type AnalyzeStudyCommand struct {
	StudyID string `json:"studyId"`
}

type AnalyzeStudyCommandResult struct {
	StudyID string `json:"studyId"`
	PreviewPath string `json:"previewPath"`
	FilledPreviewPath string `json:"filledPreviewPath"`
	LoadedWidth uint32 `json:"loadedWidth"`
	LoadedHeight uint32 `json:"loadedHeight"`
	Mode string `json:"mode"`
	MeasurementScale *MeasurementScale `json:"measurementScale,omitempty"`
}

type ProcessStudyCommand struct {
	StudyID string `json:"studyId"`
	OutputPath *string `json:"outputPath,omitempty"`
	PresetID string `json:"presetId"`
	Invert bool `json:"invert"`
	Brightness *int `json:"brightness,omitempty"`
	Contrast *float64 `json:"contrast,omitempty"`
	Equalize bool `json:"equalize"`
	Compare bool `json:"compare"`
	Palette *PaletteName `json:"palette,omitempty"`
}

type ProcessStudyCommandResult struct {
	StudyID string `json:"studyId"`
	PreviewPath string `json:"previewPath"`
	DicomPath string `json:"dicomPath"`
	LoadedWidth uint32 `json:"loadedWidth"`
	LoadedHeight uint32 `json:"loadedHeight"`
	Mode string `json:"mode"`
	MeasurementScale *MeasurementScale `json:"measurementScale,omitempty"`
}

type MeasureLineAnnotationCommand struct {
	StudyID string `json:"studyId"`
	Annotation LineAnnotation `json:"annotation"`
}

type MeasureLineAnnotationCommandResult struct {
	StudyID string `json:"studyId"`
	Annotation LineAnnotation `json:"annotation"`
}

type JobResult struct {
	Kind    JobKind `json:"kind"`
	Payload any     `json:"payload"`
}

type JobSnapshot struct {
	JobID string `json:"jobId"`
	JobKind JobKind `json:"jobKind"`
	StudyID *string `json:"studyId,omitempty"`
	State JobState `json:"state"`
	Progress JobProgress `json:"progress"`
	FromCache bool `json:"fromCache"`
	Result *JobResult `json:"result,omitempty"`
	Error *BackendError `json:"error,omitempty"`
}

var DefinitionRefs = map[string]string{
	"PaletteName": "#/$defs/PaletteName",
	"ProcessingControls": "#/$defs/ProcessingControls",
	"ProcessingPreset": "#/$defs/ProcessingPreset",
	"ProcessingManifest": "#/$defs/ProcessingManifest",
	"MeasurementScale": "#/$defs/MeasurementScale",
	"AnnotationSource": "#/$defs/AnnotationSource",
	"AnnotationPoint": "#/$defs/AnnotationPoint",
	"LineMeasurement": "#/$defs/LineMeasurement",
	"LineAnnotation": "#/$defs/LineAnnotation",
	"RectangleAnnotation": "#/$defs/RectangleAnnotation",
	"PolylineAnnotation": "#/$defs/PolylineAnnotation",
	"AnnotationBundle": "#/$defs/AnnotationBundle",
	"BackendErrorCode": "#/$defs/BackendErrorCode",
	"BackendError": "#/$defs/BackendError",
	"JobKind": "#/$defs/JobKind",
	"JobState": "#/$defs/JobState",
	"JobProgress": "#/$defs/JobProgress",
	"StartedJob": "#/$defs/StartedJob",
	"JobCommand": "#/$defs/JobCommand",
	"GetJobsCommand": "#/$defs/GetJobsCommand",
	"OpenStudyCommand": "#/$defs/OpenStudyCommand",
	"StudyRecord": "#/$defs/StudyRecord",
	"OpenStudyCommandResult": "#/$defs/OpenStudyCommandResult",
	"RenderStudyCommand": "#/$defs/RenderStudyCommand",
	"RenderStudyCommandResult": "#/$defs/RenderStudyCommandResult",
	"AnalyzeStudyCommand": "#/$defs/AnalyzeStudyCommand",
	"AnalyzeStudyCommandResult": "#/$defs/AnalyzeStudyCommandResult",
	"ProcessStudyCommand": "#/$defs/ProcessStudyCommand",
	"ProcessStudyCommandResult": "#/$defs/ProcessStudyCommandResult",
	"MeasureLineAnnotationCommand": "#/$defs/MeasureLineAnnotationCommand",
	"MeasureLineAnnotationCommandResult": "#/$defs/MeasureLineAnnotationCommandResult",
	"JobResult": "#/$defs/JobResult",
	"JobSnapshot": "#/$defs/JobSnapshot",
}
