package gate_test

import (
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/slipramp/slipramp/internal/gate"
	"github.com/slipramp/slipramp/internal/testcheck"
)

func TestDecodeSourceFile(t *testing.T) {
	t.Parallel()

	const header = `apiVersion: slipramp.com/v1alpha1
kind: GateSourceConfig
`
	for _, test := range []struct {
		name  string
		input string
		want  gate.SourceSet
	}{
		{name: "no sources", input: "sources: {}\n", want: gate.SourceSet{}},
		{
			name: "defaults",
			input: `sources:
  homelab:
    prometheus:
      url: http://prometheus:9090
`,
			want: gate.SourceSet{"homelab": {Prometheus: gate.PrometheusSource{
				URL:                "http://prometheus:9090",
				EvaluationInterval: gate.DefaultEvaluationInterval,
				Timeout:            gate.DefaultRequestTimeout,
				MaximumConcurrency: gate.DefaultMaximumConcurrency,
			}}},
		},
		{
			name: "explicit polling and transport settings",
			input: `sources:
  production:
    prometheus:
      url: https://prometheus.example.com/metrics
      publicUrl: https://metrics.example.com/prometheus
      evaluationInterval: 30s
      timeout: 8s
      maximumConcurrency: 7
      bearerTokenFile: /var/run/slipramp/gates/token
      tls:
        caFile: /var/run/slipramp/gates/ca.crt
        clientCertificateFile: /var/run/slipramp/gates/tls.crt
        clientKeyFile: /var/run/slipramp/gates/tls.key
`,
			want: gate.SourceSet{"production": {Prometheus: gate.PrometheusSource{
				URL:                "https://prometheus.example.com/metrics",
				PublicURL:          "https://metrics.example.com/prometheus",
				EvaluationInterval: 30 * time.Second,
				Timeout:            8 * time.Second,
				MaximumConcurrency: 7,
				BearerTokenFile:    "/var/run/slipramp/gates/token",
				TLS: gate.TLSConfig{
					CAFile:                "/var/run/slipramp/gates/ca.crt",
					ClientCertificateFile: "/var/run/slipramp/gates/tls.crt",
					ClientKeyFile:         "/var/run/slipramp/gates/tls.key",
				},
			}}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			sources, err := gate.DecodeSourceFile(strings.NewReader(header + test.input))
			if err != nil {
				t.Fatalf("DecodeSourceFile() error = %v", err)
			}
			if !maps.Equal(sources, test.want) {
				t.Errorf("sources = %#v, want %#v", sources, test.want)
			}
		})
	}
}

func TestDecodeSourceFileRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		configure func(string) string
		wantError string
	}{
		{
			name:      "unknown field",
			configure: func(input string) string { return input + "      extra: true\n" },
			wantError: "field extra not found",
		},
		{
			name:      "multiple documents",
			configure: func(input string) string { return input + "---\n{}\n" },
			wantError: "exactly one document",
		},
		{
			name: "wrong apiVersion",
			configure: func(input string) string {
				return strings.Replace(input, "slipramp.com/v1alpha1", "slipramp.com/v1", 1)
			},
			wantError: "apiVersion",
		},
		{
			name: "wrong kind",
			configure: func(input string) string {
				return strings.Replace(input, "GateSourceConfig", "Sources", 1)
			},
			wantError: "kind",
		},
		{
			name: "missing sources",
			configure: func(input string) string {
				return input[:strings.Index(input, "sources:")]
			},
			wantError: "must define sources",
		},
		{
			name: "invalid source name",
			configure: func(input string) string {
				return strings.Replace(input, "homelab:", "HomeLab:", 1)
			},
			wantError: "Gate source name",
		},
		{
			name: "relative URL",
			configure: func(input string) string {
				return strings.Replace(input, "http://prometheus:9090", "/prometheus", 1)
			},
			wantError: "absolute URL",
		},
		{
			name: "URL credentials",
			configure: func(input string) string {
				return strings.Replace(input, "prometheus:9090", "user:pass@prometheus:9090", 1)
			},
			wantError: "credentials, a query, or a fragment",
		},
		{
			name: "URL query",
			configure: func(input string) string {
				return strings.Replace(input, "prometheus:9090", "prometheus:9090?tenant=one", 1)
			},
			wantError: "credentials, a query, or a fragment",
		},
		{
			name: "public URL with credentials",
			configure: func(input string) string {
				return input + "      publicUrl: https://user:password@metrics.example.com\n"
			},
			wantError: "publicUrl: prometheus URL must not contain credentials",
		},
		{
			name: "invalid interval",
			configure: func(input string) string {
				return input + "      evaluationInterval: soon\n"
			},
			wantError: "evaluation interval",
		},
		{
			name: "timeout equals interval",
			configure: func(input string) string {
				return input + "      evaluationInterval: 5s\n      timeout: 5s\n"
			},
			wantError: "shorter than the evaluation interval",
		},
		{
			name: "excessive concurrency",
			configure: func(input string) string {
				return input + "      maximumConcurrency: 33\n"
			},
			wantError: "between 1 and 32",
		},
		{
			name: "relative bearer token file",
			configure: func(input string) string {
				return input + "      bearerTokenFile: token\n"
			},
			wantError: "bearer token file must be an absolute path",
		},
		{
			name: "empty bearer token file",
			configure: func(input string) string {
				return input + "      bearerTokenFile: \"\"\n"
			},
			wantError: "bearer token file must not be empty",
		},
		{
			name: "TLS with HTTP",
			configure: func(input string) string {
				return input + "      tls:\n        caFile: /certificates/ca.crt\n"
			},
			wantError: "requires an HTTPS URL",
		},
		{
			name: "incomplete client certificate",
			configure: func(input string) string {
				input = strings.Replace(input, "http://", "https://", 1)

				return input + "      tls:\n        clientCertificateFile: /certificates/tls.crt\n"
			},
			wantError: "certificate and key files must be configured together",
		},
	}

	const valid = `apiVersion: slipramp.com/v1alpha1
kind: GateSourceConfig
sources:
  homelab:
    prometheus:
      url: http://prometheus:9090
`
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := gate.DecodeSourceFile(strings.NewReader(test.configure(valid)))
			testcheck.ErrorContains(t, err, test.wantError)
		})
	}
}
