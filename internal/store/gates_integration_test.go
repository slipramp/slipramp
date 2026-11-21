package store

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/slipramp/slipramp/internal/gate"
	"github.com/slipramp/slipramp/internal/repository"
	"github.com/slipramp/slipramp/internal/run"
)

func TestInactiveGateObservationCleanupPreservesActiveDemandIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	repositoryID := uuid.New()
	oldRevisionID := uuid.New()
	now := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	createGateTestRepository(t, database, repositoryID, oldRevisionID, now.Add(-2*time.Hour))

	definition := persistedCatalog("1.14.3").Gates[0].Spec
	digest, err := definition.Digest()
	if err != nil {
		t.Fatalf("digest Gate definition: %v", err)
	}
	value := float64(0)
	oldObservation := gate.CurrentObservation{
		RepositoryID:       repositoryID,
		GateName:           "network-idle",
		CatalogRevisionID:  oldRevisionID,
		DefinitionDigest:   digest,
		EvaluatorProcessID: uuid.New(),
		Observation:        gate.Observation{State: gate.ObservationPassing, Value: &value},
		EvaluatedAt:        now.Add(-90 * time.Minute),
		ValidUntil:         now.Add(-89 * time.Minute),
	}
	if err := database.UpsertGateObservation(ctx, oldObservation); err != nil {
		t.Fatalf("record old Gate observation: %v", err)
	}

	activeRevisionID := uuid.New()
	refresh := successfulCatalogRefresh(strings.Repeat("b", 40), now.Add(-time.Hour), activeRevisionID)
	refresh.Revision.Catalog = persistedCatalog("1.14.3")
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, refresh); err != nil {
		t.Fatalf("activate current catalog: %v", err)
	}
	record := gateTestRun(t, database, repositoryID, activeRevisionID, now.Add(-30*time.Minute))
	if _, err := database.CreateRun(ctx, record, nil); err != nil {
		t.Fatalf("create waiting Run: %v", err)
	}
	activeObservation := oldObservation
	activeObservation.CatalogRevisionID = activeRevisionID
	activeObservation.EvaluatedAt = now.Add(-20 * time.Minute)
	activeObservation.ValidUntil = now.Add(-19 * time.Minute)
	if err := database.UpsertGateObservation(ctx, activeObservation); err != nil {
		t.Fatalf("record active Gate observation: %v", err)
	}

	deleted, err := database.DeleteInactiveGateObservations(ctx, now.Add(-10*time.Minute))
	if err != nil {
		t.Fatalf("DeleteInactiveGateObservations() error = %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted Gate observations = %d, want 1", deleted)
	}
	summaries, err := database.ListRunGateSummaries(ctx, []uuid.UUID{record.ID}, activeObservation.EvaluatorProcessID, now)
	if err != nil {
		t.Fatalf("read retained Gate evidence: %v", err)
	}
	requirements := summaries[record.ID]
	if len(requirements) != 1 || requirements[0].State != gate.RequirementUnavailable ||
		requirements[0].EvaluatedAt == nil || !requirements[0].EvaluatedAt.Equal(activeObservation.EvaluatedAt) {
		t.Errorf("active observation after cleanup = %#v", requirements)
	}
}

func TestPrometheusGateEvaluationRecoversAndRequiresCurrentProcessIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	var sourceState atomic.Int32
	source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		switch sourceState.Load() {
		case 0:
			_, _ = io.WriteString(writer, `{"status":"success","data":{"resultType":"scalar","result":[1,"1"]}}`)
		case 1:
			_, _ = io.WriteString(writer, `{"status":"success","data":{"resultType":"scalar","result":[1,"0"]}}`)
		default:
			http.Error(writer, "unavailable", http.StatusServiceUnavailable)
		}
	}))
	defer source.Close()

	now := time.Now().UTC()
	repositoryID := uuid.New()
	revisionID := uuid.New()
	createGateTestRepository(t, database, repositoryID, revisionID, now.Add(-time.Minute))
	record := gateTestRun(t, database, repositoryID, revisionID, now.Add(-30*time.Second))
	if _, err := database.CreateRun(ctx, record, nil); err != nil {
		t.Fatalf("create gated Run: %v", err)
	}

	firstProcess := uuid.New()
	evaluatePrometheusGate(t, ctx, database, source.URL, firstProcess, gate.ObservationBlocking)
	assertNoGateRunAssignment(t, database, firstProcess, time.Now().UTC())

	sourceState.Store(1)
	evaluatePrometheusGate(t, ctx, database, source.URL, firstProcess, gate.ObservationPassing)
	restartedProcess := uuid.New()
	assertNoGateRunAssignment(t, database, restartedProcess, time.Now().UTC())

	sourceState.Store(2)
	evaluatePrometheusGate(t, ctx, database, source.URL, restartedProcess, gate.ObservationUnavailable)
	assertNoGateRunAssignment(t, database, restartedProcess, time.Now().UTC())

	sourceState.Store(1)
	evaluatePrometheusGate(t, ctx, database, source.URL, restartedProcess, gate.ObservationPassing)
	assigned, err := database.ClaimQueuedRun(
		ctx,
		"colo",
		testSchedulingCapacity(),
		restartedProcess,
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("claim recovered gated Run: %v", err)
	}
	if assigned.ID != record.ID || assigned.Phase != run.PhaseAssigned {
		t.Errorf("assigned Run = %#v", assigned)
	}
}

func evaluatePrometheusGate(
	t *testing.T,
	ctx context.Context,
	database *Database,
	sourceURL string,
	processID uuid.UUID,
	wantState gate.ObservationState,
) {
	t.Helper()

	store := recordingGateStore{Database: database, observations: make(chan gate.CurrentObservation, 1)}
	coordinator, err := gate.NewCoordinator(
		store,
		gate.SourceSet{"homelab": {Prometheus: gate.PrometheusSource{
			URL:                sourceURL,
			EvaluationInterval: 15 * time.Second,
			Timeout:            5 * time.Second,
			MaximumConcurrency: 2,
		}}},
		processID,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		nil,
	)
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}

	runContext, cancel := context.WithCancel(ctx)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		coordinator.Run(runContext)
	}()
	defer func() {
		cancel()
		<-stopped
	}()

	select {
	case observation := <-store.observations:
		if observation.Observation.State != wantState {
			t.Fatalf("recorded observation = %#v, want state %q", observation, wantState)
		}
	case <-ctx.Done():
		t.Fatalf("wait for Gate observation: %v", ctx.Err())
	}
}

type recordingGateStore struct {
	*Database
	observations chan gate.CurrentObservation
}

func (s recordingGateStore) UpsertGateObservation(ctx context.Context, observation gate.CurrentObservation) error {
	if err := s.Database.UpsertGateObservation(ctx, observation); err != nil {
		return err
	}
	select {
	case s.observations <- observation:
	case <-ctx.Done():
	}
	return nil
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
	summaries, err := database.ListRunGateSummaries(ctx, []uuid.UUID{record.ID}, uuid.New(), createdAt)
	if err != nil {
		t.Fatalf("read Run Gate evidence: %v", err)
	}
	requirements := summaries[record.ID]
	wantDefinition := persistedCatalog("1.14.3").Gates[0].Spec
	if requirements.State() != gate.SummaryWaiting || len(requirements) != 2 ||
		requirements[0].Name != "network-idle" || requirements[0].State != gate.RequirementUnevaluated ||
		requirements[0].Condition == nil || *requirements[0].Condition != wantDefinition.Prometheus.Condition ||
		requirements[1].Name != "change-window" || requirements[1].State != gate.RequirementUnavailable ||
		requirements[1].Condition != nil {
		t.Fatalf("ordered Run Gate requirements = %#v", requirements)
	}
	targets, err := database.ListGateEvaluationDemand(ctx)
	if err != nil {
		t.Fatalf("ListGateEvaluationDemand() error = %v", err)
	}
	if len(targets) != 1 || targets[0].RepositoryID != repositoryID ||
		targets[0].GateName != "network-idle" || targets[0].CatalogRevisionID != revisionID ||
		targets[0].WaiterCount != 1 || !targets[0].LastRequestedAt.Equal(createdAt) ||
		targets[0].Definition != wantDefinition {
		t.Errorf("Gate evaluation demand = %#v", targets)
	}
	metricsSource := database.MetricsSnapshotSource(uuid.Nil)
	metrics, err := metricsSource.LoadMetricsSnapshot(ctx, createdAt)
	if err != nil || metrics.WaitingGateSubjects["run"] != 1 {
		t.Fatalf("waiting Runs with two Gate requirements = %v, error = %v", metrics.WaitingGateSubjects, err)
	}

	if _, err := database.CancelRun(ctx, record.ID, createdAt.Add(time.Second), nil); err != nil {
		t.Fatalf("cancel queued Run: %v", err)
	}
	targets, err = database.ListGateEvaluationDemand(ctx)
	if err != nil {
		t.Fatalf("ListGateEvaluationDemand() after cancellation error = %v", err)
	}
	if len(targets) != 0 {
		t.Errorf("Gate evaluation demand after cancellation = %#v", targets)
	}
	metrics, err = metricsSource.LoadMetricsSnapshot(ctx, createdAt.Add(time.Second))
	if err != nil || metrics.WaitingGateSubjects["run"] != 0 {
		t.Errorf("waiting Runs after cancellation = %v, error = %v", metrics.WaitingGateSubjects, err)
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

func TestRunGateReleaseCommitsWithAssignmentIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	repositoryID := uuid.New()
	revisionID := uuid.New()
	createdAt := time.Date(2026, time.November, 19, 14, 0, 0, 0, time.UTC)
	createGateTestRepository(t, database, repositoryID, revisionID, createdAt)

	record := gateTestRun(t, database, repositoryID, revisionID, createdAt)
	if _, err := database.CreateRun(ctx, record, nil); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	ungated := gateTestRun(t, database, repositoryID, revisionID, createdAt.Add(time.Second))
	ungated.Snapshot.Action.Gates = nil
	if _, err := database.CreateRun(ctx, ungated, nil); err != nil {
		t.Fatalf("create later ungated Run: %v", err)
	}
	snapshots, err := database.GetRunSchedulingSnapshots(
		ctx,
		[]uuid.UUID{record.ID, ungated.ID},
		uuid.New(), createdAt,
		[]byte(`{"colo":4}`),
		[]byte(`{}`),
	)
	if err != nil {
		t.Fatalf("GetRunSchedulingSnapshots() error = %v", err)
	}
	if len(snapshots) != 2 {
		t.Fatalf("Run scheduling snapshots = %#v", snapshots)
	}
	snapshotsByID := make(map[uuid.UUID]run.SchedulingSnapshot, len(snapshots))
	for _, snapshot := range snapshots {
		snapshotsByID[snapshot.RunID] = snapshot
	}
	gatedSnapshot := snapshotsByID[record.ID]
	ungatedSnapshot := snapshotsByID[ungated.ID]
	if gatedSnapshot.GatesReady || gatedSnapshot.EligiblePosition != nil ||
		!ungatedSnapshot.GatesReady || ungatedSnapshot.EligiblePosition == nil ||
		*ungatedSnapshot.EligiblePosition != 1 {
		t.Errorf("Gate-aware Run scheduling snapshots = %#v", snapshots)
	}
	if _, err := database.CancelRun(ctx, ungated.ID, createdAt.Add(2*time.Second), nil); err != nil {
		t.Fatalf("cancel later ungated Run: %v", err)
	}

	assignedAt := createdAt.Add(time.Minute)
	evaluatorID := uuid.New()
	assertNoGateRunAssignment(t, database, evaluatorID, assignedAt)

	definition := persistedCatalog("1.14.3").Gates[0].Spec
	digest, err := definition.Digest()
	if err != nil {
		t.Fatalf("digest Gate definition: %v", err)
	}
	observedValue := 1.0
	observation := gate.CurrentObservation{
		RepositoryID:       repositoryID,
		GateName:           "network-idle",
		CatalogRevisionID:  revisionID,
		DefinitionDigest:   digest,
		EvaluatorProcessID: evaluatorID,
		Observation: gate.Observation{
			State: gate.ObservationBlocking,
			Value: &observedValue,
		},
		EvaluatedAt: assignedAt.Add(-10 * time.Second),
		ValidUntil:  assignedAt.Add(35 * time.Second),
	}
	if err := database.UpsertGateObservation(ctx, observation); err != nil {
		t.Fatalf("record blocking Gate observation: %v", err)
	}
	assertNoGateRunAssignment(t, database, evaluatorID, assignedAt)

	observedValue = 0
	observation.Observation = gate.Observation{
		State: gate.ObservationPassing,
		Value: &observedValue,
	}
	observation.EvaluatedAt = assignedAt.Add(-time.Minute)
	observation.ValidUntil = assignedAt
	if err := database.UpsertGateObservation(ctx, observation); err != nil {
		t.Fatalf("record stale Gate observation: %v", err)
	}
	assertNoGateRunAssignment(t, database, evaluatorID, assignedAt)

	observation.EvaluatedAt = assignedAt.Add(-10 * time.Second)
	observation.ValidUntil = assignedAt.Add(35 * time.Second)
	if err := database.UpsertGateObservation(ctx, observation); err != nil {
		t.Fatalf("record passing Gate observation: %v", err)
	}
	waiting, err := database.ListRunGateSummaries(ctx, []uuid.UUID{record.ID}, evaluatorID, assignedAt)
	if err != nil {
		t.Fatalf("list observed Run Gate projection: %v", err)
	}
	requirements := waiting[record.ID]
	if len(requirements) != 1 || requirements[0].State != gate.RequirementPassing ||
		requirements[0].Value == nil || *requirements[0].Value != observedValue ||
		requirements[0].EvaluatedAt == nil || !requirements[0].EvaluatedAt.Equal(observation.EvaluatedAt) {
		t.Errorf("observed Run Gate projection = %#v", requirements)
	}

	releaseRevisionID := uuid.New()
	releaseCatalog := persistedCatalog("1.14.4")
	releaseCatalog.Gates[0].Metadata.Title = "Release window"
	releaseCatalog.Gates[0].Spec.Prometheus.Condition.Value = 2
	refresh := successfulCatalogRefresh(
		strings.Repeat("c", 40),
		assignedAt.Add(-5*time.Second),
		releaseRevisionID,
	)
	refresh.Revision.Catalog = releaseCatalog
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, refresh); err != nil {
		t.Fatalf("activate changed Gate definition: %v", err)
	}
	assertNoGateRunAssignment(t, database, evaluatorID, assignedAt)

	definition = releaseCatalog.Gates[0].Spec
	digest, err = definition.Digest()
	if err != nil {
		t.Fatalf("digest changed Gate definition: %v", err)
	}
	observedValue = 2
	observation.CatalogRevisionID = releaseRevisionID
	observation.DefinitionDigest = digest
	observation.Observation.Value = &observedValue
	if err := database.UpsertGateObservation(ctx, observation); err != nil {
		t.Fatalf("record changed passing Gate observation: %v", err)
	}
	claimed, err := database.ClaimQueuedRun(
		ctx,
		"colo",
		testSchedulingCapacity(),
		evaluatorID,
		assignedAt,
	)
	if err != nil {
		t.Fatalf("claim passing gated Run: %v", err)
	}
	if claimed.ID != record.ID || claimed.Phase != run.PhaseAssigned {
		t.Errorf("claimed Run = %#v", claimed)
	}

	var (
		releasedAt               time.Time
		releaseCatalogRevisionID pgtype.UUID
		releaseDefinition        []byte
		releaseDefinitionDigest  string
		releaseEvaluatorID       pgtype.UUID
		releaseObservedValue     float64
		releaseEvaluatedAt       time.Time
		releaseValidUntil        time.Time
	)
	err = database.pool.QueryRow(
		ctx,
		`SELECT
             released_at,
             release_catalog_revision_id,
             release_definition,
             release_definition_digest,
             release_evaluator_process_id,
             release_observed_value,
             release_evaluated_at,
             release_valid_until
         FROM run_gates
         WHERE run_id = $1 AND gate_name = 'network-idle'`,
		databaseUUID(record.ID),
	).Scan(
		&releasedAt,
		&releaseCatalogRevisionID,
		&releaseDefinition,
		&releaseDefinitionDigest,
		&releaseEvaluatorID,
		&releaseObservedValue,
		&releaseEvaluatedAt,
		&releaseValidUntil,
	)
	if err != nil {
		t.Fatalf("load Run Gate release evidence: %v", err)
	}
	var releasedDefinition gate.Definition
	if err := json.Unmarshal(releaseDefinition, &releasedDefinition); err != nil {
		t.Fatalf("decode released Gate definition: %v", err)
	}
	if !releasedAt.Equal(assignedAt) ||
		uuid.UUID(releaseCatalogRevisionID.Bytes) != releaseRevisionID ||
		releasedDefinition != definition ||
		releaseDefinitionDigest != digest ||
		uuid.UUID(releaseEvaluatorID.Bytes) != evaluatorID ||
		releaseObservedValue != observedValue ||
		!releaseEvaluatedAt.Equal(observation.EvaluatedAt) ||
		!releaseValidUntil.Equal(observation.ValidUntil) {
		t.Errorf("released Gate evidence does not match current observation")
	}

	targets, err := database.ListGateEvaluationDemand(ctx)
	if err != nil {
		t.Fatalf("ListGateEvaluationDemand() after release error = %v", err)
	}
	if len(targets) != 0 {
		t.Errorf("Gate evaluation demand after release = %#v", targets)
	}
	service := run.NewService(run.Dependencies{Gates: database, GateEvaluatorProcessID: evaluatorID})
	summaries, err := service.GateSummaries(ctx, []run.Run{claimed, ungated}, assignedAt)
	if err != nil {
		t.Fatalf("read released Run Gate summary: %v", err)
	}
	summary := summaries[claimed.ID]
	if summary.State() != gate.SummaryReleased || len(summary) != 1 ||
		summary[0].Title != "Release window" || summary[0].ReleasedAt == nil || !summary[0].ReleasedAt.Equal(assignedAt) ||
		summary[0].Condition == nil || *summary[0].Condition != definition.Prometheus.Condition ||
		summary[0].Value == nil || *summary[0].Value != observedValue {
		t.Errorf("released Run Gate summary = %#v", summary)
	}
	if got := summaries[ungated.ID].State(); got != gate.SummaryNotRequired {
		t.Errorf("ungated Run summary state = %q", got)
	}
}

func assertNoGateRunAssignment(
	t *testing.T,
	database *Database,
	evaluatorID uuid.UUID,
	assignedAt time.Time,
) {
	t.Helper()

	_, err := database.ClaimQueuedRun(
		t.Context(),
		"colo",
		testSchedulingCapacity(),
		evaluatorID,
		assignedAt,
	)
	if !errors.Is(err, run.ErrNoRunAssignment) {
		t.Fatalf("ClaimQueuedRun() error = %v, want no assignment", err)
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
