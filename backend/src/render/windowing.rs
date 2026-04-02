#[derive(Debug, Clone, Copy, PartialEq)]
pub struct WindowLevel {
    pub center: f32,
    pub width: f32,
}

#[derive(Debug, Clone, Copy, PartialEq, Default)]
#[allow(dead_code)] // Render spec variants will be wired into the frontend in a later phase.
pub enum WindowMode {
    #[default]
    Default,
    FullRange,
    Manual(WindowLevel),
}

#[derive(Debug, Clone, Copy)]
pub struct WindowTransform {
    lower: f32,
    upper: f32,
    scale: f32,
    offset: f32,
}

impl WindowTransform {
    pub fn from_window(window: WindowLevel) -> Option<Self> {
        if window.width <= 1.0 {
            return None;
        }

        let scale = 255.0 / (window.width - 1.0);
        Some(Self {
            lower: window.center - 0.5 - (window.width - 1.0) / 2.0,
            upper: window.center - 0.5 + (window.width - 1.0) / 2.0,
            scale,
            offset: 127.5 - (window.center - 0.5) * scale,
        })
    }

    pub fn map(self, value: f32) -> u8 {
        if value <= self.lower {
            0
        } else if value > self.upper {
            255
        } else {
            clamp_to_byte(value * self.scale + self.offset)
        }
    }
}

pub fn clamp_to_byte(value: f32) -> u8 {
    if value <= 0.0 {
        0
    } else if value >= 255.0 {
        255
    } else {
        (value + 0.5) as u8
    }
}

pub fn map_linear(value: f32, min_value: f32, max_value: f32) -> u8 {
    if max_value <= min_value {
        return 0;
    }

    clamp_to_byte((value - min_value) * (255.0 / (max_value - min_value)))
}

#[cfg(test)]
mod tests {
    use super::{WindowLevel, WindowTransform, map_linear};

    #[test]
    fn dicom_window_mapping_matches_expected_breakpoints() {
        let transform = WindowTransform::from_window(WindowLevel {
            center: 128.0,
            width: 256.0,
        })
        .expect("window transform");

        assert_eq!(transform.map(0.0), 0);
        assert_eq!(transform.map(127.5), 128);
        assert_eq!(transform.map(255.0), 255);
    }

    #[test]
    fn linear_mapping_uses_full_available_range() {
        assert_eq!(map_linear(0.0, 0.0, 1024.0), 0);
        assert_eq!(map_linear(512.0, 0.0, 1024.0), 128);
        assert_eq!(map_linear(1024.0, 0.0, 1024.0), 255);
    }
}
