package processing

import (
	"fmt"

	"xrayview/go-backend/internal/imaging"
	"xrayview/go-backend/internal/render"
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
		return PipelineOutput{}, err
	}

	outputPreview := processed
	if normalizedPalette != PaletteNone {
		mode = fmt.Sprintf("%s with %s palette", mode, normalizedPalette)
		outputPreview, err = ApplyNamedPalette(processed, normalizedPalette)
		if err != nil {
			return PipelineOutput{}, err
		}
	}

	if compare {
		outputPreview, err = CombineComparison(sourcePreview, outputPreview)
		if err != nil {
			return PipelineOutput{}, err
		}
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
	return ProcessRenderedPreview(sourcePreview, controls, palette, compare)
}
