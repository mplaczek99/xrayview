use crate::analysis::measurement_service::measure_line;
use crate::annotations::{
    AnnotationBundle, AnnotationPoint, AnnotationSource, LineAnnotation, RectangleAnnotation,
};
use crate::tooth_measurement::{Point, ToothAnalysis};

pub fn suggested_annotations(analysis: &ToothAnalysis) -> AnnotationBundle {
    let Some(tooth) = analysis.tooth.as_ref() else {
        return AnnotationBundle::default();
    };

    let measurement_scale = analysis.calibration.measurement_scale;
    let width_line = LineAnnotation {
        id: "auto-tooth-width".into(),
        label: "Tooth width".into(),
        source: AnnotationSource::AutoTooth,
        start: point(tooth.geometry.width_line.start),
        end: point(tooth.geometry.width_line.end),
        editable: true,
        confidence: Some(tooth.confidence),
        measurement: Some(measure_line(
            point(tooth.geometry.width_line.start),
            point(tooth.geometry.width_line.end),
            measurement_scale,
        )),
    };
    let height_line = LineAnnotation {
        id: "auto-tooth-height".into(),
        label: "Tooth height".into(),
        source: AnnotationSource::AutoTooth,
        start: point(tooth.geometry.height_line.start),
        end: point(tooth.geometry.height_line.end),
        editable: true,
        confidence: Some(tooth.confidence),
        measurement: Some(measure_line(
            point(tooth.geometry.height_line.start),
            point(tooth.geometry.height_line.end),
            measurement_scale,
        )),
    };
    let bounding_box = RectangleAnnotation {
        id: "auto-tooth-bounding-box".into(),
        label: "Tooth bounding box".into(),
        source: AnnotationSource::AutoTooth,
        x: f64::from(tooth.geometry.bounding_box.x),
        y: f64::from(tooth.geometry.bounding_box.y),
        width: f64::from(tooth.geometry.bounding_box.width),
        height: f64::from(tooth.geometry.bounding_box.height),
        editable: false,
        confidence: Some(tooth.confidence),
    };

    AnnotationBundle {
        lines: vec![width_line, height_line],
        rectangles: vec![bounding_box],
    }
}

fn point(point: Point) -> AnnotationPoint {
    AnnotationPoint {
        x: f64::from(point.x),
        y: f64::from(point.y),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::MeasurementScale;
    use crate::tooth_measurement::{
        BoundingBox, LineSegment, ToothCalibration, ToothCandidate, ToothGeometry,
        ToothImageMetadata, ToothMeasurementBundle, ToothMeasurementValues,
    };

    #[test]
    fn creates_editable_line_suggestions_for_detected_tooth() {
        let analysis = ToothAnalysis {
            image: ToothImageMetadata {
                width: 640,
                height: 480,
            },
            calibration: ToothCalibration {
                pixel_units: "px",
                measurement_scale: Some(MeasurementScale {
                    row_spacing_mm: 0.2,
                    column_spacing_mm: 0.3,
                    source: "PixelSpacing",
                }),
                real_world_measurements_available: true,
            },
            tooth: Some(ToothCandidate {
                confidence: 0.82,
                mask_area_pixels: 1200,
                measurements: ToothMeasurementBundle {
                    pixel: ToothMeasurementValues {
                        tooth_width: 40.0,
                        tooth_height: 80.0,
                        bounding_box_width: 42.0,
                        bounding_box_height: 88.0,
                        units: "px",
                    },
                    calibrated: None,
                },
                geometry: ToothGeometry {
                    bounding_box: BoundingBox {
                        x: 120,
                        y: 80,
                        width: 44,
                        height: 92,
                    },
                    width_line: LineSegment {
                        start: Point { x: 122, y: 96 },
                        end: Point { x: 160, y: 96 },
                    },
                    height_line: LineSegment {
                        start: Point { x: 141, y: 84 },
                        end: Point { x: 141, y: 170 },
                    },
                },
            }),
            warnings: Vec::new(),
        };

        let suggestions = suggested_annotations(&analysis);

        assert_eq!(suggestions.lines.len(), 2);
        assert_eq!(suggestions.rectangles.len(), 1);
        assert_eq!(suggestions.lines[0].source, AnnotationSource::AutoTooth);
        assert_eq!(
            suggestions.lines[0]
                .measurement
                .as_ref()
                .and_then(|value| value.calibrated_length_mm),
            Some(11.4)
        );
        assert!(!suggestions.rectangles[0].editable);
    }
}
