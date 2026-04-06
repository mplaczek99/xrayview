package rustdecode

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

	"xrayview/go-backend/internal/contracts"
	"xrayview/go-backend/internal/imaging"
)

const (
	HelperBinaryName = "xrayview-decode-helper"
	HelperBinaryEnv  = "XRAYVIEW_RUST_DECODE_HELPER_BIN"
)

type SourceStudy struct {
	Image            imaging.SourceImage         `json:"image"`
	Metadata         SourceMetadata              `json:"metadata"`
	MeasurementScale *contracts.MeasurementScale `json:"measurementScale,omitempty"`
}

type SourceMetadata struct {
	StudyInstanceUID  string             `json:"studyInstanceUid"`
	PreservedElements []PreservedElement `json:"preservedElements"`
}

type PreservedElement struct {
	TagGroup   uint16   `json:"tagGroup"`
	TagElement uint16   `json:"tagElement"`
	VR         string   `json:"vr"`
	Values     []string `json:"values"`
}

type Helper struct {
	command []string
	runner  commandRunner
}

type commandRunner interface {
	Run(ctx context.Context, command []string) ([]byte, []byte, error)
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

func (helper *Helper) DecodeStudy(ctx context.Context, inputPath string) (SourceStudy, error) {
	if strings.TrimSpace(inputPath) == "" {
		return SourceStudy{}, errors.New("input path is required")
	}

	command := append(helper.Command(), "--input", inputPath)
	stdout, stderr, err := helper.runner.Run(ctx, command)
	if err != nil {
		message := strings.TrimSpace(string(stderr))
		if message == "" {
			message = strings.TrimSpace(string(stdout))
		}
		if message != "" {
			return SourceStudy{}, fmt.Errorf("run rust decode helper: %w: %s", err, message)
		}
		return SourceStudy{}, fmt.Errorf("run rust decode helper: %w", err)
	}

	var study SourceStudy
	if err := json.Unmarshal(bytes.TrimSpace(stdout), &study); err != nil {
		return SourceStudy{}, fmt.Errorf("decode rust helper payload: %w", err)
	}

	if err := validateStudy(study); err != nil {
		return SourceStudy{}, err
	}

	return study, nil
}

func newHelper(command []string, runner commandRunner) (*Helper, error) {
	if len(command) == 0 {
		return nil, errors.New("rust decode helper command is required")
	}
	if runner == nil {
		return nil, errors.New("rust decode helper runner is required")
	}

	return &Helper{
		command: append([]string(nil), command...),
		runner:  runner,
	}, nil
}

func (execRunner) Run(ctx context.Context, command []string) ([]byte, []byte, error) {
	if len(command) == 0 {
		return nil, nil, errors.New("command is required")
	}

	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	return stdout.Bytes(), stderr.Bytes(), err
}

func validateStudy(study SourceStudy) error {
	if err := study.Image.Validate(); err != nil {
		return fmt.Errorf("rust helper returned an invalid image: %w", err)
	}

	if strings.TrimSpace(study.Metadata.StudyInstanceUID) == "" {
		return errors.New("rust helper returned an empty studyInstanceUid")
	}

	for _, element := range study.Metadata.PreservedElements {
		if strings.TrimSpace(element.VR) == "" {
			return errors.New("rust helper returned a preserved element without a VR")
		}
	}

	return nil
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
