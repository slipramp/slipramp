// Package ansiblereport runs Ansible Runner and packages its event stream for Run report ingestion.
package ansiblereport

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	SourceSchemaVersion   = 2
	MaximumSourceSize     = 32 * 1024 * 1024
	MaximumSourceEvents   = 200_000
	DefaultReportPath     = "/slipramp/artifacts/ansible-playbook.json"
	defaultRunnerIdentity = "slipramp"
	maximumMetadataSize   = 1024
)

type Mode string

const (
	ModePreview Mode = "preview"
	ModeApply   Mode = "apply"
)

type RunnerStatus string

const (
	RunnerStatusSuccessful RunnerStatus = "successful"
	RunnerStatusFailed     RunnerStatus = "failed"
	RunnerStatusTimeout    RunnerStatus = "timeout"
	RunnerStatusCanceled   RunnerStatus = "canceled"
)

type Versions struct {
	AnsibleCore   string `json:"ansibleCore,omitempty"`
	AnsibleRunner string `json:"ansibleRunner,omitempty"`
}

type RunnerResult struct {
	Status   RunnerStatus `json:"status"`
	ExitCode int          `json:"exitCode"`
}

type sourceEnvelope struct {
	SchemaVersion       int          `json:"schemaVersion"`
	RepositoryDirectory string       `json:"repositoryDirectory"`
	Mode                Mode         `json:"mode"`
	Versions            Versions     `json:"versions"`
	Runner              RunnerResult `json:"runner"`
}

type PackageConfig struct {
	Destination             string
	Mode                    Mode
	RunnerArtifactDirectory string
	RepositoryDirectory     string
	RunnerExitCodeFallback  int
	Canceled                bool
	Versions                Versions
}

func PackageSource(config PackageConfig) error {
	if config.Destination == "" {
		return errors.New("report destination is required")
	}
	if config.Mode != ModePreview && config.Mode != ModeApply {
		return fmt.Errorf("unsupported Ansible report mode %q", config.Mode)
	}
	if config.RunnerArtifactDirectory == "" {
		return errors.New("runner artifact directory is required")
	}
	if !filepath.IsAbs(config.RepositoryDirectory) || filepath.Clean(config.RepositoryDirectory) != config.RepositoryDirectory {
		return errors.New("repository directory must be an absolute clean path")
	}

	runner, err := readRunnerResult(config)
	if err != nil {
		return err
	}
	events, err := sourceEventFiles(filepath.Join(config.RunnerArtifactDirectory, "job_events"))
	if err != nil {
		return err
	}

	header := sourceEnvelope{
		SchemaVersion:       SourceSchemaVersion,
		RepositoryDirectory: config.RepositoryDirectory,
		Mode:                config.Mode,
		Versions:            config.Versions,
		Runner:              runner,
	}
	encodedHeader, err := json.Marshal(header)
	if err != nil {
		return fmt.Errorf("encode Ansible report envelope: %w", err)
	}

	directory := filepath.Dir(config.Destination)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create Ansible report directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".ansible-playbook-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary Ansible report: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()

	buffered := bufio.NewWriter(temporary)
	output := &boundedWriter{destination: buffered, maximum: MaximumSourceSize}
	prefix := append(encodedHeader[:len(encodedHeader)-1], []byte(",\"events\":[")...)
	if _, err := output.Write(prefix); err != nil {
		_ = temporary.Close()
		return err
	}
	for index, event := range events {
		if index > 0 {
			if _, err := output.Write([]byte(",")); err != nil {
				_ = temporary.Close()
				return err
			}
		}
		contents, err := readSourceEvent(event)
		if err != nil {
			_ = temporary.Close()
			return err
		}
		if _, err := output.Write(contents); err != nil {
			_ = temporary.Close()
			return err
		}
	}
	if _, err := output.Write([]byte("]}\n")); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := buffered.Flush(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("flush Ansible report: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync Ansible report: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close Ansible report: %w", err)
	}
	if err := os.Rename(temporaryPath, config.Destination); err != nil {
		return fmt.Errorf("install Ansible report: %w", err)
	}

	return nil
}

func readRunnerResult(config PackageConfig) (RunnerResult, error) {
	status, err := readOptionalString(filepath.Join(config.RunnerArtifactDirectory, "status"))
	if err != nil {
		return RunnerResult{}, fmt.Errorf("read Runner status: %w", err)
	}
	if status == "" {
		switch {
		case config.Canceled:
			status = string(RunnerStatusCanceled)
		case config.RunnerExitCodeFallback == 0:
			status = string(RunnerStatusSuccessful)
		default:
			status = string(RunnerStatusFailed)
		}
	}
	runnerStatus := RunnerStatus(status)
	if runnerStatus != RunnerStatusSuccessful && runnerStatus != RunnerStatusFailed &&
		runnerStatus != RunnerStatusTimeout && runnerStatus != RunnerStatusCanceled {
		return RunnerResult{}, fmt.Errorf("runner status is invalid: %q", status)
	}

	exitCode := config.RunnerExitCodeFallback
	value, err := readOptionalString(filepath.Join(config.RunnerArtifactDirectory, "rc"))
	if err != nil {
		return RunnerResult{}, fmt.Errorf("read Runner exit code: %w", err)
	}
	if value != "" {
		exitCode, err = strconv.Atoi(value)
		if err != nil {
			return RunnerResult{}, fmt.Errorf("runner exit code is invalid: %q", value)
		}
	}
	if exitCode < 0 || exitCode > 255 {
		return RunnerResult{}, fmt.Errorf("runner exit code is outside the supported range: %d", exitCode)
	}

	return RunnerResult{Status: runnerStatus, ExitCode: exitCode}, nil
}

func readOptionalString(path string) (string, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	contents, readErr := io.ReadAll(io.LimitReader(file, maximumMetadataSize+1))
	closeErr := file.Close()
	if readErr != nil {
		return "", readErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if len(contents) > maximumMetadataSize {
		return "", fmt.Errorf("value exceeds %d bytes", maximumMetadataSize)
	}
	if !utf8.Valid(contents) {
		return "", errors.New("value is not valid UTF-8")
	}

	return strings.TrimSpace(string(contents)), nil
}

type sourceEventFile struct {
	counter int
	path    string
}

func sourceEventFiles(directory string) ([]sourceEventFile, error) {
	handle, err := os.Open(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read Runner job events: %w", err)
	}
	defer func() { _ = handle.Close() }()

	events := make([]sourceEventFile, 0)
	for {
		entries, readErr := handle.ReadDir(1024)
		for _, entry := range entries {
			name := entry.Name()
			if strings.HasSuffix(name, "-partial.json") || filepath.Ext(name) != ".json" {
				continue
			}
			separator := strings.IndexByte(name, '-')
			identifier := strings.TrimSuffix(name[separator+1:], ".json")
			if separator <= 0 || identifier == "" {
				return nil, fmt.Errorf("runner event filename is invalid: %s", name)
			}
			counter, parseErr := strconv.Atoi(name[:separator])
			if parseErr != nil || counter <= 0 {
				return nil, fmt.Errorf("runner event filename is invalid: %s", name)
			}
			events = append(events, sourceEventFile{counter: counter, path: filepath.Join(directory, name)})
			if len(events) > MaximumSourceEvents {
				return nil, fmt.Errorf("runner emitted more than %d events", MaximumSourceEvents)
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("read Runner job events: %w", readErr)
		}
	}

	slices.SortFunc(events, func(left, right sourceEventFile) int {
		if left.counter != right.counter {
			return cmp.Compare(left.counter, right.counter)
		}
		return strings.Compare(left.path, right.path)
	})
	for index := 1; index < len(events); index++ {
		if events[index-1].counter == events[index].counter {
			return nil, fmt.Errorf("runner event counter %d is duplicated", events[index].counter)
		}
	}

	return events, nil
}

func readSourceEvent(event sourceEventFile) ([]byte, error) {
	file, err := os.Open(event.path)
	if err != nil {
		return nil, fmt.Errorf("open Runner event %d: %w", event.counter, err)
	}
	contents, readErr := io.ReadAll(io.LimitReader(file, MaximumSourceSize+1))
	closeErr := file.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read Runner event %d: %w", event.counter, readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close Runner event %d: %w", event.counter, closeErr)
	}
	if len(contents) > MaximumSourceSize {
		return nil, fmt.Errorf("runner event %d exceeds the report size limit", event.counter)
	}
	if !utf8.Valid(contents) || !json.Valid(contents) {
		return nil, fmt.Errorf("runner event %d is not valid UTF-8 JSON", event.counter)
	}
	trimmed := bytes.TrimSpace(contents)
	if len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
		return nil, fmt.Errorf("runner event %d must be a JSON object", event.counter)
	}

	var identity struct {
		Counter json.RawMessage `json:"counter"`
	}
	if err := json.Unmarshal(trimmed, &identity); err != nil {
		return nil, fmt.Errorf("decode Runner event %d identity: %w", event.counter, err)
	}
	var counter int
	if len(identity.Counter) == 0 || json.Unmarshal(identity.Counter, &counter) != nil || counter != event.counter {
		return nil, fmt.Errorf("runner event counter does not match %s", filepath.Base(event.path))
	}

	compacted := make([]byte, 0, len(trimmed))
	buffer := bytes.NewBuffer(compacted)
	if err := json.Compact(buffer, trimmed); err != nil {
		return nil, fmt.Errorf("compact Runner event %d: %w", event.counter, err)
	}

	return buffer.Bytes(), nil
}

type boundedWriter struct {
	destination io.Writer
	written     int
	maximum     int
}

func (w *boundedWriter) Write(contents []byte) (int, error) {
	if w.written+len(contents) > w.maximum {
		return 0, fmt.Errorf("ansible output exceeds %d bytes", w.maximum)
	}
	n, err := w.destination.Write(contents)
	w.written += n

	return n, err
}
