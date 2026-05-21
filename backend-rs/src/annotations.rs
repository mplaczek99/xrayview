use crate::contracts::{AnnotationPoint, LineAnnotation, LineMeasurement, MeasurementScale};

pub fn measure_line_annotation(
    mut annotation: LineAnnotation,
    measurement_scale: Option<&MeasurementScale>,
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
    measurement_scale: Option<&MeasurementScale>,
) -> LineMeasurement {
    let dx = end.x - start.x;
    let dy = end.y - start.y;
    let pixel_length = round_measurement(dx.hypot(dy));
    let calibrated_length_mm = measurement_scale.map(|scale| {
        round_measurement((dx * scale.column_spacing_mm).hypot(dy * scale.row_spacing_mm))
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
    fn measure_line_measures_pixel_length() {
        let measurement = measure_line(
            AnnotationPoint { x: 12.0, y: 18.0 },
            AnnotationPoint { x: 15.0, y: 22.0 },
            None,
        );

        assert_eq!(measurement.pixel_length, 5.0);
        assert_eq!(measurement.calibrated_length_mm, None);
    }

    #[test]
    fn measure_line_measures_calibrated_length() {
        let scale = MeasurementScale {
            row_spacing_mm: 0.2,
            column_spacing_mm: 0.3,
            source: "PixelSpacing".to_string(),
        };
        let measurement = measure_line(
            AnnotationPoint { x: 10.0, y: 8.0 },
            AnnotationPoint { x: 14.0, y: 11.0 },
            Some(&scale),
        );

        assert_eq!(measurement.pixel_length, 5.0);
        assert_eq!(measurement.calibrated_length_mm, Some(1.3));
    }

    #[test]
    fn measure_line_rounds_half_away_from_zero() {
        let measurement = measure_line(
            AnnotationPoint { x: 0.0, y: 0.0 },
            AnnotationPoint { x: 0.05, y: 0.0 },
            None,
        );

        assert_eq!(measurement.pixel_length, 0.1);
    }
}
