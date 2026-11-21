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

// SourceSet is the administrator-configured collection of Gate metric sources.
type SourceSet map[string]Source

type Source struct {
	Prometheus PrometheusSource
}

type PrometheusSource struct {
	URL                string
	PublicURL          string
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
	definition := requirement.Prometheus
	if definition == nil || requirement.State == RequirementStopped {
		return nil
	}
	publicURL := s[definition.Source].Prometheus.PublicURL
	if publicURL == "" {
		return nil
	}
	base, err := validatePrometheusURL(publicURL)
	if err != nil {
		return nil
	}
	viewer := base.JoinPath("query")
	params := viewer.Query()
	params.Set("g0.expr", definition.Query)
	params.Set("g0.tab", "table")

	evaluatedAt := requirement.EvaluatedAt
	if evaluatedAt == nil && requirement.State == RequirementReleased && len(requirement.Overrides) > 0 {
		last := requirement.Overrides[len(requirement.Overrides)-1]
		if last.DefinitionDigest == requirement.DefinitionDigest {
			evaluatedAt = last.EvaluatedAt
		}
	}
	if evaluatedAt != nil {
		params.Set("g0.moment_input", evaluatedAt.UTC().Format("2006-01-02T15:04:05.000Z"))
	}
	viewer.RawQuery = params.Encode()
	return new(viewer.String())
}

func (s SourceSet) Validate() error {
	for _, name := range s.Names() {
		if err := kubevalidate.DNSLabel("Gate source name", name); err != nil {
			return err
		}
		if err := s[name].Prometheus.validate(); err != nil {
			return fmt.Errorf("validate Gate source %q: %w", name, err)
		}
	}

	return nil
}

func (s PrometheusSource) validate() error {
	parsed, err := validatePrometheusURL(s.URL)
	if err != nil {
		return err
	}
	if s.PublicURL != "" {
		if _, err := validatePrometheusURL(s.PublicURL); err != nil {
			return fmt.Errorf("publicUrl: %w", err)
		}
	}
	if s.EvaluationInterval <= 0 {
		return fmt.Errorf("prometheus evaluation interval must be positive")
	}
	if s.Timeout <= 0 {
		return fmt.Errorf("prometheus timeout must be positive")
	}
	if s.Timeout >= s.EvaluationInterval {
		return fmt.Errorf("prometheus timeout must be shorter than the evaluation interval")
	}
	if s.MaximumConcurrency <= 0 || s.MaximumConcurrency > MaximumSourceConcurrency {
		return fmt.Errorf(
			"prometheus maximum concurrency must be between 1 and %d",
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
			return fmt.Errorf("prometheus %s must be an absolute path", file.description)
		}
	}
	if (s.TLS.ClientCertificateFile == "") != (s.TLS.ClientKeyFile == "") {
		return fmt.Errorf("prometheus TLS client certificate and key files must be configured together")
	}
	if parsed.Scheme != "https" && s.TLS != (TLSConfig{}) {
		return fmt.Errorf("prometheus TLS configuration requires an HTTPS URL")
	}

	return nil
}

func validatePrometheusURL(value string) (*url.URL, error) {
	if strings.TrimSpace(value) == "" {
		return nil, fmt.Errorf("prometheus URL is required")
	}
	if strings.TrimSpace(value) != value {
		return nil, fmt.Errorf("prometheus URL must not have leading or trailing whitespace")
	}

	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Hostname() == "" || parsed.Opaque != "" {
		return nil, fmt.Errorf("prometheus URL must be an absolute URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("prometheus URL must use HTTP or HTTPS")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || strings.Contains(value, "#") {
		return nil, fmt.Errorf("prometheus URL must not contain credentials, a query, or a fragment")
	}

	return parsed, nil
}
