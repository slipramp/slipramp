package gate

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

	"github.com/slipramp/slipramp/internal/durationvalidate"
)

const (
	SourceFileAPIVersion = "slipramp.com/v1alpha1"
	SourceFileKind       = "GateSourceConfig"
)

type sourceFileDocument struct {
	APIVersion string               `yaml:"apiVersion"`
	Kind       string               `yaml:"kind"`
	Sources    map[string]yaml.Node `yaml:"sources"`
}

type sourceFilePrometheus struct {
	sourceFileHTTP
	PublicURL string `json:"publicUrl,omitempty"`
}

type sourceFileHTTP struct {
	URL                string         `json:"url"`
	EvaluationInterval *string        `json:"evaluationInterval,omitempty"`
	Timeout            *string        `json:"timeout,omitempty"`
	MaximumConcurrency *int           `json:"maximumConcurrency,omitempty"`
	BearerTokenFile    *string        `json:"bearerTokenFile,omitempty"`
	TLS                *sourceFileTLS `json:"tls,omitempty"`
}

type sourceFileTLS struct {
	CAFile                *string `json:"caFile,omitempty"`
	ClientCertificateFile *string `json:"clientCertificateFile,omitempty"`
	ClientKeyFile         *string `json:"clientKeyFile,omitempty"`
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
		node := document.Sources[name]
		evaluator, fields, err := evaluatorFields(&node)
		if err != nil {
			return nil, fmt.Errorf("decode Gate source %q: %w", name, err)
		}
		codec, err := lookupEvaluator(evaluator)
		if err != nil {
			return nil, fmt.Errorf("decode Gate source %q: %w", name, err)
		}
		raw, err := validatedFields(fields, codec.sourceSchema)
		if err != nil {
			return nil, fmt.Errorf("validate Gate source %q: %w", name, err)
		}
		source, err := codec.decodeSource(name, raw)
		if err != nil {
			return nil, fmt.Errorf("decode Gate source %q: %w", name, err)
		}
		sources[name] = source
	}

	if err := sources.Validate(); err != nil {
		return nil, err
	}

	return sources, nil
}

func decodePrometheusSource(name string, fields json.RawMessage) (Source, error) {
	var encoded sourceFilePrometheus
	if err := decodePayload(fields, &encoded); err != nil {
		return nil, err
	}
	config, err := encoded.settings(name)
	if err != nil {
		return nil, err
	}
	return PrometheusSource{HTTPSource: config, PublicURL: encoded.PublicURL}, nil
}

func decodeWebhookSource(name string, fields json.RawMessage) (Source, error) {
	var encoded sourceFileHTTP
	if err := decodePayload(fields, &encoded); err != nil {
		return nil, err
	}
	config, err := encoded.settings(name)
	if err != nil {
		return nil, err
	}
	return WebhookSource{HTTPSource: config}, nil
}

func (encoded sourceFileHTTP) settings(name string) (HTTPSource, error) {
	evaluationInterval, err := optionalSourceDuration(
		fmt.Sprintf("Gate source %q evaluation interval", name),
		encoded.EvaluationInterval,
		DefaultEvaluationInterval,
	)
	if err != nil {
		return HTTPSource{}, err
	}
	timeout, err := optionalSourceDuration(
		fmt.Sprintf("Gate source %q timeout", name),
		encoded.Timeout,
		DefaultRequestTimeout,
	)
	if err != nil {
		return HTTPSource{}, err
	}
	maximumConcurrency := DefaultMaximumConcurrency
	if encoded.MaximumConcurrency != nil {
		maximumConcurrency = *encoded.MaximumConcurrency
	}

	config := HTTPSource{
		URL:                encoded.URL,
		EvaluationInterval: evaluationInterval,
		Timeout:            timeout,
		MaximumConcurrency: maximumConcurrency,
	}
	if encoded.BearerTokenFile != nil {
		if *encoded.BearerTokenFile == "" {
			return HTTPSource{}, fmt.Errorf("gate source %q bearer token file must not be empty", name)
		}
		config.BearerTokenFile = *encoded.BearerTokenFile
	}
	if encoded.TLS != nil {
		var tlsErr error
		config.TLS.CAFile, tlsErr = optionalSourceFile(name, "TLS CA file", encoded.TLS.CAFile)
		if tlsErr != nil {
			return HTTPSource{}, tlsErr
		}
		config.TLS.ClientCertificateFile, tlsErr = optionalSourceFile(
			name,
			"TLS client certificate file",
			encoded.TLS.ClientCertificateFile,
		)
		if tlsErr != nil {
			return HTTPSource{}, tlsErr
		}
		config.TLS.ClientKeyFile, tlsErr = optionalSourceFile(
			name,
			"TLS client key file",
			encoded.TLS.ClientKeyFile,
		)
		if tlsErr != nil {
			return HTTPSource{}, tlsErr
		}
	}
	return config, nil
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
		return "", fmt.Errorf("gate source %q %s must not be empty", sourceName, description)
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
