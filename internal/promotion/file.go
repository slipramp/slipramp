package promotion

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"

	"go.yaml.in/yaml/v3"

	"github.com/slipramp/slipramp/internal/durationvalidate"
)

const (
	IntegrationFileAPIVersion = "slipramp.com/v1alpha1"
	IntegrationFileKind       = "KargoIntegrationConfig"
)

type integrationFileDocument struct {
	APIVersion   string                           `yaml:"apiVersion"`
	Kind         string                           `yaml:"kind"`
	Integrations map[string]integrationFileConfig `yaml:"integrations"`
}

type integrationFileConfig struct {
	ClusterName      string            `yaml:"cluster"`
	UIURL            string            `yaml:"uiUrl,omitempty"`
	ArgoCDUIURLs     map[string]string `yaml:"argoCdUiUrls,omitempty"`
	Repositories     []string          `yaml:"repositories"`
	EnvironmentOrder []string          `yaml:"environmentOrder"`
	Preparation      struct {
		Repository string `yaml:"repository"`
		Target     string `yaml:"target"`
		Action     string `yaml:"action"`
	} `yaml:"preparation"`
	CandidateBranchCleanup *struct {
		Retention   string            `yaml:"retention"`
		Credentials map[string]string `yaml:"credentials"`
	} `yaml:"candidateBranchCleanup,omitempty"`
	MaximumStageObservationAge string `yaml:"maximumStageObservationAge"`
	PendingReviewRetention     string `yaml:"pendingReviewRetention"`
}

// LoadIntegrationFile reads and validates one versioned Kargo integration
// configuration document used by the Server.
func LoadIntegrationFile(path string) (IntegrationSet, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read Kargo integration configuration: %w", err)
	}

	return DecodeIntegrationFile(bytes.NewReader(contents))
}

// DecodeIntegrationFile validates one versioned Kargo integration
// configuration document.
func DecodeIntegrationFile(input io.Reader) (IntegrationSet, error) {
	decoder := yaml.NewDecoder(input)
	decoder.KnownFields(true)

	var document integrationFileDocument
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("decode Kargo integration configuration: %w", err)
	}

	if err := requireSingleIntegrationDocument(decoder); err != nil {
		return nil, err
	}

	if document.APIVersion != IntegrationFileAPIVersion {
		return nil, fmt.Errorf(
			"kargo integration configuration apiVersion must be %q",
			IntegrationFileAPIVersion,
		)
	}

	if document.Kind != IntegrationFileKind {
		return nil, fmt.Errorf("kargo integration configuration kind must be %q", IntegrationFileKind)
	}

	if document.Integrations == nil {
		return nil, errors.New("kargo integration configuration must define integrations")
	}

	integrations := make(IntegrationSet, len(document.Integrations))
	for _, name := range slices.Sorted(maps.Keys(document.Integrations)) {
		input := document.Integrations[name]
		maximumAge, err := durationvalidate.Positive(
			fmt.Sprintf("Kargo integration %q maximum Stage observation age", name),
			input.MaximumStageObservationAge,
		)
		if err != nil {
			return nil, err
		}
		pendingRetention, err := durationvalidate.Positive(
			fmt.Sprintf("Kargo integration %q pending review retention", name),
			input.PendingReviewRetention,
		)
		if err != nil {
			return nil, err
		}

		var branchCleanup *CandidateBranchCleanupConfig
		if input.CandidateBranchCleanup != nil {
			cleanupRetention, cleanupErr := durationvalidate.Positive(
				fmt.Sprintf("Kargo integration %q candidate branch cleanup retention", name),
				input.CandidateBranchCleanup.Retention,
			)
			if cleanupErr != nil {
				return nil, cleanupErr
			}
			branchCleanup = &CandidateBranchCleanupConfig{
				Retention:   cleanupRetention,
				Credentials: maps.Clone(input.CandidateBranchCleanup.Credentials),
			}
		}

		integrations[name] = IntegrationConfig{
			ClusterName:      input.ClusterName,
			UIURL:            input.UIURL,
			ArgoCDUIURLs:     maps.Clone(input.ArgoCDUIURLs),
			Repositories:     slices.Clone(input.Repositories),
			EnvironmentOrder: slices.Clone(input.EnvironmentOrder),
			Preparation: PreparationConfig{
				Repository: input.Preparation.Repository,
				Target:     input.Preparation.Target,
				Action:     input.Preparation.Action,
			},
			CandidateBranchCleanup:     branchCleanup,
			MaximumStageObservationAge: maximumAge,
			PendingReviewRetention:     pendingRetention,
		}
	}

	if err := integrations.Validate(); err != nil {
		return nil, err
	}

	return integrations, nil
}

func requireSingleIntegrationDocument(decoder *yaml.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("decode trailing Kargo integration configuration: %w", err)
	}

	return errors.New("kargo integration configuration must contain exactly one document")
}
