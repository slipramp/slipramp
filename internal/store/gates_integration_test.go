package store

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/gate"
	"github.com/slipramp/slipramp/internal/repository"
	"github.com/slipramp/slipramp/internal/run"
)

func TestGateObservationPersistenceIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	repositoryID := uuid.New()
	revisionID := uuid.New()
	evaluatedAt := time.Date(2026, time.November, 19, 12, 0, 0, 0, time.UTC)
	createGateTestRepository(t, database, repositoryID, revisionID, evaluatedAt)

	definition := persistedCatalog("1.14.3").Gates[0].NormalizedDefinition()
	digest, err := definition.Digest()
	if err != nil {
		t.Fatalf("digest Gate definition: %v", err)
	}
	_, err = database.GetGateObservation(
		ctx,
		repositoryID,
		"network-idle",
		revisionID,
		digest,
	)
	if !errors.Is(err, gate.ErrObservationNotFound) {
		t.Fatalf("missing observation error = %v", err)
	}

	observedValue := 0.0
	passing := gate.CurrentObservation{
		RepositoryID:       repositoryID,
		GateName:           "network-idle",
		CatalogRevisionID:  revisionID,
		DefinitionDigest:   digest,
		EvaluatorProcessID: uuid.New(),
		Observation: gate.Observation{
			State: gate.ObservationPassing,
			Value: &observedValue,
		},
		EvaluatedAt: evaluatedAt,
		ValidUntil:  evaluatedAt.Add(45 * time.Second),
	}
	if err := database.UpsertGateObservation(ctx, passing); err != nil {
		t.Fatalf("UpsertGateObservation() error = %v", err)
	}
	stored, err := database.GetGateObservation(
		ctx,
		repositoryID,
		passing.GateName,
		revisionID,
		digest,
	)
	if err != nil {
		t.Fatalf("GetGateObservation() error = %v", err)
	}
	if !reflect.DeepEqual(stored, passing) {
		t.Errorf("stored observation = %#v, want %#v", stored, passing)
	}

	unavailable := passing
	unavailable.EvaluatorProcessID = uuid.New()
	unavailable.Observation = gate.Observation{
		State:      gate.ObservationUnavailable,
		Diagnostic: "Prometheus request timed out.",
	}
	unavailable.EvaluatedAt = evaluatedAt.Add(time.Minute)
	unavailable.ValidUntil = unavailable.EvaluatedAt.Add(45 * time.Second)
	if err := database.UpsertGateObservation(ctx, unavailable); err != nil {
		t.Fatalf("replace Gate observation: %v", err)
	}
	stored, err = database.GetGateObservation(
		ctx,
		repositoryID,
		passing.GateName,
		revisionID,
		digest,
	)
	if err != nil {
		t.Fatalf("get replaced Gate observation: %v", err)
	}
	if !reflect.DeepEqual(stored, unavailable) {
		t.Errorf("replaced observation = %#v, want %#v", stored, unavailable)
	}
}

func TestRunGateRequirementsCommitWithRunIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	repositoryID := uuid.New()
	revisionID := uuid.New()
	createdAt := time.Date(2026, time.November, 19, 13, 0, 0, 0, time.UTC)
	createGateTestRepository(t, database, repositoryID, revisionID, createdAt)

	record := gateTestRun(t, database, repositoryID, revisionID, createdAt)
	record.Snapshot.Action.Gates = []string{"network-idle", "change-window"}
	if _, err := database.CreateRun(ctx, record, nil); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	names, err := database.queries.ListRunGateNames(ctx, databaseUUID(record.ID))
	if err != nil {
		t.Fatalf("list Run Gates: %v", err)
	}
	if !reflect.DeepEqual(names, record.Snapshot.Action.Gates) {
		t.Errorf("Run Gates = %v, want %v", names, record.Snapshot.Action.Gates)
	}
	targets, err := database.ListGateEvaluationTargets(ctx)
	if err != nil {
		t.Fatalf("ListGateEvaluationTargets() error = %v", err)
	}
	wantDefinition := persistedCatalog("1.14.3").Gates[0].NormalizedDefinition()
	if len(targets) != 1 || targets[0].RepositoryID != repositoryID ||
		targets[0].GateName != "network-idle" || targets[0].CatalogRevisionID != revisionID ||
		!reflect.DeepEqual(targets[0].Definition, wantDefinition) {
		t.Errorf("Gate evaluation targets = %#v", targets)
	}
	if _, err := database.CancelRun(ctx, record.ID, createdAt.Add(time.Second), nil); err != nil {
		t.Fatalf("cancel queued Run: %v", err)
	}
	targets, err = database.ListGateEvaluationTargets(ctx)
	if err != nil {
		t.Fatalf("ListGateEvaluationTargets() after cancellation error = %v", err)
	}
	if len(targets) != 0 {
		t.Errorf("Gate evaluation targets after cancellation = %#v", targets)
	}

	invalid := gateTestRun(t, database, repositoryID, revisionID, createdAt.Add(time.Minute))
	invalid.Snapshot.Action.Gates = []string{"network-idle", "network-idle"}
	if _, err := database.CreateRun(ctx, invalid, nil); err == nil {
		t.Fatal("CreateRun() with duplicate Gate error = nil")
	}
	if _, err := database.GetRun(ctx, invalid.ID); !errors.Is(err, run.ErrRunNotFound) {
		t.Errorf("rolled back Run error = %v, want Run not found", err)
	}
}

func createGateTestRepository(
	t *testing.T,
	database *Database,
	repositoryID uuid.UUID,
	revisionID uuid.UUID,
	at time.Time,
) {
	t.Helper()

	_, err := database.CreateRepository(t.Context(), repository.Repository{
		ID:             repositoryID,
		Name:           "infrastructure",
		CloneURL:       "https://github.com/example/infrastructure.git",
		Branch:         "main",
		CommitIndexing: repository.CommitIndexingObservedHeads,
		CreatedAt:      at,
	})
	if err != nil {
		t.Fatalf("CreateRepository() error = %v", err)
	}
	refresh := successfulCatalogRefresh(strings.Repeat("a", 40), at, revisionID)
	refresh.Revision.Catalog = persistedCatalog("1.14.3")
	if err := database.RecordRepositoryRefresh(t.Context(), repositoryID, refresh); err != nil {
		t.Fatalf("record repository refresh: %v", err)
	}
}

func gateTestRun(
	t *testing.T,
	database *Database,
	repositoryID uuid.UUID,
	revisionID uuid.UUID,
	createdAt time.Time,
) run.Run {
	t.Helper()

	catalogContents := persistedCatalog("1.14.3")
	target := catalogContents.Targets[0]
	action, found := target.EffectiveAction("preview")
	if !found {
		t.Fatal("preview Action is missing")
	}

	return run.Run{
		ID:                uuid.New(),
		Phase:             run.PhaseQueued,
		Review:            run.Review{State: run.ReviewStateNotRequired},
		TargetID:          activeTargetID(t, database, repositoryID, "network"),
		RepositoryID:      repositoryID,
		CatalogRevisionID: revisionID,
		CommitSHA:         strings.Repeat("a", 40),
		TargetName:        "network",
		ActionName:        "preview",
		Parameters:        map[string]any{},
		PlacementName:     "infrastructure",
		ClusterName:       "colo",
		Source:            run.Source{Type: run.SourceTypeManual},
		RequestDigest:     strings.Repeat("b", 64),
		Snapshot: run.Snapshot{
			Repository: run.RepositorySnapshot{
				Name:     "infrastructure",
				CloneURL: "https://github.com/example/infrastructure.git",
				Branch:   "main",
			},
			Target:  target,
			Action:  action,
			Runtime: catalogContents.Runtimes[0],
			Placement: run.PlacementSnapshot{
				Name:         "infrastructure",
				ClusterName:  "colo",
				JobNamespace: "slipramp-jobs",
			},
		},
		CreatedAt: createdAt,
	}
}
