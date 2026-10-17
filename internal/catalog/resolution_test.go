package catalog_test

import (
	"testing"

	"github.com/slipramp/slipramp/internal/catalog"
)

func TestEffectiveActionAppliesDefaultsWithoutMutatingTarget(t *testing.T) {
	t.Parallel()

	input := validCatalog()
	target := input.Targets[0]
	target.Spec.Timeout = "30m"
	target.Spec.Credentials = map[string]string{
		"backend": "default-backend",
		"ssh":     "default-ssh",
	}
	target.Spec.Resources = catalog.ResourceRequirements{
		Requests: map[string]string{"cpu": "100m", "memory": "128Mi"},
		Limits:   map[string]string{"memory": "1Gi"},
	}
	target.Spec.Actions["preview"] = catalog.Action{
		Command:     []string{"terraform", "plan"},
		Placement:   "preview-cluster",
		Credentials: map[string]string{"backend": "preview-backend"},
		Resources: catalog.ResourceRequirements{
			Requests: map[string]string{"cpu": "250m"},
		},
		Parameters: map[string]catalog.Parameter{
			"mode": {
				Type:     catalog.ParameterTypeEnum,
				Values:   []string{"safe", "fast"},
				Argument: &catalog.ParameterArgument{Name: "--mode"},
			},
		},
		Inputs: map[string]catalog.Input{
			"plan": {
				Artifact:  catalog.ArtifactReference{FromAction: "preview"},
				MountPath: "/inputs/plan",
			},
		},
	}

	effective, found := target.EffectiveAction("preview")
	if !found {
		t.Fatal("EffectiveAction() found = false")
	}
	if effective.Runtime != "terraform" ||
		effective.Placement != "preview-cluster" ||
		effective.Timeout != "30m" ||
		effective.Credentials["backend"] != "preview-backend" ||
		effective.Credentials["ssh"] != "default-ssh" ||
		effective.Resources.Requests["cpu"] != "250m" ||
		effective.Resources.Requests["memory"] != "128Mi" ||
		effective.Resources.Limits["memory"] != "1Gi" ||
		effective.Inputs["plan"].Artifact.Compatibility !=
			catalog.ArtifactCompatibilityExactCatalogRevision {
		t.Errorf("EffectiveAction() = %#v", effective)
	}

	effective.Command[0] = "changed"
	effective.Credentials["backend"] = "changed"
	parameter := effective.Parameters["mode"]
	parameter.Values[0] = "changed"
	parameter.Argument.Name = "--changed"
	effective.Parameters["mode"] = parameter

	original := target.Spec.Actions["preview"]
	if original.Command[0] != "terraform" ||
		target.Spec.Credentials["backend"] != "default-backend" ||
		original.Parameters["mode"].Values[0] != "safe" ||
		original.Parameters["mode"].Argument.Name != "--mode" {
		t.Fatal("EffectiveAction() mutated the source target")
	}
}
