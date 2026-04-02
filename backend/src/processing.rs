use anyhow::{Result, bail};
use rayon::prelude::*;

#[derive(Debug, Clone)]
pub struct GrayscaleControls {
    pub invert: bool,
    pub brightness: i32,
    pub contrast: f64,
    pub equalize: bool,
    pub pipeline: Option<String>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum Step {
    Grayscale,
    Invert,
    Brightness,
    Contrast,
    Equalize,
}

const DEFAULT_PIPELINE_ORDER: [Step; 5] = [
    Step::Grayscale,
    Step::Invert,
    Step::Brightness,
    Step::Contrast,
    Step::Equalize,
];

pub fn validate_pipeline(pipeline: Option<&str>) -> Result<()> {
    let _ = pipeline_steps(pipeline)?;
    Ok(())
}

pub fn process_grayscale_pixels(pixels: &mut [u8], controls: &GrayscaleControls) -> Result<String> {
    let requested_steps = pipeline_steps(controls.pipeline.as_deref())?;
    let effective_steps = effective_pipeline_steps(&requested_steps, controls);
    let mut mode = String::from("grayscale");
    let mut lookup = identity_lookup_table();
    let mut pending_lookup = false;

    let flush_lookup = |pixels: &mut [u8], lookup: &mut [u8; 256], pending_lookup: &mut bool| {
        if !*pending_lookup {
            return;
        }
        apply_lookup_in_place(pixels, lookup);
        *lookup = identity_lookup_table();
        *pending_lookup = false;
    };

    for step in effective_steps {
        match step {
            Step::Grayscale => {}
            Step::Invert => {
                if controls.invert {
                    // Invert, brightness, and contrast are all point operations, so we
                    // compose them into one lookup table and touch the pixel buffer once.
                    compose_invert_lookup(&mut lookup);
                    pending_lookup = true;
                    mode = mode.replacen("grayscale", "inverted grayscale", 1);
                }
            }
            Step::Brightness => {
                if controls.brightness != 0 {
                    compose_brightness_lookup(&mut lookup, controls.brightness);
                    pending_lookup = true;
                    mode = format!("{mode} with brightness {:+}", controls.brightness);
                }
            }
            Step::Contrast => {
                if controls.contrast != 1.0 {
                    compose_contrast_lookup(&mut lookup, controls.contrast);
                    pending_lookup = true;
                    mode = format!("{mode} with contrast {}", controls.contrast);
                }
            }
            Step::Equalize => {
                if controls.equalize {
                    // Equalization depends on the current histogram, so any queued point
                    // operations must be applied before we recalculate the distribution.
                    flush_lookup(pixels, &mut lookup, &mut pending_lookup);
                    equalize_histogram_in_place(pixels);
                    mode = format!("{mode} with histogram equalization");
                }
            }
        }
    }

    flush_lookup(pixels, &mut lookup, &mut pending_lookup);
    Ok(mode)
}

fn pipeline_steps(pipeline: Option<&str>) -> Result<Vec<Step>> {
    let Some(pipeline) = pipeline else {
        return Ok(Vec::new());
    };

    let mut steps = Vec::new();
    for raw_step in pipeline.split(',') {
        let step = raw_step.trim().to_ascii_lowercase();
        if step.is_empty() {
            bail!("pipeline steps must not be empty");
        }

        let step = match step.as_str() {
            "grayscale" => Step::Grayscale,
            "invert" => Step::Invert,
            "brightness" => Step::Brightness,
            "contrast" => Step::Contrast,
            "equalize" => Step::Equalize,
            _ => bail!("unknown pipeline step: {step}"),
        };

        if steps.contains(&step) {
            bail!(
                "duplicate pipeline step: {}",
                raw_step.trim().to_ascii_lowercase()
            );
        }
        steps.push(step);
    }

    Ok(steps)
}

fn effective_pipeline_steps(requested: &[Step], controls: &GrayscaleControls) -> Vec<Step> {
    if requested.is_empty() {
        return DEFAULT_PIPELINE_ORDER.to_vec();
    }

    // A custom pipeline primarily reorders enabled filters. Any enabled step the user
    // omitted is appended later so presets can override order without disabling work.
    let mut steps = vec![Step::Grayscale];
    for step in requested {
        if *step == Step::Grayscale || !step_enabled(*step, controls) || steps.contains(step) {
            continue;
        }
        steps.push(*step);
    }

    for step in DEFAULT_PIPELINE_ORDER.iter().skip(1) {
        if step_enabled(*step, controls) && !steps.contains(step) {
            steps.push(*step);
        }
    }

    steps
}

fn step_enabled(step: Step, controls: &GrayscaleControls) -> bool {
    match step {
        Step::Grayscale => true,
        Step::Invert => controls.invert,
        Step::Brightness => controls.brightness != 0,
        Step::Contrast => controls.contrast != 1.0,
        Step::Equalize => controls.equalize,
    }
}

fn identity_lookup_table() -> [u8; 256] {
    let mut lookup = [0_u8; 256];
    for (index, value) in lookup.iter_mut().enumerate() {
        *value = index as u8;
    }
    lookup
}

fn compose_invert_lookup(lookup: &mut [u8; 256]) {
    for value in lookup.iter_mut() {
        *value = 255 - *value;
    }
}

fn compose_brightness_lookup(lookup: &mut [u8; 256], delta: i32) {
    for value in lookup.iter_mut() {
        *value = clamp_lookup_value(i32::from(*value) + delta);
    }
}

fn compose_contrast_lookup(lookup: &mut [u8; 256], factor: f64) {
    for value in lookup.iter_mut() {
        let adjusted = 128.0 + factor * (f64::from(*value) - 128.0);
        *value = clamp_lookup_value(adjusted.round() as i32);
    }
}

fn clamp_lookup_value(value: i32) -> u8 {
    if value < 0 {
        0
    } else if value > 255 {
        255
    } else {
        value as u8
    }
}

fn apply_lookup_in_place(pixels: &mut [u8], lookup: &[u8; 256]) {
    // rayon: parallel pixel loop
    pixels.par_iter_mut().for_each(|value| {
        *value = lookup[*value as usize];
    });
}

fn equalize_histogram_in_place(pixels: &mut [u8]) {
    if pixels.is_empty() {
        return;
    }

    let mut histogram = [0_usize; 256];
    for value in pixels.iter().copied() {
        histogram[value as usize] += 1;
    }

    let total = pixels.len();
    let mut cdf = 0_usize;
    let mut cdf_min = 0_usize;
    let mut found = false;
    for count in histogram.iter().copied() {
        cdf += count;
        if !found && count != 0 {
            cdf_min = cdf;
            found = true;
        }
    }

    if cdf_min == total {
        // A flat image has no contrast to redistribute.
        return;
    }

    let mut lookup = [0_u8; 256];
    cdf = 0;
    let denom = total - cdf_min;
    for (index, count) in histogram.iter().copied().enumerate() {
        cdf += count;
        if cdf <= cdf_min {
            continue;
        }
        let value = ((cdf - cdf_min) * 255 + denom / 2) / denom;
        lookup[index] = value as u8;
    }

    apply_lookup_in_place(pixels, &lookup);
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn custom_pipeline_keeps_omitted_enabled_filters() {
        let mut pixels = vec![100_u8];
        let controls = GrayscaleControls {
            invert: false,
            brightness: 20,
            contrast: 2.0,
            equalize: false,
            pipeline: Some(String::from("contrast")),
        };

        let _ = process_grayscale_pixels(&mut pixels, &controls).expect("process grayscale pixels");

        assert_eq!(pixels, vec![92]);
    }

    #[test]
    fn rejects_duplicate_pipeline_step() {
        let err = validate_pipeline(Some("grayscale,contrast,contrast"))
            .expect_err("pipeline should be rejected");
        assert_eq!(err.to_string(), "duplicate pipeline step: contrast");
    }
}
