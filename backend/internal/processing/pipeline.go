package processing

import (
	"fmt"

	"xrayview/backend/internal/imaging"
	"xrayview/backend/internal/render"
)

type PipelineOutput struct {
	Preview imaging.PreviewImage
	Mode    string
}

func ProcessRenderedPreview(
	sourcePreview imaging.PreviewImage,
	controls GrayscaleControls,
	palette string,
	compare bool,
) (PipelineOutput, error) {
	processed, mode, err := ProcessPreviewImage(sourcePreview, controls)
	if err != nil {
		return PipelineOutput{}, err
	}

	normalizedPalette, err := NormalizePaletteName(palette)
	if err != nil {
		processed.Release()
		return PipelineOutput{}, err
	}

	outputPreview := processed
	if normalizedPalette != PaletteNone {
		mode = fmt.Sprintf("%s with %s palette", mode, normalizedPalette)
		paletted, err := ApplyNamedPalette(processed, normalizedPalette)
		processed.Release()
		if err != nil {
			return PipelineOutput{}, err
		}
		outputPreview = paletted
	}

	if compare {
		compared, err := CombineComparison(sourcePreview, outputPreview)
		outputPreview.Release()
		if err != nil {
			return PipelineOutput{}, err
		}
		outputPreview = compared
		mode = fmt.Sprintf("comparison of grayscale and %s", mode)
	}

	return PipelineOutput{
		Preview: outputPreview,
		Mode:    mode,
	}, nil
}

func ProcessSourceImage(
	source imaging.SourceImage,
	plan render.RenderPlan,
	controls GrayscaleControls,
	palette string,
	compare bool,
) (PipelineOutput, error) {
	if err := source.Validate(); err != nil {
		return PipelineOutput{}, fmt.Errorf("validate source image: %w", err)
	}

	sourcePreview := render.RenderSourceImage(source, plan)
	defer sourcePreview.Release()
	return ProcessRenderedPreview(sourcePreview, controls, palette, compare)
}
