package cache

import (
	"errors"
	"log/slog"
	"os"
	"sync"

	"xrayview/go-backend/internal/contracts"
)

type Memory struct {
	mu      sync.Mutex
	logger  *slog.Logger
	entries map[string]contracts.JobResult
}

func NewMemory(logger *slog.Logger) *Memory {
	return &Memory{
		logger:  logger,
		entries: make(map[string]contracts.JobResult),
	}
}

func (memory *Memory) StoreRender(
	fingerprint string,
	result contracts.RenderStudyCommandResult,
) {
	memory.storeLocked(fingerprint, contracts.JobResult{
		Kind:    contracts.JobKindRenderStudy,
		Payload: cloneRenderResult(result),
	})
}

func (memory *Memory) LoadRender(
	fingerprint string,
) (contracts.RenderStudyCommandResult, bool) {
	var zero contracts.RenderStudyCommandResult

	result, ok := memory.loadLocked(fingerprint, contracts.JobKindRenderStudy)
	if !ok {
		return zero, false
	}

	payload, ok := result.Payload.(contracts.RenderStudyCommandResult)
	if !ok {
		memory.discardInvalidEntry(fingerprint, result.Kind, "render payload type mismatch")
		return zero, false
	}

	return cloneRenderResult(payload), true
}

func (memory *Memory) StoreProcess(
	fingerprint string,
	result contracts.ProcessStudyCommandResult,
) {
	memory.storeLocked(fingerprint, contracts.JobResult{
		Kind:    contracts.JobKindProcessStudy,
		Payload: cloneProcessResult(result),
	})
}

func (memory *Memory) LoadProcess(
	fingerprint string,
) (contracts.ProcessStudyCommandResult, bool) {
	var zero contracts.ProcessStudyCommandResult

	result, ok := memory.loadLocked(fingerprint, contracts.JobKindProcessStudy)
	if !ok {
		return zero, false
	}

	payload, ok := result.Payload.(contracts.ProcessStudyCommandResult)
	if !ok {
		memory.discardInvalidEntry(fingerprint, result.Kind, "process payload type mismatch")
		return zero, false
	}

	return cloneProcessResult(payload), true
}

func (memory *Memory) StoreAnalyze(
	fingerprint string,
	result contracts.AnalyzeStudyCommandResult,
) {
	memory.storeLocked(fingerprint, contracts.JobResult{
		Kind:    contracts.JobKindAnalyzeStudy,
		Payload: cloneAnalyzeResult(result),
	})
}

func (memory *Memory) LoadAnalyze(
	fingerprint string,
) (contracts.AnalyzeStudyCommandResult, bool) {
	var zero contracts.AnalyzeStudyCommandResult

	result, ok := memory.loadLocked(fingerprint, contracts.JobKindAnalyzeStudy)
	if !ok {
		return zero, false
	}

	payload, ok := result.Payload.(contracts.AnalyzeStudyCommandResult)
	if !ok {
		memory.discardInvalidEntry(fingerprint, result.Kind, "analyze payload type mismatch")
		return zero, false
	}

	return cloneAnalyzeResult(payload), true
}

func (memory *Memory) storeLocked(fingerprint string, result contracts.JobResult) {
	memory.mu.Lock()
	defer memory.mu.Unlock()

	memory.entries[fingerprint] = result
}

func (memory *Memory) loadLocked(
	fingerprint string,
	expectedKind contracts.JobKind,
) (contracts.JobResult, bool) {
	memory.mu.Lock()
	defer memory.mu.Unlock()

	result, ok := memory.entries[fingerprint]
	if !ok {
		return contracts.JobResult{}, false
	}

	if !resultArtifactsExist(result, memory.logger, fingerprint) {
		delete(memory.entries, fingerprint)
		return contracts.JobResult{}, false
	}

	if result.Kind != expectedKind {
		memory.warnInvalidEntry(
			fingerprint,
			result.Kind,
			"memory cache entry kind mismatch",
			slog.String("expected_kind", string(expectedKind)),
		)
		delete(memory.entries, fingerprint)
		return contracts.JobResult{}, false
	}

	return result, true
}

func (memory *Memory) discardInvalidEntry(
	fingerprint string,
	kind contracts.JobKind,
	message string,
) {
	memory.mu.Lock()
	defer memory.mu.Unlock()

	memory.warnInvalidEntry(fingerprint, kind, message)
	delete(memory.entries, fingerprint)
}

func (memory *Memory) warnInvalidEntry(
	fingerprint string,
	kind contracts.JobKind,
	message string,
	attrs ...slog.Attr,
) {
	if memory.logger == nil {
		return
	}

	allAttrs := []slog.Attr{
		slog.String("fingerprint", fingerprint),
		slog.String("job_kind", string(kind)),
	}
	allAttrs = append(allAttrs, attrs...)
	memory.logger.Warn(message, attrsToAny(allAttrs)...)
}

func attrsToAny(attrs []slog.Attr) []any {
	values := make([]any, 0, len(attrs))
	for _, attr := range attrs {
		values = append(values, attr)
	}

	return values
}

func resultArtifactsExist(
	result contracts.JobResult,
	logger *slog.Logger,
	fingerprint string,
) bool {
	switch result.Kind {
	case contracts.JobKindRenderStudy:
		payload, ok := result.Payload.(contracts.RenderStudyCommandResult)
		if !ok {
			warnPayloadTypeMismatch(logger, fingerprint, result.Kind)
			return false
		}

		return artifactExists(logger, fingerprint, result.Kind, payload.PreviewPath)
	case contracts.JobKindProcessStudy:
		payload, ok := result.Payload.(contracts.ProcessStudyCommandResult)
		if !ok {
			warnPayloadTypeMismatch(logger, fingerprint, result.Kind)
			return false
		}

		return artifactExists(logger, fingerprint, result.Kind, payload.PreviewPath) &&
			artifactExists(logger, fingerprint, result.Kind, payload.DicomPath)
	case contracts.JobKindAnalyzeStudy:
		payload, ok := result.Payload.(contracts.AnalyzeStudyCommandResult)
		if !ok {
			warnPayloadTypeMismatch(logger, fingerprint, result.Kind)
			return false
		}

		return artifactExists(logger, fingerprint, result.Kind, payload.PreviewPath)
	default:
		if logger != nil {
			logger.Warn(
				"discarding unsupported memory cache entry kind",
				slog.String("fingerprint", fingerprint),
				slog.String("job_kind", string(result.Kind)),
			)
		}
		return false
	}
}

func warnPayloadTypeMismatch(
	logger *slog.Logger,
	fingerprint string,
	kind contracts.JobKind,
) {
	if logger == nil {
		return
	}

	logger.Warn(
		"discarding invalid memory cache entry payload",
		slog.String("fingerprint", fingerprint),
		slog.String("job_kind", string(kind)),
	)
}

func artifactExists(
	logger *slog.Logger,
	fingerprint string,
	kind contracts.JobKind,
	path string,
) bool {
	info, err := os.Stat(path)
	if err == nil {
		return !info.IsDir()
	}

	if logger != nil && !errors.Is(err, os.ErrNotExist) {
		logger.Warn(
			"discarding stale memory cache entry",
			slog.String("fingerprint", fingerprint),
			slog.String("job_kind", string(kind)),
			slog.String("artifact_path", path),
			slog.Any("error", err),
		)
	}

	return false
}

func cloneRenderResult(
	result contracts.RenderStudyCommandResult,
) contracts.RenderStudyCommandResult {
	result.MeasurementScale = cloneMeasurementScale(result.MeasurementScale)
	return result
}

func cloneProcessResult(
	result contracts.ProcessStudyCommandResult,
) contracts.ProcessStudyCommandResult {
	result.MeasurementScale = cloneMeasurementScale(result.MeasurementScale)
	return result
}

func cloneAnalyzeResult(
	result contracts.AnalyzeStudyCommandResult,
) contracts.AnalyzeStudyCommandResult {
	result.Analysis = cloneToothAnalysis(result.Analysis)
	result.SuggestedAnnotations = cloneAnnotationBundle(result.SuggestedAnnotations)
	return result
}

func cloneMeasurementScale(
	scale *contracts.MeasurementScale,
) *contracts.MeasurementScale {
	if scale == nil {
		return nil
	}

	value := *scale
	return &value
}

func cloneToothAnalysis(analysis contracts.ToothAnalysis) contracts.ToothAnalysis {
	analysis.Calibration.MeasurementScale = cloneMeasurementScale(analysis.Calibration.MeasurementScale)
	analysis.Tooth = cloneToothCandidatePointer(analysis.Tooth)
	analysis.Teeth = cloneToothCandidates(analysis.Teeth)
	analysis.Warnings = append([]string(nil), analysis.Warnings...)
	return analysis
}

func cloneToothCandidatePointer(
	candidate *contracts.ToothCandidate,
) *contracts.ToothCandidate {
	if candidate == nil {
		return nil
	}

	value := cloneToothCandidate(*candidate)
	return &value
}

func cloneToothCandidates(candidates []contracts.ToothCandidate) []contracts.ToothCandidate {
	if candidates == nil {
		return nil
	}

	cloned := make([]contracts.ToothCandidate, len(candidates))
	for index, candidate := range candidates {
		cloned[index] = cloneToothCandidate(candidate)
	}

	return cloned
}

func cloneToothCandidate(candidate contracts.ToothCandidate) contracts.ToothCandidate {
	candidate.Measurements.Calibrated = cloneToothMeasurementValues(candidate.Measurements.Calibrated)
	return candidate
}

func cloneToothMeasurementValues(
	values *contracts.ToothMeasurementValues,
) *contracts.ToothMeasurementValues {
	if values == nil {
		return nil
	}

	value := *values
	return &value
}

func cloneAnnotationBundle(bundle contracts.AnnotationBundle) contracts.AnnotationBundle {
	return contracts.AnnotationBundle{
		Lines:      cloneLineAnnotations(bundle.Lines),
		Rectangles: cloneRectangleAnnotations(bundle.Rectangles),
	}
}

func cloneLineAnnotations(lines []contracts.LineAnnotation) []contracts.LineAnnotation {
	if lines == nil {
		return nil
	}

	cloned := make([]contracts.LineAnnotation, len(lines))
	for index, line := range lines {
		cloned[index] = line
		cloned[index].Confidence = cloneFloat64Pointer(line.Confidence)
		cloned[index].Measurement = cloneLineMeasurement(line.Measurement)
	}

	return cloned
}

func cloneRectangleAnnotations(rectangles []contracts.RectangleAnnotation) []contracts.RectangleAnnotation {
	if rectangles == nil {
		return nil
	}

	cloned := make([]contracts.RectangleAnnotation, len(rectangles))
	for index, rectangle := range rectangles {
		cloned[index] = rectangle
		cloned[index].Confidence = cloneFloat64Pointer(rectangle.Confidence)
	}

	return cloned
}

func cloneLineMeasurement(measurement *contracts.LineMeasurement) *contracts.LineMeasurement {
	if measurement == nil {
		return nil
	}

	value := *measurement
	value.CalibratedLengthMM = cloneFloat64Pointer(measurement.CalibratedLengthMM)
	return &value
}

func cloneFloat64Pointer(value *float64) *float64 {
	if value == nil {
		return nil
	}

	cloned := *value
	return &cloned
}
