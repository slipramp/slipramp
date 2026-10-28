package placement

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"time"

	"go.yaml.in/yaml/v3"
	corev1 "k8s.io/api/core/v1"

	"github.com/slipramp/slipramp/internal/durationvalidate"
)

const (
	FileAPIVersion = "slipramp.com/v1alpha1"
	FileKind       = "PlacementConfig"
)

type fileDocument struct {
	APIVersion string                   `yaml:"apiVersion"`
	Kind       string                   `yaml:"kind"`
	Clusters   map[string]fileCluster   `yaml:"clusters,omitempty"`
	Placements map[string]filePlacement `yaml:"placements"`
}

type fileCluster struct {
	MaximumActiveRuns *int32 `yaml:"maximumActiveRuns,omitempty"`
}

type filePlacement struct {
	ClusterName        string                    `yaml:"cluster"`
	JobNamespace       string                    `yaml:"jobNamespace"`
	MaximumActiveRuns  *int32                    `yaml:"maximumActiveRuns,omitempty"`
	ServiceAccountName string                    `yaml:"serviceAccountName,omitempty"`
	Timeout            *string                   `yaml:"timeout,omitempty"`
	Resources          fileResourceRequirements  `yaml:"resources,omitempty"`
	NodeSelector       map[string]string         `yaml:"nodeSelector,omitempty"`
	Credentials        map[string]fileCredential `yaml:"credentials,omitempty"`
	PodTemplate        yaml.Node                 `yaml:"podTemplate,omitempty"`
}

type fileResourceRequirements struct {
	Requests map[string]string `yaml:"requests,omitempty"`
	Limits   map[string]string `yaml:"limits,omitempty"`
}

type fileCredential struct {
	SecretRef fileSecretReference `yaml:"secretRef"`
}

type fileSecretReference struct {
	Name string `yaml:"name"`
}

// LoadFile reads and validates the versioned Placement configuration shared by
// the Server and repository CLI.
func LoadFile(path string) (Set, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return Set{}, fmt.Errorf("read placement configuration: %w", err)
	}

	return Decode(bytes.NewReader(contents))
}

// Decode validates one versioned Placement configuration document.
func Decode(input io.Reader) (Set, error) {
	decoder := yaml.NewDecoder(input)
	decoder.KnownFields(true)

	var document fileDocument
	if err := decoder.Decode(&document); err != nil {
		return Set{}, fmt.Errorf("decode placement configuration: %w", err)
	}

	if err := requireSingleDocument(decoder); err != nil {
		return Set{}, err
	}

	if document.APIVersion != FileAPIVersion {
		return Set{}, fmt.Errorf("placement configuration apiVersion must be %q", FileAPIVersion)
	}

	if document.Kind != FileKind {
		return Set{}, fmt.Errorf("placement configuration kind must be %q", FileKind)
	}

	if document.Placements == nil {
		return Set{}, errors.New("placement configuration must define placements")
	}

	clusterMaximumActiveRuns := make(map[string]int32, len(document.Clusters))
	for _, name := range slices.Sorted(maps.Keys(document.Clusters)) {
		maximum, err := positiveMaximumActiveRuns("cluster", name, document.Clusters[name].MaximumActiveRuns)
		if err != nil {
			return Set{}, err
		}
		clusterMaximumActiveRuns[name] = maximum
	}

	configured := make(map[string]Config, len(document.Placements))
	for _, name := range slices.Sorted(maps.Keys(document.Placements)) {
		input := document.Placements[name]
		maximumActiveRuns, err := positiveMaximumActiveRuns("placement", name, input.MaximumActiveRuns)
		if err != nil {
			return Set{}, err
		}
		timeout, err := parseFileTimeout(name, input.Timeout)
		if err != nil {
			return Set{}, err
		}

		podTemplate, err := decodeFilePodTemplate(name, input.PodTemplate)
		if err != nil {
			return Set{}, err
		}

		credentials := make(map[string]CredentialSource, len(input.Credentials))
		for credentialName, source := range input.Credentials {
			credentials[credentialName] = CredentialSource{
				SecretRef: SecretReference{Name: source.SecretRef.Name},
			}
		}

		configured[name] = Config{
			ClusterName:        input.ClusterName,
			JobNamespace:       input.JobNamespace,
			MaximumActiveRuns:  maximumActiveRuns,
			ServiceAccountName: input.ServiceAccountName,
			Timeout:            timeout,
			Resources: ResourceRequirements{
				Requests: input.Resources.Requests,
				Limits:   input.Resources.Limits,
			},
			NodeSelector: input.NodeSelector,
			Credentials:  credentials,
			PodTemplate:  podTemplate,
		}
	}

	return NewSetWithClusterLimits(configured, clusterMaximumActiveRuns)
}

func positiveMaximumActiveRuns(subject, name string, value *int32) (int32, error) {
	if value == nil {
		return 0, nil
	}
	if *value < 1 {
		return 0, fmt.Errorf("%s %q maximum active Runs must be positive", subject, name)
	}

	return *value, nil
}

func decodeFilePodTemplate(placementName string, node yaml.Node) (*corev1.PodTemplateSpec, error) {
	if node.Kind == 0 {
		return nil, nil
	}
	if node.Kind == yaml.ScalarNode && node.Tag == "!!null" {
		return nil, fmt.Errorf("placement %q pod template must be an object", placementName)
	}

	var value any
	if err := node.Decode(&value); err != nil {
		return nil, fmt.Errorf("placement %q pod template: %w", placementName, err)
	}

	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode placement %q pod template: %w", placementName, err)
	}

	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()

	var template corev1.PodTemplateSpec
	if err := decoder.Decode(&template); err != nil {
		return nil, fmt.Errorf("decode placement %q pod template: %w", placementName, err)
	}

	return &template, nil
}

func requireSingleDocument(decoder *yaml.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("decode trailing placement configuration: %w", err)
	}

	return errors.New("placement configuration must contain exactly one document")
}

func parseFileTimeout(placementName string, value *string) (time.Duration, error) {
	if value == nil {
		return 0, nil
	}

	return durationvalidate.Positive(fmt.Sprintf("placement %q timeout", placementName), *value)
}
