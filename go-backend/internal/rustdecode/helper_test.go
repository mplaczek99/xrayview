package rustdecode

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"
)

type stubRunner struct {
	stdout      []byte
	stderr      []byte
	err         error
	lastCommand []string
}

func (runner *stubRunner) Run(_ context.Context, command []string) ([]byte, []byte, error) {
	runner.lastCommand = append([]string(nil), command...)
	return runner.stdout, runner.stderr, runner.err
}

func TestCommandFromEnvironmentUsesExplicitBinary(t *testing.T) {
	t.Setenv(HelperBinaryEnv, "/tmp/custom-helper")

	if got, want := CommandFromEnvironment(), []string{"/tmp/custom-helper"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("CommandFromEnvironment() = %#v, want %#v", got, want)
	}
}

func TestDecodeStudyParsesPayloadAndAppendsInputFlag(t *testing.T) {
	runner := &stubRunner{
		stdout: []byte(`{
			"image": {
				"width": 2,
				"height": 2,
				"pixels": [0, 64, 128, 255],
				"minValue": 0,
				"maxValue": 255,
				"defaultWindow": {
					"center": 127.5,
					"width": 255
				},
				"invert": false
			},
			"metadata": {
				"studyInstanceUid": "1.2.3.4",
				"preservedElements": [
					{
						"tagGroup": 16,
						"tagElement": 16,
						"vr": "PN",
						"values": ["Test^Patient"]
					}
				]
			},
			"measurementScale": {
				"rowSpacingMm": 0.25,
				"columnSpacingMm": 0.40,
				"source": "PixelSpacing"
			}
		}`),
	}
	helper, err := newHelper([]string{"helper-bin", "--json"}, runner)
	if err != nil {
		t.Fatalf("newHelper returned error: %v", err)
	}

	study, err := helper.DecodeStudy(context.Background(), "/tmp/sample-study.dcm")
	if err != nil {
		t.Fatalf("DecodeStudy returned error: %v", err)
	}

	if got, want := runner.lastCommand, []string{"helper-bin", "--json", "--input", "/tmp/sample-study.dcm"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("command = %#v, want %#v", got, want)
	}

	if got, want := study.Image.Width, uint32(2); got != want {
		t.Fatalf("Image.Width = %d, want %d", got, want)
	}
	if got, want := study.Image.Height, uint32(2); got != want {
		t.Fatalf("Image.Height = %d, want %d", got, want)
	}
	if got, want := len(study.Image.Pixels), 4; got != want {
		t.Fatalf("len(Image.Pixels) = %d, want %d", got, want)
	}
	if study.Image.DefaultWindow == nil {
		t.Fatal("DefaultWindow = nil, want decoded window")
	}
	if got, want := study.Image.DefaultWindow.Center, float32(127.5); got != want {
		t.Fatalf("DefaultWindow.Center = %v, want %v", got, want)
	}
	if study.MeasurementScale == nil {
		t.Fatal("MeasurementScale = nil, want decoded measurement scale")
	}
	if got, want := study.Metadata.StudyInstanceUID, "1.2.3.4"; got != want {
		t.Fatalf("StudyInstanceUID = %q, want %q", got, want)
	}
	if got, want := study.Metadata.PreservedElements[0].VR, "PN"; got != want {
		t.Fatalf("PreservedElements[0].VR = %q, want %q", got, want)
	}
}

func TestDecodeStudyRejectsMismatchedPixelCount(t *testing.T) {
	runner := &stubRunner{
		stdout: []byte(`{
			"image": {
				"width": 2,
				"height": 2,
				"pixels": [0, 64, 128],
				"minValue": 0,
				"maxValue": 255,
				"invert": false
			},
			"metadata": {
				"studyInstanceUid": "1.2.3.4",
				"preservedElements": []
			}
		}`),
	}
	helper, err := newHelper([]string{"helper-bin"}, runner)
	if err != nil {
		t.Fatalf("newHelper returned error: %v", err)
	}

	_, err = helper.DecodeStudy(context.Background(), "/tmp/sample-study.dcm")
	if err == nil {
		t.Fatal("DecodeStudy returned nil error, want validation failure")
	}
}

func TestDecodeStudyReportsHelperFailure(t *testing.T) {
	runner := &stubRunner{
		stderr: []byte("error: decode failed"),
		err:    errors.New("exit status 1"),
	}
	helper, err := newHelper([]string{"helper-bin"}, runner)
	if err != nil {
		t.Fatalf("newHelper returned error: %v", err)
	}

	_, err = helper.DecodeStudy(context.Background(), "/tmp/sample-study.dcm")
	if err == nil || err.Error() != "run rust decode helper: exit status 1: error: decode failed" {
		t.Fatalf("DecodeStudy error = %v, want helper stderr included", err)
	}
}

func TestDecodeStudyWithRustHelperOnRepoSample(t *testing.T) {
	if existingDevBinaryPath() == "" {
		if _, err := exec.LookPath("cargo"); err != nil {
			t.Skip("cargo is not available and no prebuilt decode helper binary was found")
		}
	}

	helper, err := New(DefaultDevCommand())
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	study, err := helper.DecodeStudy(ctx, sampleDicomPath(t))
	if err != nil {
		t.Fatalf("DecodeStudy returned error: %v", err)
	}

	if got, want := study.Image.Width, uint32(2048); got != want {
		t.Fatalf("Image.Width = %d, want %d", got, want)
	}
	if got, want := study.Image.Height, uint32(1088); got != want {
		t.Fatalf("Image.Height = %d, want %d", got, want)
	}
	if got, want := len(study.Image.Pixels), 2048*1088; got != want {
		t.Fatalf("len(Image.Pixels) = %d, want %d", got, want)
	}
	if study.Image.DefaultWindow == nil {
		t.Fatal("DefaultWindow = nil, want sample window defaults")
	}
	if got, want := study.Image.DefaultWindow.Center, float32(127.5); got != want {
		t.Fatalf("DefaultWindow.Center = %v, want %v", got, want)
	}
	if study.Image.Invert {
		t.Fatal("Image.Invert = true, want false for sample fixture")
	}
	if study.MeasurementScale != nil {
		t.Fatalf("MeasurementScale = %+v, want nil for sample fixture", study.MeasurementScale)
	}
	if study.Metadata.StudyInstanceUID == "" {
		t.Fatal("StudyInstanceUID = empty, want helper metadata")
	}
}

func sampleDicomPath(t *testing.T) string {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller returned no file path")
	}

	return filepath.Clean(
		filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "images", "sample-dental-radiograph.dcm"),
	)
}
