package catalog_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/placement"
)

func TestInfrastructureOpsExample(t *testing.T) {
	t.Parallel()

	exampleRoot := filepath.Join("..", "..", "examples", "infrastructure-ops")
	credentials := map[string]placement.CredentialSource{
		"operations": {
			SecretRef: placement.SecretReference{Name: "infrastructure-operations"},
		},
		"ssh-known-hosts": {
			SecretRef: placement.SecretReference{Name: "infrastructure-known-hosts"},
		},
	}
	placements, err := placement.NewSet(map[string]placement.Config{
		"infrastructure-admin": {
			ClusterName:  "primary",
			JobNamespace: "slipramp-runs",
			Credentials:  credentials,
		},
	})
	if err != nil {
		t.Fatalf("placement.NewSet() error = %v", err)
	}

	loaded, err := catalog.Load(os.DirFS(exampleRoot), placements)
	if err != nil {
		t.Fatalf("catalog.Load() error = %v", err)
	}

	if len(loaded.Runtimes) != 3 {
		t.Errorf("len(Runtimes) = %d, want 3", len(loaded.Runtimes))
	}
	if len(loaded.Targets) != 3 {
		t.Fatalf("len(Targets) = %d, want 3", len(loaded.Targets))
	}

	targets := make(map[string]catalog.Target, len(loaded.Targets))
	for _, target := range loaded.Targets {
		targets[target.Metadata.Name] = target
	}

	nodeExporter := targets["node-exporter"]
	if len(nodeExporter.Spec.Actions) != 3 {
		t.Errorf("node_exporter action count = %d, want 3", len(nodeExporter.Spec.Actions))
	}
	preview := nodeExporter.Spec.Actions["preview"]
	wantPreviewCommand := []string{
		"./slipramp/node-exporter.sh",
		"plan",
		"exporter_nodes",
	}
	if !slices.Equal(preview.Command, wantPreviewCommand) {
		t.Errorf("node_exporter preview command = %q, want %q", preview.Command, wantPreviewCommand)
	}

	rustfs := targets["rustfs-buckets"]
	apply, found := rustfs.EffectiveAction("apply")
	if !found {
		t.Fatal("rustfs apply action not found")
	}
	plan := apply.Inputs["plan"]
	if plan.Artifact.Compatibility != catalog.ArtifactCompatibilityExactCatalogRevision {
		t.Errorf(
			"plan compatibility = %q, want %q",
			plan.Artifact.Compatibility,
			catalog.ArtifactCompatibilityExactCatalogRevision,
		)
	}

	upgrade := targets["kubernetes-upgrade"]
	if len(upgrade.Spec.Actions) != 4 {
		t.Errorf("kubernetes upgrade action count = %d, want 4", len(upgrade.Spec.Actions))
	}
	upgradeHost := upgrade.Spec.Actions["upgrade-node"].Parameters["host"]
	if upgradeHost.Type != catalog.ParameterTypeEnum || !upgradeHost.Required {
		t.Errorf("upgrade host parameter = %#v, want required enum", upgradeHost)
	}
	previewHost := upgrade.Spec.Actions["preview"].Parameters["host"]
	if previewHost.Type != catalog.ParameterTypeEnum || !previewHost.Required {
		t.Errorf("preview host parameter = %#v, want required enum", previewHost)
	}
	if !slices.Equal(previewHost.Values, upgradeHost.Values) {
		t.Errorf("preview host values = %q, want %q", previewHost.Values, upgradeHost.Values)
	}
	for _, actionName := range []string{
		"upgrade-node",
		"upgrade-control-planes",
		"upgrade-compute",
	} {
		action := upgrade.Spec.Actions[actionName]
		aptUpgrade := action.Parameters["apt-upgrade"]
		if aptUpgrade.Type != catalog.ParameterTypeBoolean ||
			aptUpgrade.Argument == nil ||
			aptUpgrade.Argument.Name != "--apt-upgrade" {
			t.Errorf("%s apt-upgrade parameter = %#v, want --apt-upgrade boolean flag", actionName, aptUpgrade)
		}
		reboot := action.Parameters["reboot"]
		if reboot.Type != catalog.ParameterTypeBoolean ||
			reboot.Argument == nil ||
			reboot.Argument.Name != "--reboot" {
			t.Errorf("%s reboot parameter = %#v, want --reboot boolean flag", actionName, reboot)
		}
	}
}
