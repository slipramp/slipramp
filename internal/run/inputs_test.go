package run

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/catalog"
)

func TestCreateRunPinsArtifactInputToProducerRevision(t *testing.T) {
	t.Parallel()

	service, runs := testService(t)
	revision := testRevision()
	target := &revision.Catalog.Targets[0]
	preview := target.Spec.Actions["preview"]
	preview.Artifacts = map[string]catalog.Artifact{
		"plan": {Path: "/artifacts/plan.tfplan"},
	}
	target.Spec.Actions["preview"] = preview
	target.Spec.Actions["apply"] = catalog.Action{
		Command: []string{"terraform", "apply"},
		Inputs: map[string]catalog.Input{
			"plan": {
				Artifact:  catalog.ArtifactReference{FromAction: "preview"},
				MountPath: "/inputs/plan.tfplan",
			},
		},
	}
	service.catalogs = &recordingCatalogs{revision: revision}
	artifactID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1300")
	producerRunID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1301")
	runs.artifacts = map[uuid.UUID]ArtifactInput{
		artifactID: {
			ArtifactID: artifactID, ProducerRunID: producerRunID, Name: "plan",
			MediaType: "application/octet-stream", SizeBytes: 12,
			SHA256: strings.Repeat("a", 64), RetentionState: "retained",
			RepositoryID: testRepositoryID, CatalogRevisionID: testRevisionID,
			CommitSHA: testCommitSHA, TargetName: "network", ActionName: "preview",
		},
	}

	created, err := service.CreateRun(context.Background(), CreateRequest{
		RepositoryID: testRepositoryID,
		TargetName:   "network",
		ActionName:   "apply",
		Inputs:       map[string]uuid.UUID{"plan": artifactID},
		Source:       Source{Type: SourceTypeManual},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	input := created.Snapshot.Inputs["plan"]
	if created.CatalogRevisionID != testRevisionID || created.CommitSHA != testCommitSHA ||
		input.ArtifactID != artifactID || input.ProducerRunID != producerRunID {
		t.Errorf("created input-pinned Run = %#v", created)
	}
	if runs.created.Snapshot.Inputs["plan"].SHA256 != strings.Repeat("a", 64) {
		t.Errorf("persisted input = %#v", runs.created.Snapshot.Inputs["plan"])
	}
}

func TestCreateRunRejectsIncompatibleArtifactInput(t *testing.T) {
	t.Parallel()

	service, runs := testService(t)
	revision := testRevision()
	revision.Catalog.Targets[0].Spec.Actions["apply"] = catalog.Action{
		Command: []string{"terraform", "apply"},
		Inputs: map[string]catalog.Input{
			"plan": {
				Artifact:  catalog.ArtifactReference{FromAction: "preview"},
				MountPath: "/inputs/plan.tfplan",
			},
		},
	}
	service.catalogs = &recordingCatalogs{revision: revision}
	artifactID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1300")
	runs.artifacts = map[uuid.UUID]ArtifactInput{
		artifactID: {
			ArtifactID: artifactID, ProducerRunID: uuid.New(), Name: "plan",
			SHA256: strings.Repeat("a", 64), RetentionState: "retained",
			RepositoryID: testRepositoryID, CatalogRevisionID: testRevisionID,
			CommitSHA: testCommitSHA, TargetName: "network", ActionName: "destroy",
		},
	}

	_, err := service.CreateRun(context.Background(), CreateRequest{
		RepositoryID: testRepositoryID,
		TargetName:   "network",
		ActionName:   "apply",
		Inputs:       map[string]uuid.UUID{"plan": artifactID},
		Source:       Source{Type: SourceTypeAPI},
	})
	if !errors.Is(err, ErrInvalidInputs) {
		t.Fatalf("CreateRun() error = %v, want invalid inputs", err)
	}
	if runs.calls != 0 {
		t.Errorf("persistence calls = %d, want 0", runs.calls)
	}
}
