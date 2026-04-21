package cache

import (
	"errors"
	"log/slog"
	"os"
	"sync"
	"time"

	"xrayview/backend/internal/contracts"
	"xrayview/backend/internal/imaging"
)

const (
	maxMemoryCacheEntries          = 32
	maxSourcePreviewEntries        = 32
	maxSourcePreviewBytes   uint64 = 64 * 1024 * 1024 // 64 MB

	// artifactCheckTTL gates how often os.Stat is called per cache entry.
	// Artifact files are written once by a job and are stable for the session;
	// the only way they disappear is via EvictArtifactsOverLimit, which widens
	// the TOCTOU window from nanoseconds to one TTL period (acceptable trade-off).
	artifactCheckTTL = 60 * time.Second
)

type cachedScale struct {
	scale *contracts.MeasurementScale
}

// resultEntry is a node in the result LRU doubly-linked list.
type resultEntry struct {
	fingerprint   string
	result        contracts.JobResult
	lastCheckedAt time.Time
	prev          *resultEntry
	next          *resultEntry
}

// sourcePreviewEntry is a node in the source preview LRU doubly-linked list.
type sourcePreviewEntry struct {
	inputPath string
	preview   imaging.PreviewImage
	prev      *sourcePreviewEntry
	next      *sourcePreviewEntry
}

type Memory struct {
	mu                 sync.RWMutex
	logger             *slog.Logger
	entries            map[string]*resultEntry
	resultHead         *resultEntry
	resultTail         *resultEntry
	sourcePreviews     map[string]*sourcePreviewEntry
	sourcePreviewBytes uint64
	previewHead        *sourcePreviewEntry
	previewTail        *sourcePreviewEntry
	measurementScales  map[string]cachedScale
}

func NewMemory(logger *slog.Logger) *Memory {
	return &Memory{
		logger:            logger,
		entries:           make(map[string]*resultEntry),
		sourcePreviews:    make(map[string]*sourcePreviewEntry),
		measurementScales: make(map[string]cachedScale),
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

// StoreSourcePreview stores a preview in the cache. The cache takes ownership
// of preview.Pixels — callers must not mutate the slice after calling Store.
// LoadSourcePreview returns a defensive clone, so readers are always safe.
func (memory *Memory) StoreSourcePreview(inputPath string, preview imaging.PreviewImage) {
	memory.mu.Lock()
	defer memory.mu.Unlock()

	if existing, ok := memory.sourcePreviews[inputPath]; ok {
		memory.sourcePreviewBytes -= existing.preview.ByteSize()
		existing.preview = preview
		memory.sourcePreviewBytes += preview.ByteSize()
		memory.movePreviewToFrontLocked(existing)
	} else {
		entry := &sourcePreviewEntry{inputPath: inputPath, preview: preview}
		memory.sourcePreviews[inputPath] = entry
		memory.pushPreviewFrontLocked(entry)
		memory.sourcePreviewBytes += preview.ByteSize()
	}
	memory.evictSourcePreviewLocked()
}

func (memory *Memory) LoadSourcePreview(inputPath string) (imaging.PreviewImage, bool) {
	// Fast path: concurrent misses return immediately without blocking writers.
	memory.mu.RLock()
	_, ok := memory.sourcePreviews[inputPath]
	memory.mu.RUnlock()
	if !ok {
		return imaging.PreviewImage{}, false
	}

	// Hit path: LRU promotion and pixel clone mutate shared state.
	memory.mu.Lock()
	defer memory.mu.Unlock()

	// Re-check: entry may have been evicted between RUnlock and Lock.
	entry, ok := memory.sourcePreviews[inputPath]
	if !ok {
		return imaging.PreviewImage{}, false
	}

	memory.movePreviewToFrontLocked(entry)
	return clonePreviewImage(entry.preview), true
}

func (memory *Memory) StoreMeasurementScale(inputPath string, scale *contracts.MeasurementScale) {
	memory.mu.Lock()
	defer memory.mu.Unlock()

	memory.measurementScales[inputPath] = cachedScale{scale: cloneMeasurementScale(scale)}
}

func (memory *Memory) LoadMeasurementScale(inputPath string) (*contracts.MeasurementScale, bool) {
	memory.mu.RLock()
	defer memory.mu.RUnlock()

	cached, ok := memory.measurementScales[inputPath]
	if !ok {
		return nil, false
	}

	return cloneMeasurementScale(cached.scale), true
}

func (memory *Memory) storeLocked(fingerprint string, result contracts.JobResult) {
	memory.mu.Lock()
	defer memory.mu.Unlock()

	now := time.Now()
	if existing, ok := memory.entries[fingerprint]; ok {
		existing.result = result
		existing.lastCheckedAt = now
		memory.moveResultToFrontLocked(existing)
	} else {
		entry := &resultEntry{fingerprint: fingerprint, result: result, lastCheckedAt: now}
		memory.entries[fingerprint] = entry
		memory.pushResultFrontLocked(entry)
	}
	memory.evictResultLocked()
}

func (memory *Memory) loadLocked(
	fingerprint string,
	expectedKind contracts.JobKind,
) (contracts.JobResult, bool) {
	// Fast path: concurrent misses return immediately without blocking writers.
	memory.mu.RLock()
	_, ok := memory.entries[fingerprint]
	memory.mu.RUnlock()
	if !ok {
		return contracts.JobResult{}, false
	}

	// Hit path: artifact validation and LRU promotion mutate shared state.
	memory.mu.Lock()
	defer memory.mu.Unlock()

	// Re-check: entry may have been evicted between RUnlock and Lock.
	entry, ok := memory.entries[fingerprint]
	if !ok {
		return contracts.JobResult{}, false
	}

	if time.Since(entry.lastCheckedAt) >= artifactCheckTTL {
		if !resultArtifactsExist(entry.result, memory.logger, fingerprint) {
			memory.removeResultEntryLocked(entry)
			delete(memory.entries, fingerprint)
			return contracts.JobResult{}, false
		}
		entry.lastCheckedAt = time.Now()
	}

	if entry.result.Kind != expectedKind {
		memory.warnInvalidEntry(
			fingerprint,
			entry.result.Kind,
			"memory cache entry kind mismatch",
			slog.String("expected_kind", string(expectedKind)),
		)
		memory.removeResultEntryLocked(entry)
		delete(memory.entries, fingerprint)
		return contracts.JobResult{}, false
	}

	memory.moveResultToFrontLocked(entry)
	return entry.result, true
}

func (memory *Memory) discardInvalidEntry(
	fingerprint string,
	kind contracts.JobKind,
	message string,
) {
	memory.mu.Lock()
	defer memory.mu.Unlock()

	memory.warnInvalidEntry(fingerprint, kind, message)
	if entry, ok := memory.entries[fingerprint]; ok {
		memory.removeResultEntryLocked(entry)
		delete(memory.entries, fingerprint)
	}
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

// evictResultLocked removes tail entries until len(entries) <= maxMemoryCacheEntries.
// Must be called with mu held. The newly inserted entry is always at the front,
// so it is never the eviction victim under normal capacity conditions.
func (memory *Memory) evictResultLocked() {
	for len(memory.entries) > maxMemoryCacheEntries && memory.resultTail != nil {
		victim := memory.resultTail
		memory.removeResultEntryLocked(victim)
		delete(memory.entries, victim.fingerprint)
	}
}

// evictSourcePreviewLocked removes tail entries until the count and byte budget
// are within limits. Stops early when only one entry remains to avoid evicting
// a single oversized entry that was just inserted.
func (memory *Memory) evictSourcePreviewLocked() {
	for len(memory.sourcePreviews) > 1 &&
		(len(memory.sourcePreviews) > maxSourcePreviewEntries || memory.sourcePreviewBytes > maxSourcePreviewBytes) &&
		memory.previewTail != nil {
		victim := memory.previewTail
		memory.sourcePreviewBytes -= victim.preview.ByteSize()
		memory.removePreviewEntryLocked(victim)
		delete(memory.sourcePreviews, victim.inputPath)
		delete(memory.measurementScales, victim.inputPath)
	}
}

// --- Result LRU list operations (mu must be held) ---

func (memory *Memory) pushResultFrontLocked(entry *resultEntry) {
	entry.prev = nil
	entry.next = memory.resultHead
	if memory.resultHead != nil {
		memory.resultHead.prev = entry
	}
	memory.resultHead = entry
	if memory.resultTail == nil {
		memory.resultTail = entry
	}
}

func (memory *Memory) moveResultToFrontLocked(entry *resultEntry) {
	if memory.resultHead == entry {
		return
	}
	memory.removeResultEntryLocked(entry)
	memory.pushResultFrontLocked(entry)
}

func (memory *Memory) removeResultEntryLocked(entry *resultEntry) {
	if entry.prev != nil {
		entry.prev.next = entry.next
	} else {
		memory.resultHead = entry.next
	}
	if entry.next != nil {
		entry.next.prev = entry.prev
	} else {
		memory.resultTail = entry.prev
	}
	entry.prev = nil
	entry.next = nil
}

// --- Source preview LRU list operations (mu must be held) ---

func (memory *Memory) pushPreviewFrontLocked(entry *sourcePreviewEntry) {
	entry.prev = nil
	entry.next = memory.previewHead
	if memory.previewHead != nil {
		memory.previewHead.prev = entry
	}
	memory.previewHead = entry
	if memory.previewTail == nil {
		memory.previewTail = entry
	}
}

func (memory *Memory) movePreviewToFrontLocked(entry *sourcePreviewEntry) {
	if memory.previewHead == entry {
		return
	}
	memory.removePreviewEntryLocked(entry)
	memory.pushPreviewFrontLocked(entry)
}

func (memory *Memory) removePreviewEntryLocked(entry *sourcePreviewEntry) {
	if entry.prev != nil {
		entry.prev.next = entry.next
	} else {
		memory.previewHead = entry.next
	}
	if entry.next != nil {
		entry.next.prev = entry.prev
	} else {
		memory.previewTail = entry.prev
	}
	entry.prev = nil
	entry.next = nil
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
	candidate.Geometry.Outline = clonePointSlice(candidate.Geometry.Outline)
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
		Polylines:  clonePolylineAnnotations(bundle.Polylines),
	}
}

func clonePreviewImage(preview imaging.PreviewImage) imaging.PreviewImage {
	preview.Pixels = append([]uint8(nil), preview.Pixels...)
	return preview
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

func clonePolylineAnnotations(polylines []contracts.PolylineAnnotation) []contracts.PolylineAnnotation {
	if polylines == nil {
		return nil
	}

	cloned := make([]contracts.PolylineAnnotation, len(polylines))
	for index, polyline := range polylines {
		cloned[index] = polyline
		cloned[index].Points = append([]contracts.AnnotationPoint(nil), polyline.Points...)
		cloned[index].Confidence = cloneFloat64Pointer(polyline.Confidence)
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

func clonePointSlice(points []contracts.Point) []contracts.Point {
	if points == nil {
		return nil
	}

	cloned := make([]contracts.Point, len(points))
	copy(cloned, points)
	return cloned
}
