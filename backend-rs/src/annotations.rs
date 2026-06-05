use crate::contracts::{AnnotationPoint, LineAnnotation, LineMeasurement, MeasurementScale};

// Public entry point: attach a measurement to an existing line annotation.
// We take ownership and return the updated annotation rather than mutating
// through a borrow — keeps callers cleaner when stitching this into a builder
// chain.
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

// Compute pixel distance and (if scale provided) the calibrated mm distance
// between two points. Scale is per-axis because bitewing X-ray sensors often
// have non-square pixels.
pub fn measure_line(
    start: AnnotationPoint,
    end: AnnotationPoint,
    measurement_scale: Option<&MeasurementScale>,
) -> LineMeasurement {
    let dx = end.x - start.x;
    let dy = end.y - start.y;
    // hypot avoids sqrt(dx*dx + dy*dy) overflow for tiny/huge inputs.
    let pixel_length = round_measurement(dx.hypot(dy));
    // For the calibrated case we multiply each component by its axis spacing
    // *before* taking hypot — that's what makes anisotropic pixel sizes work.
    // Doing pixel_length * scale would only be right if row/col spacing matched.
    let calibrated_length_mm = measurement_scale.map(|scale| {
        round_measurement((dx * scale.column_spacing_mm).hypot(dy * scale.row_spacing_mm))
    });

    LineMeasurement {
        pixel_length,
        calibrated_length_mm,
    }
}

// One decimal place is the spec — clinicians don't read measurements past
// 0.1 mm, and showing more would imply more precision than the sensor has.
fn round_measurement(value: f64) -> f64 {
    (value * 10.0).round() / 10.0
}

#[cfg(test)]
mod tests {
    use super::*;

    // Classic 3-4-5 triangle — sanity check that hypot gives integer 5.
    #[test]
    fn measure_line_measures_pixel_length() {
        let measurement = measure_line(
            AnnotationPoint { x: 12.0, y: 18.0 },
            AnnotationPoint { x: 15.0, y: 22.0 },
            None,
        );

        assert_eq!(measurement.pixel_length, 5.0);
        // No scale was passed, so the mm field must stay None — never 0.
        assert_eq!(measurement.calibrated_length_mm, None);
    }

    // Anisotropic spacing path: row=0.2 mm/px, col=0.3 mm/px, dx=4, dy=3
    // → hypot(4*0.3, 3*0.2) = hypot(1.2, 0.6) ≈ 1.341... → rounds to 1.3.
    // This is the test that catches accidental "scale * pixel_length" math.
    #[test]
    fn measure_line_measures_calibrated_length() {
        let scale = MeasurementScale {
            row_spacing_mm: 0.2,
            column_spacing_mm: 0.3,
            source: "manualCalibration".to_string(),
        };
        let measurement = measure_line(
            AnnotationPoint { x: 10.0, y: 8.0 },
            AnnotationPoint { x: 14.0, y: 11.0 },
            Some(&scale),
        );

        assert_eq!(measurement.pixel_length, 5.0);
        assert_eq!(measurement.calibrated_length_mm, Some(1.3));
    }

    // Pins down the rounding behavior: 0.05 → 0.1 (half away from zero per
    // f64::round). If this ever flips to banker's rounding the UI will start
    // showing 0.0 for tiny line drags, which is the wrong UX.
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
