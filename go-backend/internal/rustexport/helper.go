package rustexport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"xrayview/go-backend/internal/imaging"
	"xrayview/go-backend/internal/rustdecode"
)

const (
	HelperBinaryName = "xrayview-export-helper"
	HelperBinaryEnv  = "XRAYVIEW_RUST_EXPORT_HELPER_BIN"
)

type exportRequest struct {
	Preview  exportPreviewImage        `json:"preview"`
	Metadata rustdecode.SourceMetadata `json:"metadata"`
}

type exportPreviewImage struct {
	Width  uint32              `json:"width"`
	Height uint32              `json:"height"`
	Format imaging.ImageFormat `json:"format"`
	Pixels []int               `json:"pixels"`
}

type Helper struct {
	command []string
	runner  commandRunner
}

type commandRunner interface {
	Run(ctx context.Context, command []string, stdin []byte) ([]byte, []byte, error)
}

type execRunner struct{}

func New(command []string) (*Helper, error) {
	return newHelper(command, execRunner{})
}

func NewFromEnvironment() (*Helper, error) {
	return New(CommandFromEnvironment())
}

func CommandFromEnvironment() []string {
	if value, ok := os.LookupEnv(HelperBinaryEnv); ok {
		value = strings.TrimSpace(value)
		if value != "" {
			return []string{value}
		}
	}

	return DefaultDevCommand()
}

func DefaultDevCommand() []string {
	if binaryPath := existingDevBinaryPath(); binaryPath != "" {
		return []string{binaryPath}
	}

	return []string{
		"cargo",
		"run",
		"--quiet",
		"--manifest-path",
		backendManifestPath(),
		"--bin",
		HelperBinaryName,
		"--",
	}
}

func (helper *Helper) Command() []string {
	return append([]string(nil), helper.command...)
}

func (helper *Helper) WriteSecondaryCapture(
	ctx context.Context,
	outputPath string,
	preview imaging.PreviewImage,
	sourceMeta rustdecode.SourceMetadata,
) error {
	if strings.TrimSpace(outputPath) == "" {
		return errors.New("output path is required")
	}
	if err := preview.Validate(); err != nil {
		return fmt.Errorf("validate preview image: %w", err)
	}
	if strings.TrimSpace(sourceMeta.StudyInstanceUID) == "" {
		return errors.New("study instance uid is required")
	}

	payload, err := json.Marshal(exportRequest{
		Preview:  newExportPreviewImage(preview),
		Metadata: sourceMeta,
	})
	if err != nil {
		return fmt.Errorf("encode rust export helper payload: %w", err)
	}

	command := append(helper.Command(), "--output", outputPath)
	stdout, stderr, err := helper.runner.Run(ctx, command, payload)
	if err != nil {
		message := strings.TrimSpace(string(stderr))
		if message == "" {
			message = strings.TrimSpace(string(stdout))
		}
		if message != "" {
			return fmt.Errorf("run rust export helper: %w: %s", err, message)
		}
		return fmt.Errorf("run rust export helper: %w", err)
	}

	if message := strings.TrimSpace(string(stdout)); message != "" {
		return fmt.Errorf("rust export helper emitted unexpected stdout: %s", message)
	}

	return nil
}

func newExportPreviewImage(preview imaging.PreviewImage) exportPreviewImage {
	pixels := make([]int, len(preview.Pixels))
	for index, value := range preview.Pixels {
		pixels[index] = int(value)
	}

	return exportPreviewImage{
		Width:  preview.Width,
		Height: preview.Height,
		Format: preview.Format,
		Pixels: pixels,
	}
}

func newHelper(command []string, runner commandRunner) (*Helper, error) {
	if len(command) == 0 {
		return nil, errors.New("rust export helper command is required")
	}
	if runner == nil {
		return nil, errors.New("rust export helper runner is required")
	}

	return &Helper{
		command: append([]string(nil), command...),
		runner:  runner,
	}, nil
}

func (execRunner) Run(ctx context.Context, command []string, stdin []byte) ([]byte, []byte, error) {
	if len(command) == 0 {
		return nil, nil, errors.New("command is required")
	}

	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	return stdout.Bytes(), stderr.Bytes(), err
}

func existingDevBinaryPath() string {
	binaryPath := filepath.Join(filepath.Dir(backendManifestPath()), "target", "debug", helperExecutableName())
	info, err := os.Stat(binaryPath)
	if err != nil || info.IsDir() {
		return ""
	}

	return binaryPath
}

func backendManifestPath() string {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		return filepath.Clean(filepath.Join("..", "backend", "Cargo.toml"))
	}

	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "backend", "Cargo.toml"))
}

func helperExecutableName() string {
	if runtime.GOOS == "windows" {
		return HelperBinaryName + ".exe"
	}

	return HelperBinaryName
}
