package catalog_test

import (
	"os"
	"path/filepath"
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

	result, err := catalog.Process(os.DirFS(exampleRoot), catalog.Options{Placements: &placements})
	if err != nil {
		t.Fatalf("catalog.Process() error = %v", err)
	}
	if result.Diagnostics.HasErrors() {
		t.Fatalf("catalog.Process() diagnostics = %s", result.Diagnostics.ErrorSummary())
	}
	loaded := result.Catalog

	var rustfs *catalog.Target
	for _, target := range loaded.Targets {
		if target.Metadata.Name == "rustfs-buckets" {
			rustfs = &target
			break
		}
	}
	if rustfs == nil {
		t.Fatal("rustfs-buckets target not found")
	}
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

	syncAction, found := rustfs.EffectiveAction("sync")
	if !found {
		t.Fatal("rustfs sync action not found")
	}
	if len(syncAction.Triggers) != 1 || syncAction.Triggers[0].Review != catalog.TriggerReviewRequired {
		t.Errorf("rustfs sync triggers = %#v, want one review-required trigger", syncAction.Triggers)
	}
}
