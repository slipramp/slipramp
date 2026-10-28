package placement

import (
	"regexp"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
)

func TestSetOwnsConfig(t *testing.T) {
	t.Parallel()

	configured := map[string]Config{
		"infrastructure": {
			ClusterName:  "colo",
			JobNamespace: "slipramp-runs",
			NodeSelector: map[string]string{"region": "west"},
			Resources: ResourceRequirements{
				Requests: map[string]string{"cpu": "100m"},
			},
			PodTemplate: &corev1.PodTemplateSpec{
				Labels: map[string]string{"identity": "enabled"},
			},
		},
	}

	placements, err := NewSet(configured)
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}

	configured["infrastructure"].NodeSelector["region"] = "east"
	cfg, ok := placements.Lookup("infrastructure")
	if !ok {
		t.Fatal("Lookup() found = false, want true")
	}

	if cfg.NodeSelector["region"] != "west" {
		t.Errorf("NodeSelector[region] = %q, want %q", cfg.NodeSelector["region"], "west")
	}

	cfg.Resources.Requests["cpu"] = "2"
	cfg.PodTemplate.Labels["identity"] = "disabled"
	cfg, _ = placements.Lookup("infrastructure")
	if cfg.Resources.Requests["cpu"] != "100m" {
		t.Errorf("Resources.Requests[cpu] = %q, want %q", cfg.Resources.Requests["cpu"], "100m")
	}

	if cfg.PodTemplate.Labels["identity"] != "enabled" {
		t.Errorf("PodTemplate.Labels[identity] = %q, want %q", cfg.PodTemplate.Labels["identity"], "enabled")
	}
}

func TestNewSetRejectsInvalidPlacement(t *testing.T) {
	t.Parallel()

	_, err := NewSet(map[string]Config{"Infrastructure": validConfig()})
	if err == nil {
		t.Fatal("NewSet() error = nil, want an error")
	}
}

func TestSetChecksumIsDeterministic(t *testing.T) {
	t.Parallel()

	first, err := NewSet(map[string]Config{
		"preview": {
			ClusterName:  "development",
			JobNamespace: "previews",
		},
		"infrastructure": {
			ClusterName:        "colo",
			JobNamespace:       "slipramp-runs",
			ServiceAccountName: "infrastructure-admin",
			Timeout:            30 * time.Minute,
			Resources: ResourceRequirements{
				Requests: map[string]string{"memory": "128Mi", "cpu": "100m"},
				Limits:   map[string]string{"memory": "1Gi", "cpu": "1"},
			},
			NodeSelector: map[string]string{"region": "west", "pool": "operations"},
			Credentials: map[string]CredentialSource{
				"terraform-backend": {SecretRef: SecretReference{Name: "terraform-backend"}},
				"cloud":             {SecretRef: SecretReference{Name: "cloud-credentials"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewSet() first error = %v", err)
	}

	second, err := NewSet(map[string]Config{
		"infrastructure": {
			ClusterName:        "colo",
			JobNamespace:       "slipramp-runs",
			ServiceAccountName: "infrastructure-admin",
			Timeout:            30 * time.Minute,
			Resources: ResourceRequirements{
				Requests: map[string]string{"cpu": "100m", "memory": "128Mi"},
				Limits:   map[string]string{"cpu": "1", "memory": "1Gi"},
			},
			NodeSelector: map[string]string{"pool": "operations", "region": "west"},
			Credentials: map[string]CredentialSource{
				"cloud":             {SecretRef: SecretReference{Name: "cloud-credentials"}},
				"terraform-backend": {SecretRef: SecretReference{Name: "terraform-backend"}},
			},
		},
		"preview": {
			ClusterName:  "development",
			JobNamespace: "previews",
		},
	})
	if err != nil {
		t.Fatalf("NewSet() second error = %v", err)
	}

	if first.Checksum() != second.Checksum() {
		t.Errorf("Checksum() = %q, want %q", first.Checksum(), second.Checksum())
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(first.Checksum()) {
		t.Errorf("Checksum() = %q, want lowercase SHA-256", first.Checksum())
	}
}

func TestSetChecksumChangesWithEffectiveConfiguration(t *testing.T) {
	t.Parallel()

	first, err := NewSet(map[string]Config{"infrastructure": validConfig()})
	if err != nil {
		t.Fatalf("NewSet() first error = %v", err)
	}

	changed := validConfig()
	changed.JobNamespace = "other-runs"
	second, err := NewSet(map[string]Config{"infrastructure": changed})
	if err != nil {
		t.Fatalf("NewSet() second error = %v", err)
	}

	if first.Checksum() == second.Checksum() {
		t.Errorf("Checksum() = %q after effective configuration changed", first.Checksum())
	}
}

func TestZeroSetHasEmptyConfigurationChecksum(t *testing.T) {
	t.Parallel()

	empty, err := NewSet(nil)
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}

	if (Set{}).Checksum() != empty.Checksum() {
		t.Errorf("zero Checksum() = %q, want %q", (Set{}).Checksum(), empty.Checksum())
	}
}
