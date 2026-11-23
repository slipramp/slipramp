// Package gate owns repository-defined release Gate evaluation contracts.
package gate

import (
	"fmt"
	"maps"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/slipramp/slipramp/internal/kubevalidate"
)

const (
	DefaultEvaluationInterval = 15 * time.Second
	DefaultRequestTimeout     = 5 * time.Second
	DefaultMaximumConcurrency = 4
	MaximumSourceConcurrency  = 32
)

// SourceSet is the administrator-configured collection of Gate evaluator sources.
type SourceSet map[string]Source

type Source interface {
	Type() string
	Validate() error
	Schedule() (time.Duration, int)
	Open() (Evaluator, error)
}

type PrometheusSource struct {
	HTTPSource
	PublicURL string
}

type HTTPSource struct {
	URL                string
	EvaluationInterval time.Duration
	Timeout            time.Duration
	MaximumConcurrency int
	BearerTokenFile    string
	TLS                TLSConfig
}

type TLSConfig struct {
	CAFile                string
	ClientCertificateFile string
	ClientKeyFile         string
}

func (s SourceSet) Names() []string {
	return slices.Sorted(maps.Keys(s))
}

// QueryURL opens the requirement's expression in its source's browser-facing viewer.
func (s SourceSet) QueryURL(requirement Requirement) *string {
	if requirement.Policy == nil || requirement.State == RequirementStopped {
		return nil
	}
	source := s[requirement.Policy.SourceName()]
	viewer, ok := source.(interface{ QueryURL(Requirement) *string })
	if !ok || source.Type() != requirement.Policy.Type() {
		return nil
	}
	return viewer.QueryURL(requirement)
}

func (s PrometheusSource) QueryURL(requirement Requirement) *string {
	definition, ok := requirement.Policy.(*PrometheusDefinition)
	if !ok || s.PublicURL == "" {
		return nil
	}
	base, err := validateSourceURL(PrometheusType, s.PublicURL)
	if err != nil {
		return nil
	}
	viewer := base.JoinPath("query")
	params := viewer.Query()
	params.Set("g0.expr", definition.Query)
	params.Set("g0.tab", "graph")

	evaluatedAt := requirement.EvaluatedAt
	if evaluatedAt == nil && requirement.State == RequirementReleased && len(requirement.Overrides) > 0 {
		last := requirement.Overrides[len(requirement.Overrides)-1]
		if last.Reference == requirement.Reference {
			evaluatedAt = last.EvaluatedAt
		}
	}
	if evaluatedAt != nil {
		params.Set("g0.end_input", evaluatedAt.UTC().Format("2006-01-02T15:04:05.000Z"))
	}
	viewer.RawQuery = params.Encode()
	return new(viewer.String())
}

func (s SourceSet) Validate() error {
	for _, name := range s.Names() {
		if err := kubevalidate.DNSLabel("Gate source name", name); err != nil {
			return err
		}
		if s[name] == nil {
			return fmt.Errorf("missing Gate source %q", name)
		}
		if err := s[name].Validate(); err != nil {
			return fmt.Errorf("validate Gate source %q: %w", name, err)
		}
	}

	return nil
}

func (PrometheusSource) Type() string { return PrometheusType }
func (s HTTPSource) Schedule() (time.Duration, int) {
	return s.EvaluationInterval, s.MaximumConcurrency
}
func (s PrometheusSource) Open() (Evaluator, error) { return NewPrometheusEvaluator(s) }

func (s PrometheusSource) Validate() error {
	if s.PublicURL != "" {
		if _, err := validateSourceURL(PrometheusType, s.PublicURL); err != nil {
			return fmt.Errorf("publicUrl: %w", err)
		}
	}
	return s.validate(PrometheusType)
}

func (s HTTPSource) validate(kind string) error {
	parsed, err := validateSourceURL(kind, s.URL)
	if err != nil {
		return err
	}
	if s.EvaluationInterval <= 0 {
		return fmt.Errorf("%s evaluation interval must be positive", kind)
	}
	if s.Timeout <= 0 {
		return fmt.Errorf("%s timeout must be positive", kind)
	}
	if s.Timeout >= s.EvaluationInterval {
		return fmt.Errorf("%s timeout must be shorter than the evaluation interval", kind)
	}
	if s.MaximumConcurrency <= 0 || s.MaximumConcurrency > MaximumSourceConcurrency {
		return fmt.Errorf(
			"%s maximum concurrency must be between 1 and %d", kind,
			MaximumSourceConcurrency,
		)
	}

	files := []struct {
		description string
		path        string
	}{
		{description: "bearer token file", path: s.BearerTokenFile},
		{description: "TLS CA file", path: s.TLS.CAFile},
		{description: "TLS client certificate", path: s.TLS.ClientCertificateFile},
		{description: "TLS client key", path: s.TLS.ClientKeyFile},
	}
	for _, file := range files {
		if file.path != "" && !filepath.IsAbs(file.path) {
			return fmt.Errorf("%s %s must be an absolute path", kind, file.description)
		}
	}
	if (s.TLS.ClientCertificateFile == "") != (s.TLS.ClientKeyFile == "") {
		return fmt.Errorf("%s TLS client certificate and key files must be configured together", kind)
	}
	if parsed.Scheme != "https" && s.TLS != (TLSConfig{}) {
		return fmt.Errorf("%s TLS configuration requires an HTTPS URL", kind)
	}

	return nil
}

func validateSourceURL(kind, value string) (*url.URL, error) {
	if strings.TrimSpace(value) == "" {
		return nil, fmt.Errorf("%s URL is required", kind)
	}
	if strings.TrimSpace(value) != value {
		return nil, fmt.Errorf("%s URL must not have leading or trailing whitespace", kind)
	}

	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Hostname() == "" || parsed.Opaque != "" {
		return nil, fmt.Errorf("%s URL must be an absolute URL", kind)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("%s URL must use HTTP or HTTPS", kind)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || strings.Contains(value, "#") {
		return nil, fmt.Errorf("%s URL must not contain credentials, a query, or a fragment", kind)
	}

	return parsed, nil
}
