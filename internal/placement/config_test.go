package placement

import (
	"strings"
	"testing"
	"time"

	"github.com/slipramp/slipramp/internal/testcheck"
	corev1 "k8s.io/api/core/v1"
)

func TestValidateName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		placementName string
		wantError     bool
	}{
		{name: "valid", placementName: "infrastructure-admin"},
		{name: "empty", placementName: "", wantError: true},
		{name: "uppercase", placementName: "Infrastructure", wantError: true},
		{name: "ends with hyphen", placementName: "infrastructure-", wantError: true},
		{
			name:          "too long",
			placementName: "a" + strings.Repeat("2", 63),
			wantError:     true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateName(test.placementName)
			if test.wantError && err == nil {
				t.Fatal("ValidateName() error = nil, want an error")
			}

			if !test.wantError && err != nil {
				t.Fatalf("ValidateName() error = %v", err)
			}
		})
	}
}

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		configure func(*Config)
		wantError string
	}{
		{name: "minimal"},
		{
			name: "complete",
			configure: func(cfg *Config) {
				cfg.ServiceAccountName = "infrastructure-admin"
				cfg.Timeout = 30 * time.Minute
				cfg.Resources = ResourceRequirements{
					Requests: map[string]string{
						"cpu":    "100m",
						"memory": "128Mi",
					},
					Limits: map[string]string{
						"example.com/accelerator": "1",
					},
				}
				cfg.NodeSelector = map[string]string{
					"topology.kubernetes.io/zone": "us-west-2a",
				}
				cfg.Credentials = map[string]CredentialSource{
					"terraform-backend": {
						SecretRef: SecretReference{Name: "terraform.backend"},
					},
				}
			},
		},
		{
			name: "invalid cluster",
			configure: func(cfg *Config) {
				cfg.ClusterName = "Invalid Cluster"
			},
			wantError: "cluster name",
		},
		{
			name: "empty namespace",
			configure: func(cfg *Config) {
				cfg.JobNamespace = ""
			},
			wantError: "job namespace",
		},
		{
			name: "invalid service account",
			configure: func(cfg *Config) {
				cfg.ServiceAccountName = "Infrastructure Admin"
			},
			wantError: "service account",
		},
		{
			name: "long service account",
			configure: func(cfg *Config) {
				cfg.ServiceAccountName = strings.Repeat("a", 64)
			},
		},
		{
			name: "negative timeout",
			configure: func(cfg *Config) {
				cfg.Timeout = -time.Second
			},
			wantError: "timeout",
		},
		{
			name: "invalid resource name",
			configure: func(cfg *Config) {
				cfg.Resources.Requests = map[string]string{"invalid resource": "1"}
			},
			wantError: "resource request name",
		},
		{
			name: "malformed resource quantity",
			configure: func(cfg *Config) {
				cfg.Resources.Limits = map[string]string{"memory": "many"}
			},
			wantError: "invalid quantity",
		},
		{
			name: "negative resource quantity",
			configure: func(cfg *Config) {
				cfg.Resources.Limits = map[string]string{"memory": "-1Gi"}
			},
			wantError: "must not be negative",
		},
		{
			name: "request exceeds limit",
			configure: func(cfg *Config) {
				cfg.Resources.Requests = map[string]string{"memory": "2Gi"}
				cfg.Resources.Limits = map[string]string{"memory": "1Gi"}
			},
			wantError: "must not exceed",
		},
		{
			name: "invalid node selector key",
			configure: func(cfg *Config) {
				cfg.NodeSelector = map[string]string{"invalid key": "value"}
			},
			wantError: "node selector key",
		},
		{
			name: "invalid node selector value",
			configure: func(cfg *Config) {
				cfg.NodeSelector = map[string]string{"example.com/node": "invalid value"}
			},
			wantError: "node selector value",
		},
		{
			name: "invalid credential name",
			configure: func(cfg *Config) {
				cfg.Credentials = map[string]CredentialSource{
					"Terraform Backend": {
						SecretRef: SecretReference{Name: "terraform-backend"},
					},
				}
			},
			wantError: "credential source name",
		},
		{
			name: "pod template",
			configure: func(cfg *Config) {
				cfg.PodTemplate = &corev1.PodTemplateSpec{
					Labels: map[string]string{
						"azure.workload.identity/use": "true",
					},
					Spec: corev1.PodSpec{
						RuntimeClassName: new("kata"),
					},
				}
			},
		},
		{
			name: "invalid pod template label",
			configure: func(cfg *Config) {
				cfg.PodTemplate = &corev1.PodTemplateSpec{
					Labels: map[string]string{"invalid key": "value"},
				}
			},
			wantError: "pod template label key",
		},
		{
			name: "protected pod template containers",
			configure: func(cfg *Config) {
				cfg.PodTemplate = &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "bypass"}},
					},
				}
			},
			wantError: "must not define containers",
		},
		{
			name: "invalid secret name",
			configure: func(cfg *Config) {
				cfg.Credentials = map[string]CredentialSource{
					"terraform-backend": {
						SecretRef: SecretReference{Name: "Terraform Backend"},
					},
				}
			},
			wantError: "secret name",
		},
		{
			name: "long secret name",
			configure: func(cfg *Config) {
				cfg.Credentials = map[string]CredentialSource{
					"terraform-backend": {
						SecretRef: SecretReference{
							Name: strings.Repeat("a", 64),
						},
					},
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			cfg := validConfig()
			if test.configure != nil {
				test.configure(&cfg)
			}

			testcheck.ErrorContains(t, cfg.Validate(), test.wantError)
		})
	}
}

func validConfig() Config {
	return Config{
		ClusterName:  "colo",
		JobNamespace: "slipramp-runs",
	}
}
