package gate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/slipramp/slipramp/internal/gate"
	"github.com/slipramp/slipramp/internal/testcheck"
)

func TestDecodeSourceFileAppliesDefaults(t *testing.T) {
	t.Parallel()

	sources, err := gate.DecodeSourceFile(strings.NewReader(`
apiVersion: slipramp.com/v1alpha1
kind: GateSourceConfig
sources:
  homelab:
    prometheus:
      url: http://prometheus:9090
`))
	if err != nil {
		t.Fatalf("DecodeSourceFile() error = %v", err)
	}

	source, ok := sources.Lookup("homelab")
	if !ok {
		t.Fatal("homelab source not found")
	}
	if source.Prometheus.URL != "http://prometheus:9090" ||
		source.Prometheus.EvaluationInterval != gate.DefaultEvaluationInterval ||
		source.Prometheus.Timeout != gate.DefaultRequestTimeout ||
		source.Prometheus.MaximumConcurrency != gate.DefaultMaximumConcurrency {
		t.Errorf("Prometheus source = %#v", source.Prometheus)
	}
	if len(sources.Names()) != 1 || sources.Names()[0] != "homelab" {
		t.Errorf("source names = %v", sources.Names())
	}
}

func TestDecodeSourceFileReadsTransportConfiguration(t *testing.T) {
	t.Parallel()

	sources, err := gate.DecodeSourceFile(strings.NewReader(`
apiVersion: slipramp.com/v1alpha1
kind: GateSourceConfig
sources:
  production:
    prometheus:
      url: https://prometheus.example.com/metrics
      evaluationInterval: 30s
      timeout: 8s
      maximumConcurrency: 7
      bearerTokenFile: /var/run/slipramp/gates/token
      tls:
        caFile: /var/run/slipramp/gates/ca.crt
        clientCertificateFile: /var/run/slipramp/gates/tls.crt
        clientKeyFile: /var/run/slipramp/gates/tls.key
`))
	if err != nil {
		t.Fatalf("DecodeSourceFile() error = %v", err)
	}

	source, ok := sources.Lookup("production")
	if !ok {
		t.Fatal("production source not found")
	}
	prometheus := source.Prometheus
	if prometheus.EvaluationInterval != 30*time.Second || prometheus.Timeout != 8*time.Second ||
		prometheus.MaximumConcurrency != 7 ||
		prometheus.BearerTokenFile != "/var/run/slipramp/gates/token" ||
		prometheus.TLS.CAFile != "/var/run/slipramp/gates/ca.crt" ||
		prometheus.TLS.ClientCertificateFile != "/var/run/slipramp/gates/tls.crt" ||
		prometheus.TLS.ClientKeyFile != "/var/run/slipramp/gates/tls.key" {
		t.Errorf("Prometheus source = %#v", prometheus)
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

func TestLoadSourceFileReportsReadFailure(t *testing.T) {
	t.Parallel()

	_, err := gate.LoadSourceFile(filepath.Join(t.TempDir(), "missing.yaml"))
	testcheck.ErrorContains(t, err, "read Gate source configuration")
}

func TestLoadSourceFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "gate-sources.yaml")
	if err := os.WriteFile(path, []byte(`
apiVersion: slipramp.com/v1alpha1
kind: GateSourceConfig
sources: {}
`), 0o600); err != nil {
		t.Fatal(err)
	}

	sources, err := gate.LoadSourceFile(path)
	if err != nil {
		t.Fatalf("LoadSourceFile() error = %v", err)
	}
	if len(sources) != 0 {
		t.Errorf("sources = %v, want empty", sources)
	}
}
