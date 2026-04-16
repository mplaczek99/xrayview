package render

import "xrayview/backend/internal/imaging"

type WindowModeKind uint8

const (
	WindowModeDefault WindowModeKind = iota
	WindowModeFullRange
	WindowModeManual
)

// The zero value preserves the historical default: use the source image's
// embedded default window when available, otherwise fall back to full-range
// mapping.
type WindowMode struct {
	Kind         WindowModeKind
	ManualWindow imaging.WindowLevel
}

type WindowTransform struct {
	lower  float32
	upper  float32
	scale  float32
	offset float32
}

func DefaultWindowMode() WindowMode {
	return WindowMode{Kind: WindowModeDefault}
}

func FullRangeWindowMode() WindowMode {
	return WindowMode{Kind: WindowModeFullRange}
}

func ManualWindowMode(window imaging.WindowLevel) WindowMode {
	return WindowMode{
		Kind:         WindowModeManual,
		ManualWindow: window,
	}
}

func NewWindowTransform(window imaging.WindowLevel) *WindowTransform {
	if window.Width <= 1.0 {
		return nil
	}

	scale := 255.0 / (window.Width - 1.0)
	return &WindowTransform{
		lower:  window.Center - 0.5 - (window.Width-1.0)/2.0,
		upper:  window.Center - 0.5 + (window.Width-1.0)/2.0,
		scale:  scale,
		offset: 127.5 - (window.Center-0.5)*scale,
	}
}

func ResolveWindow(source imaging.SourceImage, mode WindowMode) *WindowTransform {
	switch mode.Kind {
	case WindowModeDefault:
		if source.DefaultWindow == nil {
			return nil
		}

		return NewWindowTransform(*source.DefaultWindow)
	case WindowModeFullRange:
		return nil
	case WindowModeManual:
		return NewWindowTransform(mode.ManualWindow)
	default:
		return nil
	}
}

func (transform WindowTransform) Map(value float32) uint8 {
	if value <= transform.lower {
		return 0
	}
	if value > transform.upper {
		return 255
	}

	return ClampToByte(value*transform.scale + transform.offset)
}

func ClampToByte(value float32) uint8 {
	if value <= 0.0 {
		return 0
	}
	if value >= 255.0 {
		return 255
	}

	return uint8(value + 0.5)
}

func MapLinear(value, minValue, maxValue float32) uint8 {
	if maxValue <= minValue {
		return 0
	}

	return ClampToByte((value - minValue) * (255.0 / (maxValue - minValue)))
}
