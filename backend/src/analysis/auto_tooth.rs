use crate::analysis::measurement_service::measure_line;
use crate::annotations::{
    AnnotationBundle, AnnotationPoint, AnnotationSource, LineAnnotation, RectangleAnnotation,
};
use crate::tooth_measurement::{Point, ToothAnalysis};

pub fn suggested_annotations(analysis: &ToothAnalysis) -> AnnotationBundle {
    if analysis.teeth.is_empty() {
        return AnnotationBundle::default();
    }

    let measurement_scale = analysis.calibration.measurement_scale;
    let mut lines = Vec::with_capacity(analysis.teeth.len() * 2);
    let mut rectangles = Vec::with_capacity(analysis.teeth.len());

    for (index, tooth) in analysis.teeth.iter().enumerate() {
        let tooth_number = index + 1;
        lines.push(LineAnnotation {
            id: format!("auto-tooth-{tooth_number}-width"),
            label: format!("Tooth {tooth_number} width"),
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
        });
        lines.push(LineAnnotation {
            id: format!("auto-tooth-{tooth_number}-height"),
            label: format!("Tooth {tooth_number} height"),
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
        });
        rectangles.push(RectangleAnnotation {
            id: format!("auto-tooth-{tooth_number}-bounding-box"),
            label: format!("Tooth {tooth_number} bounding box"),
            source: AnnotationSource::AutoTooth,
            x: f64::from(tooth.geometry.bounding_box.x),
            y: f64::from(tooth.geometry.bounding_box.y),
            width: f64::from(tooth.geometry.bounding_box.width),
            height: f64::from(tooth.geometry.bounding_box.height),
            editable: false,
            confidence: Some(tooth.confidence),
        });
    }

    AnnotationBundle { lines, rectangles }
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
    fn creates_editable_line_suggestions_for_detected_teeth() {
        let primary = ToothCandidate {
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
        };
        let secondary = ToothCandidate {
            confidence: 0.77,
            mask_area_pixels: 990,
            measurements: ToothMeasurementBundle {
                pixel: ToothMeasurementValues {
                    tooth_width: 36.0,
                    tooth_height: 76.0,
                    bounding_box_width: 40.0,
                    bounding_box_height: 84.0,
                    units: "px",
                },
                calibrated: None,
            },
            geometry: ToothGeometry {
                bounding_box: BoundingBox {
                    x: 200,
                    y: 86,
                    width: 40,
                    height: 86,
                },
                width_line: LineSegment {
                    start: Point { x: 202, y: 104 },
                    end: Point { x: 236, y: 104 },
                },
                height_line: LineSegment {
                    start: Point { x: 220, y: 90 },
                    end: Point { x: 220, y: 172 },
                },
            },
        };
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
            tooth: Some(primary.clone()),
            teeth: vec![primary, secondary],
            warnings: Vec::new(),
        };

        let suggestions = suggested_annotations(&analysis);

        assert_eq!(suggestions.lines.len(), 4);
        assert_eq!(suggestions.rectangles.len(), 2);
        assert_eq!(suggestions.lines[0].id, "auto-tooth-1-width");
        assert_eq!(suggestions.lines[2].id, "auto-tooth-2-width");
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
