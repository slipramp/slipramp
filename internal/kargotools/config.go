package kargotools

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"
)

const (
	DefaultArgoTargetsPath = "/run/slipramp/credentials/argocd/targets.json"
	DefaultGitDiffPath     = "/slipramp/artifacts/git.diff"
	DefaultArgoDiffPath    = "/slipramp/artifacts/argo.json"
)

var fullGitObjectID = regexp.MustCompile(`^(?:[0-9a-fA-F]{40}|[0-9a-fA-F]{64})$`)

type Config struct {
	PromotionID         string
	ExternalPromotionID string
	Project             string
	Stage               string
	Application         string
	Environment         string
	RepositoryName      string
	RepositoryURL       string
	BaseRevision        string
	CandidateRevision   string
	CandidateBranch     string
	TargetBranch        string
	SourcePaths         string

	ArgoTargetsPath string
	GitDiffPath     string
	ArgoDiffPath    string
	GitBinary       string
	ArgoBinary      string
	GitUsername     string
	GitPassword     string
}

type ArgoTarget struct {
	Server    string `json:"server"`
	AuthToken string `json:"authToken"`
	Plaintext bool   `json:"plaintext,omitempty"`
	GRPCWeb   bool   `json:"grpcWeb,omitempty"`
}

func (c *Config) setDefaults() {
	if c.ArgoTargetsPath == "" {
		c.ArgoTargetsPath = DefaultArgoTargetsPath
	}
	if c.GitDiffPath == "" {
		c.GitDiffPath = DefaultGitDiffPath
	}
	if c.ArgoDiffPath == "" {
		c.ArgoDiffPath = DefaultArgoDiffPath
	}
	if c.GitBinary == "" {
		c.GitBinary = "git"
	}
	if c.ArgoBinary == "" {
		c.ArgoBinary = "argocd"
	}
	c.Environment = strings.ToLower(strings.TrimSpace(c.Environment))
	c.BaseRevision = strings.ToLower(c.BaseRevision)
	c.CandidateRevision = strings.ToLower(c.CandidateRevision)
}

func (c Config) validate() error {
	required := []struct {
		name  string
		value string
	}{
		{name: "promotion-id", value: c.PromotionID},
		{name: "external-promotion-id", value: c.ExternalPromotionID},
		{name: "project", value: c.Project},
		{name: "stage", value: c.Stage},
		{name: "application", value: c.Application},
		{name: "environment", value: c.Environment},
		{name: "repository-name", value: c.RepositoryName},
		{name: "repository-url", value: c.RepositoryURL},
		{name: "base-revision", value: c.BaseRevision},
		{name: "candidate-revision", value: c.CandidateRevision},
		{name: "candidate-branch", value: c.CandidateBranch},
		{name: "target-branch", value: c.TargetBranch},
		{name: "source-paths", value: c.SourcePaths},
	}
	for _, field := range required {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%s is required", field.name)
		}
	}
	if !fullGitObjectID.MatchString(c.BaseRevision) {
		return errors.New("base-revision must be a full Git object ID")
	}
	if !fullGitObjectID.MatchString(c.CandidateRevision) {
		return errors.New("candidate-revision must be a full Git object ID")
	}
	if !strings.HasPrefix(c.CandidateBranch, "kargo/promotion/") ||
		len(c.CandidateBranch) == len("kargo/promotion/") {
		return errors.New("candidate-branch must be under kargo/promotion/")
	}
	if c.GitDiffPath == c.ArgoDiffPath {
		return errors.New("git and Argo CD diff paths must be different")
	}
	if (c.GitUsername == "") != (c.GitPassword == "") {
		return errors.New("git username and password must be configured together")
	}

	var sourcePaths []string
	if err := json.Unmarshal([]byte(c.SourcePaths), &sourcePaths); err != nil {
		return fmt.Errorf("source-paths must be a JSON string array: %w", err)
	}
	if sourcePaths == nil {
		return errors.New("source-paths must be a JSON string array")
	}
	for index, sourcePath := range sourcePaths {
		if strings.TrimSpace(sourcePath) == "" {
			return fmt.Errorf("source-paths item %d must not be empty", index+1)
		}
	}

	return nil
}

func (c Config) gitEnvironment() []string {
	environment := setEnvironment(os.Environ(), "GIT_TERMINAL_PROMPT", "0")
	if c.GitUsername == "" {
		return environment
	}

	credential := base64.StdEncoding.EncodeToString([]byte(c.GitUsername + ":" + c.GitPassword))
	environment = setEnvironment(environment, "GIT_CONFIG_COUNT", "1")
	environment = setEnvironment(environment, "GIT_CONFIG_KEY_0", "http.extraHeader")
	environment = setEnvironment(environment, "GIT_CONFIG_VALUE_0", "Authorization: Basic "+credential)

	return environment
}

func (t ArgoTarget) validate(environment string) error {
	if strings.TrimSpace(t.Server) == "" || strings.ContainsAny(t.Server, "\x00\r\n") {
		return fmt.Errorf("argo CD target for environment %q requires a valid server", environment)
	}
	if strings.TrimSpace(t.AuthToken) == "" || strings.ContainsAny(t.AuthToken, "\x00\r\n") {
		return fmt.Errorf("argo CD target for environment %q requires a valid auth token", environment)
	}

	return nil
}

func loadArgoTargets(path string) (map[string]ArgoTarget, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open Argo CD targets: %w", err)
	}
	defer func() { _ = file.Close() }()

	targets := map[string]ArgoTarget{}
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&targets); err != nil {
		return nil, fmt.Errorf("decode Argo CD targets: %w", err)
	}
	if err := requireJSONEnd(decoder); err != nil {
		return nil, fmt.Errorf("decode Argo CD targets: %w", err)
	}
	if len(targets) == 0 {
		return nil, errors.New("at least one Argo CD target is required")
	}
	for _, environment := range slices.Sorted(maps.Keys(targets)) {
		if environment != strings.ToLower(strings.TrimSpace(environment)) || environment == "" {
			return nil, fmt.Errorf("argo CD target environment %q must be normalized lowercase", environment)
		}
		if err := targets[environment].validate(environment); err != nil {
			return nil, err
		}
	}

	return targets, nil
}

func requireJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("unexpected data after JSON object")
		}
		return err
	}

	return nil
}
