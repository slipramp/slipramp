package harness

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	ServerURLEnvironment      = "SLIPRAMP_SERVER_URL"
	RunIDEnvironment          = "SLIPRAMP_RUN_ID"
	ArtifactTokenEnvironment  = "SLIPRAMP_ARTIFACT_TOKEN"
	ArtifactsEnvironment      = "SLIPRAMP_ARTIFACTS"
	ArtifactInputsEnvironment = "SLIPRAMP_ARTIFACT_INPUTS"
)

var internalEnvironmentNames = map[string]struct{}{
	ServerURLEnvironment:        {},
	RunIDEnvironment:            {},
	ArtifactTokenEnvironment:    {},
	ArtifactsEnvironment:        {},
	ArtifactInputsEnvironment:   {},
	CancellationFileEnvironment: {},
}

type ArtifactDeclaration struct {
	Path      string `json:"path"`
	MediaType string `json:"mediaType,omitempty"`
	Sensitive bool   `json:"sensitive,omitempty"`
}

type InputDeclaration struct {
	MountPath string `json:"mountPath"`
}

type ArtifactConfig struct {
	ServerURL string
	RunID     string
	Token     string
	Artifacts map[string]ArtifactDeclaration
	Inputs    map[string]InputDeclaration
	Client    *http.Client
}

func LoadArtifactConfig(lookup func(string) string) (ArtifactConfig, error) {
	values := []string{
		lookup(ServerURLEnvironment),
		lookup(RunIDEnvironment),
		lookup(ArtifactTokenEnvironment),
		lookup(ArtifactsEnvironment),
		lookup(ArtifactInputsEnvironment),
	}
	allEmpty := true
	for _, value := range values {
		allEmpty = allEmpty && value == ""
	}
	if allEmpty {
		return ArtifactConfig{}, nil
	}
	if values[0] == "" || values[1] == "" || values[2] == "" {
		return ArtifactConfig{}, errors.New("artifact transfer environment is incomplete")
	}
	serverURL, err := url.Parse(values[0])
	if err != nil || serverURL.Scheme == "" || serverURL.Host == "" {
		return ArtifactConfig{}, errors.New("artifact Server URL must be absolute")
	}

	config := ArtifactConfig{
		ServerURL: strings.TrimRight(values[0], "/"),
		RunID:     values[1],
		Token:     values[2],
		Artifacts: map[string]ArtifactDeclaration{},
		Inputs:    map[string]InputDeclaration{},
		Client:    &http.Client{Timeout: 2 * time.Minute},
	}
	if values[3] != "" && values[3] != "null" {
		if err := json.Unmarshal([]byte(values[3]), &config.Artifacts); err != nil {
			return ArtifactConfig{}, fmt.Errorf("decode artifact declarations: %w", err)
		}
	}
	if values[4] != "" && values[4] != "null" {
		if err := json.Unmarshal([]byte(values[4]), &config.Inputs); err != nil {
			return ArtifactConfig{}, fmt.Errorf("decode artifact input declarations: %w", err)
		}
	}

	return config, nil
}

func (c ArtifactConfig) Enabled() bool {
	return c.ServerURL != ""
}

func (c ArtifactConfig) PrepareArtifactOutputs() error {
	for _, name := range slices.Sorted(maps.Keys(c.Artifacts)) {
		declaration := c.Artifacts[name]
		if err := os.MkdirAll(filepath.Dir(declaration.Path), 0o755); err != nil {
			return fmt.Errorf("prepare artifact %q directory: %w", name, err)
		}
	}

	return nil
}

func (c ArtifactConfig) MaterializeInputs(ctx context.Context) error {
	for _, name := range slices.Sorted(maps.Keys(c.Inputs)) {
		declaration := c.Inputs[name]
		if err := c.downloadInput(ctx, name, declaration.MountPath); err != nil {
			return fmt.Errorf("materialize input %q: %w", name, err)
		}
	}

	return nil
}

func (c ArtifactConfig) UploadArtifacts(ctx context.Context) error {
	for _, name := range slices.Sorted(maps.Keys(c.Artifacts)) {
		declaration := c.Artifacts[name]
		if err := c.uploadArtifact(ctx, name, declaration.Path); err != nil {
			return fmt.Errorf("upload artifact %q: %w", name, err)
		}
	}

	return nil
}

func (c ArtifactConfig) downloadInput(ctx context.Context, name string, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("create input directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".slipramp-input-*")
	if err != nil {
		return fmt.Errorf("create temporary input: %w", err)
	}
	temporaryName := temporary.Name()
	defer func() { _ = os.Remove(temporaryName) }()

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		c.ServerURL+"/execution/v1/runs/"+url.PathEscape(c.RunID)+"/inputs/"+url.PathEscape(name),
		nil,
	)
	if err != nil {
		_ = temporary.Close()
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.Token)
	response, err := c.Client.Do(request)
	if err != nil {
		_ = temporary.Close()
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		_ = temporary.Close()
		return fmt.Errorf("server returned HTTP %d", response.StatusCode)
	}

	digest := sha256.New()
	if _, err := io.Copy(io.MultiWriter(temporary, digest), response.Body); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write input: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close input: %w", err)
	}
	wantDigest := strings.TrimPrefix(response.Header.Get("Digest"), "sha-256=")
	gotDigest := hex.EncodeToString(digest.Sum(nil))
	if wantDigest == "" || gotDigest != wantDigest {
		return errors.New("input digest does not match metadata")
	}
	if err := os.Rename(temporaryName, destination); err != nil {
		return fmt.Errorf("install input: %w", err)
	}

	return nil
}

func (c ArtifactConfig) uploadArtifact(ctx context.Context, name, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat declared file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("declared artifact must be a regular file")
	}

	digest, err := fileSHA256(path)
	if err != nil {
		return err
	}
	endpoint := c.ServerURL + "/execution/v1/runs/" + url.PathEscape(c.RunID) +
		"/artifacts/" + url.PathEscape(name)

	var lastErr error
	for attempt := range 3 {
		body := io.ReadCloser(http.NoBody)
		if info.Size() > 0 {
			body, err = os.Open(path)
			if err != nil {
				return fmt.Errorf("open declared file: %w", err)
			}
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, body)
		if err != nil {
			_ = body.Close()
			return err
		}
		request.ContentLength = info.Size()
		request.Header.Set("Authorization", "Bearer "+c.Token)
		request.Header.Set("Content-Type", "application/octet-stream")
		request.Header.Set("X-Slipramp-Content-SHA256", digest)
		response, err := c.Client.Do(request)
		_ = body.Close()
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusNoContent {
				return nil
			}
			if response.StatusCode < 500 {
				return fmt.Errorf("server returned HTTP %d", response.StatusCode)
			}
			lastErr = fmt.Errorf("server returned HTTP %d", response.StatusCode)
		} else {
			lastErr = err
		}
		if attempt < 2 {
			timer := time.NewTimer(time.Duration(attempt+1) * 250 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}

	return lastErr
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open declared file for digest: %w", err)
	}
	defer func() { _ = file.Close() }()

	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", fmt.Errorf("digest declared file: %w", err)
	}

	return hex.EncodeToString(digest.Sum(nil)), nil
}

func actionEnvironment(environment []string) []string {
	result := make([]string, 0, len(environment))
	for _, value := range environment {
		name, _, _ := strings.Cut(value, "=")
		if _, internal := internalEnvironmentNames[name]; !internal {
			result = append(result, value)
		}
	}

	return result
}
