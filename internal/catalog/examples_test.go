package catalog_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/placement"
)

func TestKcloudOpsExample(t *testing.T) {
	t.Parallel()

	exampleRoot := filepath.Join("..", "..", "examples", "kcloud-ops")
	credentials := map[string]placement.CredentialSource{
		"homelab-ssh": {
			SecretRef: placement.SecretReference{Name: "kcloud-ops-ssh"},
		},
		"rustfs-admin": {
			SecretRef: placement.SecretReference{Name: "rustfs-admin"},
		},
		"rustfs-terraform-state": {
			SecretRef: placement.SecretReference{Name: "rustfs-terraform-state"},
		},
	}
	placements, err := placement.NewSet(map[string]placement.Config{
		"homelab-infrastructure": {
			ClusterName:  "homelab",
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

	if len(loaded.Runtimes) != 2 {
		t.Errorf("len(Runtimes) = %d, want 2", len(loaded.Runtimes))
	}
	if len(loaded.Targets) != 2 {
		t.Fatalf("len(Targets) = %d, want 2", len(loaded.Targets))
	}

	targets := make(map[string]catalog.Target, len(loaded.Targets))
	for _, target := range loaded.Targets {
		targets[target.Metadata.Name] = target
	}

	nodeExporter := targets["homelab-node-exporter"]
	if len(nodeExporter.Spec.Actions) != 3 {
		t.Errorf(
			"node_exporter action count = %d, want 3",
			len(nodeExporter.Spec.Actions),
		)
	}
	preview := nodeExporter.Spec.Actions["preview"]
	wantPreviewCommand := []string{
		"./slipramp/node-exporter.sh",
		"plan",
		"exporter_nodes",
	}
	if !slices.Equal(preview.Command, wantPreviewCommand) {
		t.Errorf(
			"node_exporter preview command = %q, want %q",
			preview.Command,
			wantPreviewCommand,
		)
	}

	rustfs := targets["rustfs-homelab"]
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
}
