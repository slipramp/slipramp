package config

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

	"github.com/slipramp/slipramp/internal/placement"
)

const (
	serverPlacementsFileEnvironment = "SLIPRAMP_PLACEMENTS_FILE"
	placementsAPIVersion            = "slipramp.com/v1alpha1"
	placementsKind                  = "PlacementConfig"
)

type placementsDocument struct {
	APIVersion string                        `yaml:"apiVersion"`
	Kind       string                        `yaml:"kind"`
	Placements map[string]placementFileEntry `yaml:"placements"`
}

type placementFileEntry struct {
	ClusterName        string                          `yaml:"cluster"`
	JobNamespace       string                          `yaml:"jobNamespace"`
	ServiceAccountName string                          `yaml:"serviceAccountName,omitempty"`
	Timeout            *string                         `yaml:"timeout,omitempty"`
	Resources          resourceRequirementsFileEntry   `yaml:"resources,omitempty"`
	NodeSelector       map[string]string               `yaml:"nodeSelector,omitempty"`
	Credentials        map[string]credentialSourceFile `yaml:"credentials,omitempty"`
	PodTemplate        yaml.Node                       `yaml:"podTemplate,omitempty"`
}

type resourceRequirementsFileEntry struct {
	Requests map[string]string `yaml:"requests,omitempty"`
	Limits   map[string]string `yaml:"limits,omitempty"`
}

type credentialSourceFile struct {
	SecretRef secretReferenceFile `yaml:"secretRef"`
}

type secretReferenceFile struct {
	Name string `yaml:"name"`
}

func loadPlacementsFile(path string) (placement.Set, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return placement.Set{}, fmt.Errorf("read placement configuration: %w", err)
	}

	decoder := yaml.NewDecoder(bytes.NewReader(contents))
	decoder.KnownFields(true)

	var document placementsDocument
	if err := decoder.Decode(&document); err != nil {
		return placement.Set{}, fmt.Errorf("decode placement configuration: %w", err)
	}

	if err := requireSingleYAMLDocument(decoder); err != nil {
		return placement.Set{}, err
	}

	if document.APIVersion != placementsAPIVersion {
		return placement.Set{}, fmt.Errorf("placement configuration apiVersion must be %q", placementsAPIVersion)
	}

	if document.Kind != placementsKind {
		return placement.Set{}, fmt.Errorf("placement configuration kind must be %q", placementsKind)
	}

	if document.Placements == nil {
		return placement.Set{}, errors.New("placement configuration must define placements")
	}

	configured := make(map[string]placement.Config, len(document.Placements))
	for _, name := range slices.Sorted(maps.Keys(document.Placements)) {
		input := document.Placements[name]
		timeout, err := parsePlacementTimeout(name, input.Timeout)
		if err != nil {
			return placement.Set{}, err
		}

		podTemplate, err := decodePodTemplate(name, input.PodTemplate)
		if err != nil {
			return placement.Set{}, err
		}

		credentials := make(map[string]placement.CredentialSource, len(input.Credentials))
		for credentialName, source := range input.Credentials {
			credentials[credentialName] = placement.CredentialSource{
				SecretRef: placement.SecretReference{
					Name: source.SecretRef.Name,
				},
			}
		}

		configured[name] = placement.Config{
			ClusterName:        input.ClusterName,
			JobNamespace:       input.JobNamespace,
			ServiceAccountName: input.ServiceAccountName,
			Timeout:            timeout,
			Resources: placement.ResourceRequirements{
				Requests: input.Resources.Requests,
				Limits:   input.Resources.Limits,
			},
			NodeSelector: input.NodeSelector,
			Credentials:  credentials,
			PodTemplate:  podTemplate,
		}
	}

	return placement.NewSet(configured)
}

func decodePodTemplate(placementName string, node yaml.Node) (*corev1.PodTemplateSpec, error) {
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

func requireSingleYAMLDocument(decoder *yaml.Decoder) error {
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

func parsePlacementTimeout(placementName string, value *string) (time.Duration, error) {
	if value == nil {
		return 0, nil
	}

	timeout, err := time.ParseDuration(*value)
	if err != nil {
		return 0, fmt.Errorf("placement %q timeout: %w", placementName, err)
	}

	if timeout <= 0 {
		return 0, fmt.Errorf("placement %q timeout must be positive", placementName)
	}

	return timeout, nil
}
