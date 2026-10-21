package placement

import (
	"testing"

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
