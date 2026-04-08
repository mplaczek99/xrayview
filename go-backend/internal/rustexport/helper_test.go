package rustexport

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"xrayview/go-backend/internal/dicommeta"
	"xrayview/go-backend/internal/imaging"
	"xrayview/go-backend/internal/rustdecode"
)

type stubRunner struct {
	stdout      []byte
	stderr      []byte
	err         error
	lastCommand []string
	lastStdin   []byte
}

func (runner *stubRunner) Run(
	_ context.Context,
	command []string,
	stdin []byte,
) ([]byte, []byte, error) {
	runner.lastCommand = append([]string(nil), command...)
	runner.lastStdin = append([]byte(nil), stdin...)
	return runner.stdout, runner.stderr, runner.err
}

func TestCommandFromEnvironmentUsesExplicitBinary(t *testing.T) {
	t.Setenv(HelperBinaryEnv, "/tmp/custom-export-helper")

	if got, want := CommandFromEnvironment(), []string{"/tmp/custom-export-helper"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("CommandFromEnvironment() = %#v, want %#v", got, want)
	}
}

func TestWriteSecondaryCaptureMarshalsRequestAndAppendsOutputFlag(t *testing.T) {
	runner := &stubRunner{}
	helper, err := newHelper([]string{"helper-bin", "--json"}, runner)
	if err != nil {
		t.Fatalf("newHelper returned error: %v", err)
	}

	err = helper.WriteSecondaryCapture(
		context.Background(),
		"/tmp/processed-output.dcm",
		imaging.GrayPreview(2, 2, []uint8{0, 64, 128, 255}),
		rustdecode.SourceMetadata{
			StudyInstanceUID: "1.2.3.4",
			PreservedElements: []rustdecode.PreservedElement{
				{
					TagGroup:   0x0010,
					TagElement: 0x0010,
					VR:         "PN",
					Values:     []string{"Test^Patient"},
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("WriteSecondaryCapture returned error: %v", err)
	}

	if got, want := runner.lastCommand, []string{"helper-bin", "--json", "--output", "/tmp/processed-output.dcm"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("command = %#v, want %#v", got, want)
	}

	var request struct {
		Preview struct {
			Width  uint32              `json:"width"`
			Height uint32              `json:"height"`
			Format imaging.ImageFormat `json:"format"`
			Pixels []int               `json:"pixels"`
		} `json:"preview"`
		Metadata rustdecode.SourceMetadata `json:"metadata"`
	}
	if err := json.Unmarshal(runner.lastStdin, &request); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if got, want := request.Preview.Format, imaging.FormatGray8; got != want {
		t.Fatalf("Preview.Format = %q, want %q", got, want)
	}
	if got, want := request.Preview.Pixels, []int{0, 64, 128, 255}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Preview.Pixels = %#v, want %#v", got, want)
	}
	if got, want := request.Metadata.StudyInstanceUID, "1.2.3.4"; got != want {
		t.Fatalf("Metadata.StudyInstanceUID = %q, want %q", got, want)
	}
	if got, want := request.Metadata.PreservedElements[0].VR, "PN"; got != want {
		t.Fatalf("Metadata.PreservedElements[0].VR = %q, want %q", got, want)
	}
}

func TestWriteSecondaryCaptureRejectsInvalidPreview(t *testing.T) {
	helper, err := newHelper([]string{"helper-bin"}, &stubRunner{})
	if err != nil {
		t.Fatalf("newHelper returned error: %v", err)
	}

	err = helper.WriteSecondaryCapture(
		context.Background(),
		"/tmp/processed-output.dcm",
		imaging.PreviewImage{Width: 2, Height: 2, Format: imaging.FormatGray8, Pixels: []uint8{0, 64, 128}},
		rustdecode.SourceMetadata{StudyInstanceUID: "1.2.3.4"},
	)
	if err == nil {
		t.Fatal("WriteSecondaryCapture returned nil error, want preview validation failure")
	}
}

func TestWriteSecondaryCaptureReportsHelperFailure(t *testing.T) {
	runner := &stubRunner{
		stderr: []byte("error: export failed"),
		err:    errors.New("exit status 1"),
	}
	helper, err := newHelper([]string{"helper-bin"}, runner)
	if err != nil {
		t.Fatalf("newHelper returned error: %v", err)
	}

	err = helper.WriteSecondaryCapture(
		context.Background(),
		"/tmp/processed-output.dcm",
		imaging.GrayPreview(1, 1, []uint8{128}),
		rustdecode.SourceMetadata{StudyInstanceUID: "1.2.3.4"},
	)
	if err == nil || err.Error() != "run rust export helper: exit status 1: error: export failed" {
		t.Fatalf("WriteSecondaryCapture error = %v, want helper stderr included", err)
	}
}

func TestWriteSecondaryCaptureWithRustHelperOnRepoSample(t *testing.T) {
	if existingDevBinaryPath() == "" {
		if _, err := exec.LookPath("cargo"); err != nil {
			t.Skip("cargo is not available and no prebuilt export helper binary was found")
		}
	}

	helper, err := New(DefaultDevCommand())
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	outputPath := filepath.Join(t.TempDir(), "secondary-capture.dcm")
	err = helper.WriteSecondaryCapture(
		ctx,
		outputPath,
		imaging.GrayPreview(2, 2, []uint8{0, 64, 128, 255}),
		rustdecode.SourceMetadata{
			StudyInstanceUID: "1.2.3.4.5",
			PreservedElements: []rustdecode.PreservedElement{
				{
					TagGroup:   0x0028,
					TagElement: 0x0030,
					VR:         "DS",
					Values:     []string{"0.20", "0.30"},
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("WriteSecondaryCapture returned error: %v", err)
	}

	metadata, err := dicommeta.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if got, want := metadata.TransferSyntaxUID, "1.2.840.10008.1.2.1"; got != want {
		t.Fatalf("TransferSyntaxUID = %q, want %q", got, want)
	}
	if got, want := metadata.PhotometricInterpretation, "MONOCHROME2"; got != want {
		t.Fatalf("PhotometricInterpretation = %q, want %q", got, want)
	}
	if metadata.MeasurementScale() == nil {
		t.Fatal("MeasurementScale = nil, want preserved PixelSpacing scale")
	}
}
