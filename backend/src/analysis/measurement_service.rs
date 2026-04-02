use crate::MeasurementScale;
use crate::annotations::{AnnotationPoint, LineAnnotation, LineMeasurement};

pub fn measure_line_annotation(
    mut annotation: LineAnnotation,
    measurement_scale: Option<MeasurementScale>,
) -> LineAnnotation {
    annotation.measurement = Some(measure_line(
        annotation.start,
        annotation.end,
        measurement_scale,
    ));
    annotation
}

pub fn measure_line(
    start: AnnotationPoint,
    end: AnnotationPoint,
    measurement_scale: Option<MeasurementScale>,
) -> LineMeasurement {
    let dx = end.x - start.x;
    let dy = end.y - start.y;
    let pixel_length = round_measurement((dx * dx + dy * dy).sqrt());
    let calibrated_length_mm = measurement_scale.map(|scale| {
        round_measurement(
            ((dx * scale.column_spacing_mm).powi(2) + (dy * scale.row_spacing_mm).powi(2)).sqrt(),
        )
    });

    LineMeasurement {
        pixel_length,
        calibrated_length_mm,
    }
}

fn round_measurement(value: f64) -> f64 {
    (value * 10.0).round() / 10.0
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn measures_line_length_in_pixels() {
        let measurement = measure_line(
            AnnotationPoint { x: 12.0, y: 18.0 },
            AnnotationPoint { x: 15.0, y: 22.0 },
            None,
        );

        assert_eq!(measurement.pixel_length, 5.0);
        assert_eq!(measurement.calibrated_length_mm, None);
    }

    #[test]
    fn measures_line_length_in_millimeters_when_calibrated() {
        let measurement = measure_line(
            AnnotationPoint { x: 10.0, y: 8.0 },
            AnnotationPoint { x: 14.0, y: 11.0 },
            Some(MeasurementScale {
                row_spacing_mm: 0.2,
                column_spacing_mm: 0.3,
                source: "PixelSpacing",
            }),
        );

        assert_eq!(measurement.pixel_length, 5.0);
        assert_eq!(measurement.calibrated_length_mm, Some(1.3));
    }
}
