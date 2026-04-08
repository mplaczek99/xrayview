package render

import (
	"testing"

	"xrayview/backend/internal/imaging"
)

func TestNewWindowTransformRejectsWindowWidthsAtOrBelowOne(t *testing.T) {
	for _, width := range []float32{0, 1} {
		transform := NewWindowTransform(imaging.WindowLevel{
			Center: 128,
			Width:  width,
		})
		if transform != nil {
			t.Fatalf("NewWindowTransform(%v) = %#v, want nil", width, transform)
		}
	}
}

func TestWindowTransformMapMatchesRustBreakpoints(t *testing.T) {
	transform := NewWindowTransform(imaging.WindowLevel{
		Center: 128,
		Width:  256,
	})
	if transform == nil {
		t.Fatal("NewWindowTransform returned nil, want transform")
	}

	for _, testCase := range []struct {
		name  string
		value float32
		want  uint8
	}{
		{name: "lower bound", value: 0, want: 0},
		{name: "midpoint", value: 127.5, want: 128},
		{name: "upper bound", value: 255, want: 255},
	} {
		if got := transform.Map(testCase.value); got != testCase.want {
			t.Fatalf("%s: Map(%v) = %d, want %d", testCase.name, testCase.value, got, testCase.want)
		}
	}
}

func TestClampToByteRoundsAndClamps(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		value float32
		want  uint8
	}{
		{name: "negative clamps low", value: -4, want: 0},
		{name: "rounds down below midpoint", value: 127.49, want: 127},
		{name: "rounds up at midpoint", value: 127.5, want: 128},
		{name: "high clamps", value: 300, want: 255},
	} {
		if got := ClampToByte(testCase.value); got != testCase.want {
			t.Fatalf("%s: ClampToByte(%v) = %d, want %d", testCase.name, testCase.value, got, testCase.want)
		}
	}
}

func TestMapLinearUsesFullAvailableRange(t *testing.T) {
	for _, testCase := range []struct {
		value float32
		want  uint8
	}{
		{value: 0, want: 0},
		{value: 512, want: 128},
		{value: 1024, want: 255},
	} {
		if got := MapLinear(testCase.value, 0, 1024); got != testCase.want {
			t.Fatalf("MapLinear(%v, 0, 1024) = %d, want %d", testCase.value, got, testCase.want)
		}
	}
}

func TestResolveWindowDefaultUsesEmbeddedWindow(t *testing.T) {
	source := imaging.SourceImage{
		MinValue: 0,
		MaxValue: 255,
		DefaultWindow: &imaging.WindowLevel{
			Center: 128,
			Width:  256,
		},
	}

	if got := mapSourceValue(source, DefaultWindowMode(), 127.5); got != 128 {
		t.Fatalf("default window mapping = %d, want 128", got)
	}
}

func TestResolveWindowDefaultFallsBackToFullRangeWhenWindowMissing(t *testing.T) {
	source := imaging.SourceImage{
		MinValue: 0,
		MaxValue: 1024,
	}

	if got := mapSourceValue(source, DefaultWindowMode(), 512); got != 128 {
		t.Fatalf("default fallback mapping = %d, want 128", got)
	}
}

func TestResolveWindowDefaultFallsBackToFullRangeWhenWindowInvalid(t *testing.T) {
	source := imaging.SourceImage{
		MinValue: 0,
		MaxValue: 1024,
		DefaultWindow: &imaging.WindowLevel{
			Center: 512,
			Width:  1,
		},
	}

	if got := mapSourceValue(source, DefaultWindowMode(), 512); got != 128 {
		t.Fatalf("invalid default window fallback mapping = %d, want 128", got)
	}
}

func TestResolveWindowFullRangeIgnoresEmbeddedWindow(t *testing.T) {
	source := imaging.SourceImage{
		MinValue: 0,
		MaxValue: 128,
		DefaultWindow: &imaging.WindowLevel{
			Center: 32,
			Width:  64,
		},
	}

	if got := mapSourceValue(source, FullRangeWindowMode(), 64); got != 128 {
		t.Fatalf("full-range mapping = %d, want 128", got)
	}
}

func TestResolveWindowManualOverridesEmbeddedWindow(t *testing.T) {
	source := imaging.SourceImage{
		MinValue: 0,
		MaxValue: 255,
		DefaultWindow: &imaging.WindowLevel{
			Center: 32,
			Width:  64,
		},
	}

	if got := mapSourceValue(source, ManualWindowMode(imaging.WindowLevel{
		Center: 128,
		Width:  256,
	}), 127.5); got != 128 {
		t.Fatalf("manual window mapping = %d, want 128", got)
	}
}

func TestResolveWindowManualFallsBackToFullRangeWhenWindowInvalid(t *testing.T) {
	source := imaging.SourceImage{
		MinValue: 0,
		MaxValue: 1024,
	}

	if got := mapSourceValue(source, ManualWindowMode(imaging.WindowLevel{
		Center: 512,
		Width:  1,
	}), 512); got != 128 {
		t.Fatalf("invalid manual window fallback mapping = %d, want 128", got)
	}
}

func mapSourceValue(source imaging.SourceImage, mode WindowMode, value float32) uint8 {
	if transform := ResolveWindow(source, mode); transform != nil {
		return transform.Map(value)
	}

	return MapLinear(value, source.MinValue, source.MaxValue)
}
