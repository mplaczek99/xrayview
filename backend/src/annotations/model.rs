use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub enum AnnotationSource {
    Manual,
    AutoTooth,
}

#[derive(Debug, Clone, Copy, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AnnotationPoint {
    pub x: f64,
    pub y: f64,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct LineMeasurement {
    pub pixel_length: f64,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub calibrated_length_mm: Option<f64>,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct LineAnnotation {
    pub id: String,
    pub label: String,
    pub source: AnnotationSource,
    pub start: AnnotationPoint,
    pub end: AnnotationPoint,
    pub editable: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub confidence: Option<f64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub measurement: Option<LineMeasurement>,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct RectangleAnnotation {
    pub id: String,
    pub label: String,
    pub source: AnnotationSource,
    pub x: f64,
    pub y: f64,
    pub width: f64,
    pub height: f64,
    pub editable: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub confidence: Option<f64>,
}

#[derive(Debug, Default, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AnnotationBundle {
    pub lines: Vec<LineAnnotation>,
    pub rectangles: Vec<RectangleAnnotation>,
}

impl AnnotationBundle {
    pub fn is_empty(&self) -> bool {
        self.lines.is_empty() && self.rectangles.is_empty()
    }
}
