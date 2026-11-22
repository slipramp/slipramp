package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/slipramp/slipramp/internal/config"
	"github.com/slipramp/slipramp/internal/gate"
	"github.com/slipramp/slipramp/internal/testcheck"
)

const serverGateSourcesFileEnvironment = "SLIPRAMP_GATE_SOURCES_FILE"

func TestLoadServerGateSources(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "gate-sources.yaml")
	if err := os.WriteFile(path, []byte(`
apiVersion: slipramp.com/v1alpha1
kind: GateSourceConfig
sources:
  homelab:
    type: prometheus
    url: http://prometheus:9090
    evaluationInterval: 20s
    timeout: 4s
`), 0o600); err != nil {
		t.Fatal(err)
	}
	environment := requiredServerEnvironment()
	environment[serverGateSourcesFileEnvironment] = path

	cfg, err := config.LoadServer(mapEnvironment(environment))
	if err != nil {
		t.Fatalf("LoadServer() error = %v", err)
	}
	source, ok := cfg.GateSources["homelab"].(gate.PrometheusSource)
	if !ok || source.EvaluationInterval != 20*time.Second ||
		source.Timeout != 4*time.Second {
		t.Errorf("GateSources[homelab] = %#v, %t", source, ok)
	}
}

func TestLoadServerRejectsInvalidGateSources(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "gate-sources.yaml")
	if err := os.WriteFile(path, []byte("not: valid: yaml"), 0o600); err != nil {
		t.Fatal(err)
	}
	environment := requiredServerEnvironment()
	environment[serverGateSourcesFileEnvironment] = path

	_, err := config.LoadServer(mapEnvironment(environment))
	testcheck.ErrorContains(t, err, serverGateSourcesFileEnvironment)
}

func TestLocalServerGateSources(t *testing.T) {
	environment := requiredServerEnvironment()
	environment[serverGateSourcesFileEnvironment] = filepath.Join("..", "..", "deploy", "local", "gate-sources.yaml")
	cfg, err := config.LoadServer(mapEnvironment(environment))
	if err != nil || len(cfg.GateSources) == 0 {
		t.Fatalf("load local Server Gate sources: %v", err)
	}
}
