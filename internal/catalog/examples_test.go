package catalog_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/placement"
)

func TestInfrastructureOpsExample(t *testing.T) {
	t.Parallel()

	exampleRoot := filepath.Join("..", "..", "examples", "infrastructure-ops")
	portable, err := catalog.Process(os.DirFS(exampleRoot), catalog.Options{})
	if err != nil {
		t.Fatalf("portable catalog.Process() error = %v", err)
	}
	if portable.Level != catalog.ValidationLevelPortable {
		t.Errorf("portable level = %q, want %q", portable.Level, catalog.ValidationLevelPortable)
	}
	if portable.Diagnostics.HasErrors() {
		t.Fatalf("portable catalog.Process() diagnostics = %s", portable.Diagnostics.ErrorSummary())
	}

	placements, err := placement.LoadFile(filepath.Join(exampleRoot, "placements.yaml"))
	if err != nil {
		t.Fatalf("placement.LoadFile() error = %v", err)
	}

	result, err := catalog.Process(os.DirFS(exampleRoot), catalog.Options{Placements: &placements})
	if err != nil {
		t.Fatalf("deployment catalog.Process() error = %v", err)
	}
	if result.Level != catalog.ValidationLevelDeployment {
		t.Errorf("deployment level = %q, want %q", result.Level, catalog.ValidationLevelDeployment)
	}
	if result.Diagnostics.HasErrors() {
		t.Fatalf("deployment catalog.Process() diagnostics = %s", result.Diagnostics.ErrorSummary())
	}
	loaded := result.Catalog

	var ansible, rustfs *catalog.Target
	for _, target := range loaded.Targets {
		switch target.Metadata.Name {
		case "ansible-baseline":
			ansible = &target
		case "rustfs-buckets":
			rustfs = &target
		}
	}
	if ansible == nil {
		t.Fatal("ansible-baseline target not found")
	}
	if ansible.Spec.Credentials["ssh"] != "ansible-ssh" ||
		ansible.Spec.Credentials["ssh-known-hosts"] != "ssh-known-hosts" {
		t.Errorf("ansible credentials = %#v", ansible.Spec.Credentials)
	}
	direct, found := ansible.EffectiveAction("direct-playbook")
	if !found {
		t.Fatal("ansible direct-playbook action not found")
	}
	wantDirectCommand := []string{"ansible-playbook", "--inventory", "inventory/hosts.yml", "site.yml"}
	if !reflect.DeepEqual(direct.Command, wantDirectCommand) {
		t.Errorf("ansible direct-playbook command = %#v, want %#v", direct.Command, wantDirectCommand)
	}
	if len(direct.Artifacts) != 0 {
		t.Errorf("ansible direct-playbook artifacts = %#v, want none", direct.Artifacts)
	}
	wantCommands := map[string][]string{
		"preview": {"slipramp-ansible", "preview", "site.yml"},
		"canary": {
			"slipramp-ansible",
			"apply",
			"--limit",
			"node01.example.com",
			"site.yml",
		},
		"sync": {"slipramp-ansible", "apply", "site.yml"},
	}
	for name, wantCommand := range wantCommands {
		action, found := ansible.EffectiveAction(name)
		if !found {
			t.Errorf("ansible %s action not found", name)
			continue
		}
		if !reflect.DeepEqual(action.Command, wantCommand) {
			t.Errorf("ansible %s command = %#v, want %#v", name, action.Command, wantCommand)
		}
	}
	for name, action := range ansible.Spec.Actions {
		if len(action.Artifacts) != 0 {
			t.Errorf("ansible action %q advertises artifacts before the report contract: %#v", name, action.Artifacts)
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
