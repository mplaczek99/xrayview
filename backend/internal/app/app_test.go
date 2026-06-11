package app

import (
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xrayview/backend/internal/config"
	"xrayview/backend/internal/contracts"
	"xrayview/backend/internal/persistence"
)

func TestOpenStudyRegistersExistingFile(t *testing.T) {
	root := t.TempDir()
	inputPath := filepath.Join(root, "sample.bmp")
	writeBMP(t, inputPath)
	app := newTestApp(t, root)

	result, err := app.OpenStudy(contracts.OpenStudyCommand{InputPath: inputPath})
	if err != nil {
		t.Fatal(err)
	}

	if result.Study.StudyID == "" {
		t.Fatal("study id is empty")
	}
	if result.Study.InputName != "sample.bmp" {
		t.Fatalf("input name = %q", result.Study.InputName)
	}
	if result.Study.MeasurementScale != nil {
		t.Fatalf("measurement scale = %+v, want nil", result.Study.MeasurementScale)
	}
	if app.StudyCount() != 1 {
		t.Fatalf("study count = %d, want 1", app.StudyCount())
	}
}

func TestOpenStudyRejectsBlankInputPath(t *testing.T) {
	app := newTestApp(t, t.TempDir())

	_, err := app.OpenStudy(contracts.OpenStudyCommand{InputPath: "  "})
	if err == nil {
		t.Fatal("expected error")
	}
	var backendErr contracts.BackendError
	if !errors.As(err, &backendErr) || backendErr.Code != contracts.InvalidInput || backendErr.Message != "inputPath is required" {
		t.Fatalf("error = %#v", err)
	}
}

func TestOpenStudyRecordsRecentStudyCatalog(t *testing.T) {
	root := t.TempDir()
	inputPath := filepath.Join(root, "sample.bmp")
	writeBMP(t, inputPath)
	app := newTestApp(t, root)

	opened, err := app.OpenStudy(contracts.OpenStudyCommand{InputPath: inputPath})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := persistence.NewCatalog(filepath.Join(root, "state")).Load()
	if err != nil {
		t.Fatal(err)
	}

	if len(catalog.RecentStudies) != 1 {
		t.Fatalf("recent count = %d, want 1", len(catalog.RecentStudies))
	}
	if catalog.RecentStudies[0].InputPath != opened.Study.InputPath || catalog.RecentStudies[0].InputName != "sample.bmp" {
		t.Fatalf("recent entry = %+v", catalog.RecentStudies[0])
	}
}

func TestMeasureLineAnnotationUsesRegisteredMeasurementScale(t *testing.T) {
	app := newTestApp(t, t.TempDir())
	study, err := app.RegisterStudy("/tmp/calibrated-measurement.bmp", &contracts.MeasurementScale{
		RowSpacingMM:    0.2,
		ColumnSpacingMM: 0.3,
		Source:          "manualCalibration",
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := app.MeasureLineAnnotation(contracts.MeasureLineAnnotationCommand{
		StudyID: study.StudyID,
		Annotation: contracts.LineAnnotation{
			ID:       "line-1",
			Label:    "Measurement 1",
			Source:   contracts.AnnotationManual,
			Start:    contracts.AnnotationPoint{X: 10, Y: 8},
			End:      contracts.AnnotationPoint{X: 14, Y: 11},
			Editable: true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	measurement := result.Annotation.Measurement
	if measurement == nil {
		t.Fatal("measurement missing")
	}
	if measurement.PixelLength != 5.0 || measurement.CalibratedLengthMM == nil || *measurement.CalibratedLengthMM != 1.3 {
		t.Fatalf("measurement = %+v", measurement)
	}
}

func TestMeasureLineAnnotationRejectsNonFinitePoints(t *testing.T) {
	app := newTestApp(t, t.TempDir())
	study, err := app.RegisterStudy("/tmp/measurement.bmp", nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = app.MeasureLineAnnotation(contracts.MeasureLineAnnotationCommand{
		StudyID: study.StudyID,
		Annotation: contracts.LineAnnotation{
			ID:       "line-1",
			Label:    "Measurement 1",
			Source:   contracts.AnnotationManual,
			Start:    contracts.AnnotationPoint{X: math.NaN(), Y: 8},
			End:      contracts.AnnotationPoint{X: 14, Y: 11},
			Editable: true,
		},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var backendErr contracts.BackendError
	if !errors.As(err, &backendErr) || backendErr.Code != contracts.InvalidInput || !strings.Contains(backendErr.Message, "finite numbers") {
		t.Fatalf("error = %#v", err)
	}
}

// set_study_calibration derives an isotropic scale from a known-length segment,
// and a subsequent measure picks it up. A 3-4-5 segment is 5 px; declaring it
// 10 mm long gives 2.0 mm/px, so a 5 px line measures 10 mm.
func TestSetStudyCalibrationDerivesScaleUsedByMeasurement(t *testing.T) {
	app := newTestApp(t, t.TempDir())
	study, err := app.RegisterStudy("/tmp/calibrate.bmp", nil)
	if err != nil {
		t.Fatal(err)
	}

	result, err := app.SetStudyCalibration(contracts.SetStudyCalibrationCommand{
		StudyID: study.StudyID,
		Reference: &contracts.CalibrationReference{
			Start:         contracts.AnnotationPoint{X: 0, Y: 0},
			End:           contracts.AnnotationPoint{X: 3, Y: 4},
			KnownLengthMM: 10,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	scale := result.Study.MeasurementScale
	if scale == nil || scale.RowSpacingMM != 2.0 || scale.ColumnSpacingMM != 2.0 || scale.Source != "manualCalibration" {
		t.Fatalf("scale = %+v", scale)
	}

	measured, err := app.MeasureLineAnnotation(contracts.MeasureLineAnnotationCommand{
		StudyID: study.StudyID,
		Annotation: contracts.LineAnnotation{
			ID:       "line-1",
			Label:    "Measurement 1",
			Source:   contracts.AnnotationManual,
			Start:    contracts.AnnotationPoint{X: 0, Y: 0},
			End:      contracts.AnnotationPoint{X: 0, Y: 5},
			Editable: true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	measurement := measured.Annotation.Measurement
	if measurement == nil || measurement.CalibratedLengthMM == nil || *measurement.CalibratedLengthMM != 10.0 {
		t.Fatalf("measurement = %+v", measurement)
	}
}

// A nil reference clears a previously-set calibration.
func TestSetStudyCalibrationClearsExistingScale(t *testing.T) {
	app := newTestApp(t, t.TempDir())
	study, err := app.RegisterStudy("/tmp/clear-calibration.bmp", &contracts.MeasurementScale{
		RowSpacingMM:    0.2,
		ColumnSpacingMM: 0.2,
		Source:          "manualCalibration",
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := app.SetStudyCalibration(contracts.SetStudyCalibrationCommand{
		StudyID:   study.StudyID,
		Reference: nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Study.MeasurementScale != nil {
		t.Fatalf("expected cleared scale, got %+v", result.Study.MeasurementScale)
	}
}

func TestSetStudyCalibrationRejectsZeroLengthReference(t *testing.T) {
	app := newTestApp(t, t.TempDir())
	study, err := app.RegisterStudy("/tmp/zero-length.bmp", nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = app.SetStudyCalibration(contracts.SetStudyCalibrationCommand{
		StudyID: study.StudyID,
		Reference: &contracts.CalibrationReference{
			Start:         contracts.AnnotationPoint{X: 7, Y: 7},
			End:           contracts.AnnotationPoint{X: 7, Y: 7},
			KnownLengthMM: 10,
		},
	})
	var backendErr contracts.BackendError
	if !errors.As(err, &backendErr) || backendErr.Code != contracts.InvalidInput || !strings.Contains(backendErr.Message, "non-zero pixel length") {
		t.Fatalf("error = %#v", err)
	}
}

func TestSetStudyCalibrationRejectsNonPositiveLength(t *testing.T) {
	app := newTestApp(t, t.TempDir())
	study, err := app.RegisterStudy("/tmp/bad-length.bmp", nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = app.SetStudyCalibration(contracts.SetStudyCalibrationCommand{
		StudyID: study.StudyID,
		Reference: &contracts.CalibrationReference{
			Start:         contracts.AnnotationPoint{X: 0, Y: 0},
			End:           contracts.AnnotationPoint{X: 3, Y: 4},
			KnownLengthMM: 0,
		},
	})
	var backendErr contracts.BackendError
	if !errors.As(err, &backendErr) || backendErr.Code != contracts.InvalidInput || !strings.Contains(backendErr.Message, "positive number") {
		t.Fatalf("error = %#v", err)
	}
}

func TestSetStudyCalibrationRejectsUnknownStudy(t *testing.T) {
	app := newTestApp(t, t.TempDir())

	_, err := app.SetStudyCalibration(contracts.SetStudyCalibrationCommand{
		StudyID: "study-missing",
		Reference: &contracts.CalibrationReference{
			Start:         contracts.AnnotationPoint{X: 0, Y: 0},
			End:           contracts.AnnotationPoint{X: 3, Y: 4},
			KnownLengthMM: 10,
		},
	})
	var backendErr contracts.BackendError
	if !errors.As(err, &backendErr) || backendErr.Code != contracts.NotFound {
		t.Fatalf("error = %#v", err)
	}
}

func TestStartRenderJobWritesPreviewAndStoresCompletedSnapshot(t *testing.T) {
	root := t.TempDir()
	inputPath := filepath.Join(root, "sample.bmp")
	writeBMP(t, inputPath)
	app := newTestApp(t, root)
	study := openStudy(t, app, inputPath)

	started, err := app.StartRenderJob(contracts.RenderStudyCommand{StudyID: study.StudyID})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := app.GetJob(contracts.JobCommand{JobID: started.JobID})
	if err != nil {
		t.Fatal(err)
	}

	if snapshot.State != contracts.JobStateCompleted || snapshot.JobKind != contracts.JobKindRenderStudy || snapshot.Result == nil {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	payload := snapshot.Result.Payload.(contracts.RenderStudyCommandResult)
	if payload.LoadedWidth != 4 || payload.LoadedHeight != 2 {
		t.Fatalf("payload = %+v", payload)
	}
	if bytes, err := os.ReadFile(payload.PreviewPath); err != nil || !strings.HasPrefix(string(bytes), "BM") {
		t.Fatalf("preview output invalid: %v", err)
	}
}

func TestStartRenderJobReusesInSessionCachedResultForSameInput(t *testing.T) {
	root := t.TempDir()
	inputPath := filepath.Join(root, "sample.bmp")
	writeBMP(t, inputPath)
	app := newTestApp(t, root)
	study := openStudy(t, app, inputPath)

	first, err := app.StartRenderJob(contracts.RenderStudyCommand{StudyID: study.StudyID})
	if err != nil {
		t.Fatal(err)
	}
	second, err := app.StartRenderJob(contracts.RenderStudyCommand{StudyID: study.StudyID})
	if err != nil {
		t.Fatal(err)
	}
	firstSnapshot, _ := app.GetJob(contracts.JobCommand{JobID: first.JobID})
	secondSnapshot, _ := app.GetJob(contracts.JobCommand{JobID: second.JobID})

	if firstSnapshot.FromCache {
		t.Fatal("first render unexpectedly from cache")
	}
	if !secondSnapshot.FromCache {
		t.Fatal("second render was not from cache")
	}
}

func TestStartRenderJobAsyncPublishesContractCompatibleStageUpdates(t *testing.T) {
	root := t.TempDir()
	inputPath := filepath.Join(root, "sample.bmp")
	writeBMP(t, inputPath)
	app := newTestApp(t, root)
	study := openStudy(t, app, inputPath)
	subscription := app.SubscribeJobUpdates()
	defer app.UnsubscribeJobUpdates(subscription.ID)

	started, err := app.StartRenderJobAsync(contracts.RenderStudyCommand{StudyID: study.StudyID})
	if err != nil {
		t.Fatal(err)
	}
	completed := waitForJobState(t, app, started.JobID, contracts.JobStateCompleted)

	if completed.JobKind != contracts.JobKindRenderStudy || completed.Result == nil {
		t.Fatalf("completed snapshot = %+v", completed)
	}
	seenQueued := false
	seenRunning := false
	deadline := time.After(200 * time.Millisecond)
	for !(seenQueued && seenRunning) {
		select {
		case update := <-subscription.Receiver:
			if update.JobID != started.JobID {
				continue
			}
			if update.State == contracts.JobStateQueued {
				seenQueued = true
			}
			if update.State == contracts.JobStateRunning {
				seenRunning = true
			}
		case <-deadline:
			t.Fatalf("missing expected updates queued=%v running=%v", seenQueued, seenRunning)
		}
	}
}

func TestStartRenderJobAsyncBroadcastsCachedCompletedJobUpdate(t *testing.T) {
	root := t.TempDir()
	inputPath := filepath.Join(root, "sample.bmp")
	writeBMP(t, inputPath)
	app := newTestApp(t, root)
	study := openStudy(t, app, inputPath)
	if _, err := app.StartRenderJob(contracts.RenderStudyCommand{StudyID: study.StudyID}); err != nil {
		t.Fatal(err)
	}
	subscription := app.SubscribeJobUpdates()
	defer app.UnsubscribeJobUpdates(subscription.ID)

	started, err := app.StartRenderJobAsync(contracts.RenderStudyCommand{StudyID: study.StudyID})
	if err != nil {
		t.Fatal(err)
	}
	update := waitForSubscriptionUpdate(t, subscription, started.JobID, contracts.JobStateCompleted)
	if !update.FromCache {
		t.Fatalf("cached update FromCache = false: %+v", update)
	}
}

func TestStartProcessJobWritesPreviewSnapshot(t *testing.T) {
	root := t.TempDir()
	inputPath := filepath.Join(root, "sample.bmp")
	writeBMP(t, inputPath)
	app := newTestApp(t, root)
	study := openStudy(t, app, inputPath)
	palette := contracts.PaletteHot

	started, err := app.StartProcessJob(contracts.ProcessStudyCommand{
		StudyID:  study.StudyID,
		PresetID: "default",
		Invert:   true,
		Palette:  &palette,
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := app.GetJob(contracts.JobCommand{JobID: started.JobID})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.State != contracts.JobStateCompleted || snapshot.JobKind != contracts.JobKindProcessStudy || snapshot.Result == nil {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	payload := snapshot.Result.Payload.(contracts.ProcessStudyCommandResult)
	if !strings.Contains(payload.Mode, "inverted grayscale") || !strings.Contains(payload.Mode, "hot palette") {
		t.Fatalf("mode = %q", payload.Mode)
	}
	if bytes, err := os.ReadFile(payload.PreviewPath); err != nil || !strings.HasPrefix(string(bytes), "BM") {
		t.Fatalf("preview output invalid: %v", err)
	}
}

func TestStartProcessJobAsyncRejectsInvalidCommandBeforeQueueing(t *testing.T) {
	root := t.TempDir()
	inputPath := filepath.Join(root, "sample.bmp")
	writeBMP(t, inputPath)
	app := newTestApp(t, root)
	study := openStudy(t, app, inputPath)
	contrast := 0.0

	_, err := app.StartProcessJobAsync(contracts.ProcessStudyCommand{
		StudyID:  study.StudyID,
		PresetID: "default",
		Contrast: &contrast,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var backendErr contracts.BackendError
	if !errors.As(err, &backendErr) || backendErr.Code != contracts.InvalidInput {
		t.Fatalf("error = %#v", err)
	}
	snapshots, err := app.GetJobs(contracts.GetJobsCommand{JobIDs: []string{"job-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 0 {
		t.Fatalf("unexpected queued jobs: %+v", snapshots)
	}
}

func TestStartProcessJobAsyncCompletesAndWritesPreview(t *testing.T) {
	root := t.TempDir()
	inputPath := filepath.Join(root, "sample.bmp")
	writeBMP(t, inputPath)
	app := newTestApp(t, root)
	study := openStudy(t, app, inputPath)
	palette := contracts.PaletteBone

	started, err := app.StartProcessJobAsync(contracts.ProcessStudyCommand{
		StudyID:  study.StudyID,
		PresetID: "default",
		Palette:  &palette,
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := waitForJobState(t, app, started.JobID, contracts.JobStateCompleted)
	payload := snapshot.Result.Payload.(contracts.ProcessStudyCommandResult)
	if payload.PreviewPath == "" || !fileExists(payload.PreviewPath) {
		t.Fatalf("preview path missing: %+v", payload)
	}
	if !strings.Contains(payload.Mode, "bone palette") {
		t.Fatalf("mode = %q", payload.Mode)
	}
}

func TestGetJobsSkipsBlankAndUnknownIDs(t *testing.T) {
	root := t.TempDir()
	inputPath := filepath.Join(root, "sample.bmp")
	writeBMP(t, inputPath)
	app := newTestApp(t, root)
	study := openStudy(t, app, inputPath)
	started, err := app.StartRenderJob(contracts.RenderStudyCommand{StudyID: study.StudyID})
	if err != nil {
		t.Fatal(err)
	}

	snapshots, err := app.GetJobs(contracts.GetJobsCommand{JobIDs: []string{"", "missing", started.JobID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 1 || snapshots[0].JobID != started.JobID {
		t.Fatalf("snapshots = %+v", snapshots)
	}
}

func TestStartAnalyzeJobWritesOverlayPreviews(t *testing.T) {
	root := t.TempDir()
	inputPath := filepath.Join(root, "analysis.bmp")
	writeAnalysisBMP(t, inputPath)
	app := newTestApp(t, root)
	study := openStudy(t, app, inputPath)

	started, err := app.StartAnalyzeJob(contracts.AnalyzeStudyCommand{StudyID: study.StudyID})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := app.GetJob(contracts.JobCommand{JobID: started.JobID})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.State != contracts.JobStateCompleted || snapshot.JobKind != contracts.JobKindAnalyzeStudy || snapshot.Result == nil {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	payload := snapshot.Result.Payload.(contracts.AnalyzeStudyCommandResult)
	if payload.LoadedWidth != 20 || payload.LoadedHeight != 20 {
		t.Fatalf("payload = %+v", payload)
	}
	if !strings.HasPrefix(payload.Mode, "dynamic tooth and bone level overlay") {
		t.Fatalf("mode = %q", payload.Mode)
	}
	if bytes, err := os.ReadFile(payload.PreviewPath); err != nil || !strings.HasPrefix(string(bytes), "BM") {
		t.Fatalf("preview output invalid: %v", err)
	}
	if bytes, err := os.ReadFile(payload.FilledPreviewPath); err != nil || !strings.HasPrefix(string(bytes), "BM") {
		t.Fatalf("filled output invalid: %v", err)
	}
}

func TestStartAnalyzeJobReusesInSessionCachedResult(t *testing.T) {
	root := t.TempDir()
	inputPath := filepath.Join(root, "analysis.bmp")
	writeAnalysisBMP(t, inputPath)
	app := newTestApp(t, root)
	study := openStudy(t, app, inputPath)

	first, err := app.StartAnalyzeJob(contracts.AnalyzeStudyCommand{StudyID: study.StudyID})
	if err != nil {
		t.Fatal(err)
	}
	second, err := app.StartAnalyzeJob(contracts.AnalyzeStudyCommand{StudyID: study.StudyID})
	if err != nil {
		t.Fatal(err)
	}
	firstSnapshot, _ := app.GetJob(contracts.JobCommand{JobID: first.JobID})
	secondSnapshot, _ := app.GetJob(contracts.JobCommand{JobID: second.JobID})
	if firstSnapshot.FromCache {
		t.Fatal("first analyze unexpectedly from cache")
	}
	if !secondSnapshot.FromCache {
		t.Fatal("second analyze was not from cache")
	}
}

func newTestApp(t *testing.T, root string) *App {
	t.Helper()
	cfg := config.Default()
	cfg.Paths.BaseDir = root
	cfg.Paths.CacheDir = filepath.Join(root, "cache")
	cfg.Paths.PersistenceDir = filepath.Join(root, "state")
	app, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func openStudy(t *testing.T, app *App, inputPath string) contracts.StudyRecord {
	t.Helper()
	result, err := app.OpenStudy(contracts.OpenStudyCommand{InputPath: inputPath})
	if err != nil {
		t.Fatal(err)
	}
	return result.Study
}

func writeBMP(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, buildBMP32(4, 2, grayscaleRamp()), 0o666); err != nil {
		t.Fatal(err)
	}
}

func writeAnalysisBMP(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, buildBMP32(20, 20, analysisFixturePixels()), 0o666); err != nil {
		t.Fatal(err)
	}
}

func waitForJobState(t *testing.T, app *App, jobID string, state contracts.JobState) contracts.JobSnapshot {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err := app.GetJob(contracts.JobCommand{JobID: jobID})
		if err == nil && snapshot.State == state {
			return snapshot
		}
		time.Sleep(5 * time.Millisecond)
	}
	snapshot, _ := app.GetJob(contracts.JobCommand{JobID: jobID})
	t.Fatalf("timed out waiting for %s, last snapshot %+v", state, snapshot)
	return contracts.JobSnapshot{}
}

func waitForSubscriptionUpdate(t *testing.T, subscription JobUpdateSubscription, jobID string, state contracts.JobState) contracts.JobSnapshot {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case update := <-subscription.Receiver:
			if update.JobID == jobID && update.State == state {
				return update
			}
		case <-deadline:
			t.Fatalf("timed out waiting for subscription update %s", state)
		}
	}
}

func fileExists(path string) bool {
	metadata, err := os.Stat(path)
	return err == nil && metadata.Mode().IsRegular()
}

type rgb struct {
	red   byte
	green byte
	blue  byte
}

func grayscaleRamp() []rgb {
	return []rgb{
		{0, 0, 0},
		{36, 36, 36},
		{72, 72, 72},
		{108, 108, 108},
		{144, 144, 144},
		{180, 180, 180},
		{216, 216, 216},
		{255, 255, 255},
	}
}

func analysisFixturePixels() []rgb {
	pixels := make([]rgb, 20*20)
	for y := 0; y < 20; y++ {
		for x := 0; x < 20; x++ {
			value := byte(24)
			if x >= 5 && x < 15 && y >= 4 && y < 14 {
				value = 220
			}
			if y == 15 {
				value = 80
			}
			pixels[y*20+x] = rgb{value, value, value}
		}
	}
	return pixels
}

func buildBMP32(width, height uint32, pixels []rgb) []byte {
	rowStride := int(width) * 4
	pixelBytes := rowStride * int(height)
	fileSize := 54 + pixelBytes
	bmp := make([]byte, 0, fileSize)
	bmp = append(bmp, 'B', 'M')
	bmp = binary.LittleEndian.AppendUint32(bmp, uint32(fileSize))
	bmp = append(bmp, 0, 0, 0, 0)
	bmp = binary.LittleEndian.AppendUint32(bmp, 54)
	bmp = binary.LittleEndian.AppendUint32(bmp, 40)
	bmp = binary.LittleEndian.AppendUint32(bmp, width)
	bmp = binary.LittleEndian.AppendUint32(bmp, height)
	bmp = binary.LittleEndian.AppendUint16(bmp, 1)
	bmp = binary.LittleEndian.AppendUint16(bmp, 32)
	bmp = binary.LittleEndian.AppendUint32(bmp, 0)
	bmp = binary.LittleEndian.AppendUint32(bmp, uint32(pixelBytes))
	bmp = binary.LittleEndian.AppendUint32(bmp, 0)
	bmp = binary.LittleEndian.AppendUint32(bmp, 0)
	bmp = binary.LittleEndian.AppendUint32(bmp, 0)
	bmp = binary.LittleEndian.AppendUint32(bmp, 0)
	for outputY := int(height) - 1; outputY >= 0; outputY-- {
		row := pixels[outputY*int(width) : (outputY+1)*int(width)]
		for _, pixel := range row {
			bmp = append(bmp, pixel.blue, pixel.green, pixel.red, 255)
		}
	}
	return bmp
}
