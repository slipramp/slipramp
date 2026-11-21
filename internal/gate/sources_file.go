package gate

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/slipramp/slipramp/internal/durationvalidate"
)

const (
	SourceFileAPIVersion = "slipramp.com/v1alpha1"
	SourceFileKind       = "GateSourceConfig"
)

type sourceFileDocument struct {
	APIVersion string                      `yaml:"apiVersion"`
	Kind       string                      `yaml:"kind"`
	Sources    map[string]sourceFileSource `yaml:"sources"`
}

type sourceFileSource struct {
	Prometheus sourceFilePrometheus `yaml:"prometheus"`
}

type sourceFilePrometheus struct {
	URL                string         `yaml:"url"`
	PublicURL          string         `yaml:"publicUrl,omitempty"`
	EvaluationInterval *string        `yaml:"evaluationInterval,omitempty"`
	Timeout            *string        `yaml:"timeout,omitempty"`
	MaximumConcurrency *int           `yaml:"maximumConcurrency,omitempty"`
	BearerTokenFile    *string        `yaml:"bearerTokenFile,omitempty"`
	TLS                *sourceFileTLS `yaml:"tls,omitempty"`
}

type sourceFileTLS struct {
	CAFile                *string `yaml:"caFile,omitempty"`
	ClientCertificateFile *string `yaml:"clientCertificateFile,omitempty"`
	ClientKeyFile         *string `yaml:"clientKeyFile,omitempty"`
}

// LoadSourceFile reads and validates one versioned Gate source configuration
// document used by the Server.
func LoadSourceFile(path string) (SourceSet, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read Gate source configuration: %w", err)
	}

	return DecodeSourceFile(bytes.NewReader(contents))
}

// DecodeSourceFile validates one versioned Gate source configuration document.
func DecodeSourceFile(input io.Reader) (SourceSet, error) {
	decoder := yaml.NewDecoder(input)
	decoder.KnownFields(true)

	var document sourceFileDocument
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("decode Gate source configuration: %w", err)
	}
	if err := requireSingleSourceDocument(decoder); err != nil {
		return nil, err
	}
	if document.APIVersion != SourceFileAPIVersion {
		return nil, fmt.Errorf("gate source configuration apiVersion must be %q", SourceFileAPIVersion)
	}
	if document.Kind != SourceFileKind {
		return nil, fmt.Errorf("gate source configuration kind must be %q", SourceFileKind)
	}
	if document.Sources == nil {
		return nil, errors.New("gate source configuration must define sources")
	}

	sources := make(SourceSet, len(document.Sources))
	for _, name := range slices.Sorted(maps.Keys(document.Sources)) {
		encoded := document.Sources[name].Prometheus
		evaluationInterval, err := optionalSourceDuration(
			fmt.Sprintf("Gate source %q Prometheus evaluation interval", name),
			encoded.EvaluationInterval,
			DefaultEvaluationInterval,
		)
		if err != nil {
			return nil, err
		}
		timeout, err := optionalSourceDuration(
			fmt.Sprintf("Gate source %q Prometheus timeout", name),
			encoded.Timeout,
			DefaultRequestTimeout,
		)
		if err != nil {
			return nil, err
		}
		maximumConcurrency := DefaultMaximumConcurrency
		if encoded.MaximumConcurrency != nil {
			maximumConcurrency = *encoded.MaximumConcurrency
		}

		prometheus := PrometheusSource{
			URL:                encoded.URL,
			PublicURL:          encoded.PublicURL,
			EvaluationInterval: evaluationInterval,
			Timeout:            timeout,
			MaximumConcurrency: maximumConcurrency,
		}
		if encoded.BearerTokenFile != nil {
			if *encoded.BearerTokenFile == "" {
				return nil, fmt.Errorf("gate source %q Prometheus bearer token file must not be empty", name)
			}
			prometheus.BearerTokenFile = *encoded.BearerTokenFile
		}
		if encoded.TLS != nil {
			var tlsErr error
			prometheus.TLS.CAFile, tlsErr = optionalSourceFile(name, "TLS CA file", encoded.TLS.CAFile)
			if tlsErr != nil {
				return nil, tlsErr
			}
			prometheus.TLS.ClientCertificateFile, tlsErr = optionalSourceFile(
				name,
				"TLS client certificate file",
				encoded.TLS.ClientCertificateFile,
			)
			if tlsErr != nil {
				return nil, tlsErr
			}
			prometheus.TLS.ClientKeyFile, tlsErr = optionalSourceFile(
				name,
				"TLS client key file",
				encoded.TLS.ClientKeyFile,
			)
			if tlsErr != nil {
				return nil, tlsErr
			}
		}
		sources[name] = Source{Prometheus: prometheus}
	}

	if err := sources.Validate(); err != nil {
		return nil, err
	}

	return sources, nil
}

func optionalSourceDuration(description string, encoded *string, fallback time.Duration) (time.Duration, error) {
	if encoded == nil {
		return fallback, nil
	}

	return durationvalidate.Positive(description, *encoded)
}

func optionalSourceFile(sourceName, description string, encoded *string) (string, error) {
	if encoded == nil {
		return "", nil
	}
	if *encoded == "" {
		return "", fmt.Errorf("gate source %q Prometheus %s must not be empty", sourceName, description)
	}

	return *encoded, nil
}

func requireSingleSourceDocument(decoder *yaml.Decoder) error {
	var trailing any
	err := decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("decode trailing Gate source configuration: %w", err)
	}

	return errors.New("gate source configuration must contain exactly one document")
}
