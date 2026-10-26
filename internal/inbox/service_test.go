package inbox

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/cluster"
	"github.com/slipramp/slipramp/internal/gittrigger"
	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/repository"
	"github.com/slipramp/slipramp/internal/run"
)

func TestServiceBuildsCompleteSnapshot(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 23, 12, 0, 0, 123456789, time.FixedZone("MDT", -6*60*60))
	at := now.UTC()
	offlineRunID := uuid.MustParse("00000000-0000-0000-0000-000000000101")
	staleRunID := uuid.MustParse("00000000-0000-0000-0000-000000000102")
	pendingReview := activeRun(
		uuid.MustParse("00000000-0000-0000-0000-000000000100"),
		run.PhaseQueued,
		"connected",
		at,
	)
	pendingReview.Review = run.ReviewStatePending
	store := &serviceStore{
		repositories: []repository.Repository{
			failedRepository(uuid.MustParse("00000000-0000-0000-0000-000000000201"), "infrastructure", at.Add(-10*time.Minute)),
		},
		clusters: []cluster.Observation{
			{Name: "offline", FirstSeenAt: at.Add(-time.Hour), LastSeenAt: at.Add(-2 * time.Minute)},
			{Name: "connected", FirstSeenAt: at.Add(-time.Hour), LastSeenAt: at.Add(-time.Minute)},
		},
		runs: []ActiveRun{
			pendingReview,
			activeRun(offlineRunID, run.PhasePreparing, "offline", at.Add(-3*time.Minute)),
			activeRun(staleRunID, run.PhaseAssigned, "connected", at.Add(-2*time.Minute)),
			activeRun(uuid.MustParse("00000000-0000-0000-0000-000000000103"), run.PhaseRunning, "connected", at.Add(-30*time.Second)),
		},
		promotions: []PromotionState{
			inboxPromotion(uuid.MustParse("00000000-0000-0000-0000-000000000301"), promotion.StatusPending, at.Add(-8*time.Minute)),
			inboxPromotion(uuid.MustParse("00000000-0000-0000-0000-000000000302"), promotion.StatusError, at.Add(-7*time.Minute)),
		},
		failures: []gittrigger.FailedDelivery{{
			ID:             uuid.MustParse("00000000-0000-0000-0000-000000000401"),
			RepositoryID:   uuid.MustParse("00000000-0000-0000-0000-000000000201"),
			RepositoryName: "infrastructure",
			TargetName:     "network",
			ActionName:     "sync",
			TriggerName:    "on-change",
			FailureCount:   8,
			Diagnostic:     "run placement not found",
			FailedAt:       at.Add(-6 * time.Minute),
		}},
	}
	service := newTestService(store, now)
	query := Query{
		PrincipalID:               uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		IncludePromotionDecisions: true,
		IncludeRunDecisions:       true,
	}

	result, err := service.Load(t.Context(), query)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if result.GeneratedAt != at || store.expiredAt != at {
		t.Errorf("evaluation time = %s, expiry time = %s, want %s", result.GeneratedAt, store.expiredAt, at)
	}
	if result.Activity != (Activity{Queued: 1, Starting: 2, Running: 1}) {
		t.Errorf("activity = %#v", result.Activity)
	}
	if len(result.Decisions) != 2 || result.Decisions[0].Kind != KindRunReview ||
		result.Decisions[1].Kind != KindPromotionReview {
		t.Errorf("decisions = %#v", result.Decisions)
	}
	if len(result.Problems) != 5 {
		t.Fatalf("problems = %d, want 5: %#v", len(result.Problems), result.Problems)
	}

	clusterProblem := findProblem(t, result.Problems, KindClusterUnreachable)
	if !strings.Contains(clusterProblem.Description, "1 active Run") {
		t.Errorf("cluster description = %q", clusterProblem.Description)
	}
	if slices.ContainsFunc(result.Problems, func(item Item) bool {
		return item.Destination.Type == DestinationRun && item.Destination.ID == offlineRunID.String()
	}) {
		t.Error("Run on unreachable Cluster was returned separately")
	}
	if runProblem := findProblem(t, result.Problems, KindRunObservation); runProblem.Destination.ID != staleRunID.String() {
		t.Errorf("standalone Run = %q, want %q", runProblem.Destination.ID, staleRunID)
	}
	if !slices.IsSortedFunc(result.Problems, newestInboxItemFirst) {
		t.Errorf("problems are not newest-first: %#v", result.Problems)
	}
}

func TestServiceAppliesDecisionVisibility(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.October, 23, 17, 0, 0, 0, time.UTC)
	pendingRun := activeRun(
		uuid.MustParse("00000000-0000-0000-0000-000000000501"),
		run.PhaseQueued,
		"connected",
		at,
	)
	pendingRun.Review = run.ReviewStatePending
	store := &serviceStore{
		runs: []ActiveRun{pendingRun},
		promotions: []PromotionState{
			inboxPromotion(
				uuid.MustParse("00000000-0000-0000-0000-000000000502"),
				promotion.StatusPending,
				at.Add(-time.Minute),
			),
		},
	}
	service := newTestService(store, at)
	principalID := uuid.MustParse("00000000-0000-0000-0000-000000000503")

	runOnly, err := service.Load(t.Context(), Query{PrincipalID: principalID, IncludeRunDecisions: true})
	if err != nil {
		t.Fatalf("Load() Run decisions error = %v", err)
	}
	if len(runOnly.Decisions) != 1 || runOnly.Decisions[0].Kind != KindRunReview ||
		runOnly.Decisions[0].Destination.ID != pendingRun.ID.String() {
		t.Errorf("Run decisions = %#v", runOnly.Decisions)
	}

	promotionOnly, err := service.Load(
		t.Context(),
		Query{PrincipalID: principalID, IncludePromotionDecisions: true},
	)
	if err != nil {
		t.Fatalf("Load() Promotion decisions error = %v", err)
	}
	if len(promotionOnly.Decisions) != 1 || promotionOnly.Decisions[0].Kind != KindPromotionReview {
		t.Errorf("Promotion decisions = %#v", promotionOnly.Decisions)
	}
}

func TestServiceAppliesVisibilityAndAcknowledgements(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.October, 23, 18, 0, 0, 0, time.UTC)
	principalID := uuid.MustParse("00000000-0000-0000-0000-000000000501")
	record := failedRepository(uuid.MustParse("00000000-0000-0000-0000-000000000502"), "infra", at)
	problemID := mustRepositoryProblem(t, record).ID
	store := &serviceStore{
		repositories: []repository.Repository{record},
		promotions: []PromotionState{
			inboxPromotion(uuid.MustParse("00000000-0000-0000-0000-000000000503"), promotion.StatusPending, at),
		},
		acknowledgements: map[uuid.UUID][]string{principalID: {problemID}},
	}

	result, err := newTestService(store, at).Load(t.Context(), Query{PrincipalID: principalID})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(result.Decisions) != 0 || len(result.Problems) != 0 {
		t.Errorf("filtered snapshot = %#v", result)
	}

	laterAttempt := at.Add(time.Minute)
	store.repositories[0].Refresh.AttemptedAt = &laterAttempt
	result, err = newTestService(store, laterAttempt).Load(t.Context(), Query{PrincipalID: principalID})
	if err != nil {
		t.Fatalf("Load() after repeated failure error = %v", err)
	}
	if len(result.Problems) != 0 {
		t.Errorf("repeated failure escaped acknowledgement = %#v", result.Problems)
	}

	store.repositories[0].Refresh.FailureStartedAt = &laterAttempt
	result, err = newTestService(store, laterAttempt).Load(t.Context(), Query{PrincipalID: principalID})
	if err != nil {
		t.Fatalf("Load() after recurrent failure error = %v", err)
	}
	if len(result.Problems) != 1 || result.Problems[0].ID == problemID {
		t.Errorf("recurrent failure = %#v, previous ID %q", result.Problems, problemID)
	}
}

func TestServiceAcknowledgesOnlyRequestedCurrentProblems(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.October, 23, 20, 0, 0, 0, time.UTC)
	principalID := uuid.MustParse("00000000-0000-0000-0000-000000000601")
	first := failedRepository(uuid.MustParse("00000000-0000-0000-0000-000000000602"), "infra", at.Add(-time.Minute))
	second := failedRepository(uuid.MustParse("00000000-0000-0000-0000-000000000603"), "applications", at)
	store := &serviceStore{repositories: []repository.Repository{first, second}}
	service := newTestService(store, at)
	firstID := mustRepositoryProblem(t, first).ID
	secondID := mustRepositoryProblem(t, second).ID

	if err := service.AcknowledgeProblems(
		t.Context(),
		principalID,
		[]string{strings.Repeat("f", 64), firstID},
	); err != nil {
		t.Fatalf("AcknowledgeProblems() error = %v", err)
	}
	if !slices.Equal(store.insertedProblemIDs, []string{firstID}) {
		t.Errorf("inserted IDs = %v, want [%s]", store.insertedProblemIDs, firstID)
	}
	if slices.Contains(store.insertedProblemIDs, secondID) {
		t.Errorf("unrequested problem %q was acknowledged", secondID)
	}
	if store.insertedPrincipalID != principalID || store.acknowledgedAt != at {
		t.Errorf("insert = principal %s at %s", store.insertedPrincipalID, store.acknowledgedAt)
	}

	store.insertedProblemIDs = nil
	store.repositories[0].Refresh = repository.RefreshState{Status: repository.RefreshStatusSucceeded}
	if err := service.AcknowledgeProblems(t.Context(), principalID, []string{firstID}); err != nil {
		t.Fatalf("AcknowledgeProblems() resolved occurrence error = %v", err)
	}
	if store.insertedProblemIDs != nil {
		t.Errorf("resolved occurrence was inserted: %v", store.insertedProblemIDs)
	}
}

func TestServiceRejectsInvalidAcknowledgementBatches(t *testing.T) {
	t.Parallel()

	principalID := uuid.MustParse("00000000-0000-0000-0000-000000000701")
	validID := strings.Repeat("a", 64)
	tests := []struct {
		name        string
		principalID uuid.UUID
		problemIDs  []string
	}{
		{name: "missing principal", problemIDs: []string{validID}},
		{name: "empty batch", principalID: principalID},
		{name: "malformed ID", principalID: principalID, problemIDs: []string{"not-a-digest"}},
		{name: "duplicate ID", principalID: principalID, problemIDs: []string{validID, validID}},
		{name: "oversized batch", principalID: principalID, problemIDs: make([]string, maximumAcknowledgementBatch+1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := newTestService(&serviceStore{}, time.Now().UTC())
			err := service.AcknowledgeProblems(t.Context(), test.principalID, test.problemIDs)
			if !errors.Is(err, ErrInvalidAcknowledgement) {
				t.Errorf("AcknowledgeProblems() error = %v, want ErrInvalidAcknowledgement", err)
			}
		})
	}
}

func TestServiceFailsClosedWhenASourceCannotBeLoaded(t *testing.T) {
	t.Parallel()

	want := errors.New("database unavailable")
	store := &serviceStore{listRunsError: want}
	result, err := newTestService(store, time.Now().UTC()).Load(
		t.Context(),
		Query{PrincipalID: uuid.MustParse("00000000-0000-0000-0000-000000000801")},
	)
	if !errors.Is(err, want) {
		t.Fatalf("Load() error = %v, want %v", err, want)
	}
	if !result.GeneratedAt.IsZero() || len(result.Decisions) != 0 || len(result.Problems) != 0 ||
		result.Activity != (Activity{}) {
		t.Errorf("partial snapshot = %#v", result)
	}
}

type serviceStore struct {
	repositories        []repository.Repository
	clusters            []cluster.Observation
	runs                []ActiveRun
	promotions          []PromotionState
	failures            []gittrigger.FailedDelivery
	acknowledgements    map[uuid.UUID][]string
	listRunsError       error
	expiredAt           time.Time
	insertedPrincipalID uuid.UUID
	insertedProblemIDs  []string
	acknowledgedAt      time.Time
}

func (s *serviceStore) ExpirePendingAt(_ context.Context, at time.Time) error {
	s.expiredAt = at
	return nil
}

func (s *serviceStore) ListRepositories(context.Context) ([]repository.Repository, error) {
	return s.repositories, nil
}

func (s *serviceStore) ListClusterObservations(context.Context) ([]cluster.Observation, error) {
	return s.clusters, nil
}

func (s *serviceStore) ListInboxActiveRuns(context.Context) ([]ActiveRun, error) {
	return s.runs, s.listRunsError
}

func (s *serviceStore) ListInboxPromotions(context.Context) ([]PromotionState, error) {
	return s.promotions, nil
}

func (s *serviceStore) ListInboxFailedGitTriggerDeliveries(context.Context) ([]gittrigger.FailedDelivery, error) {
	return s.failures, nil
}

func (s *serviceStore) ListInboxProblemAcknowledgements(
	_ context.Context,
	principalID uuid.UUID,
) ([]string, error) {
	return s.acknowledgements[principalID], nil
}

func (s *serviceStore) InsertInboxProblemAcknowledgements(
	_ context.Context,
	principalID uuid.UUID,
	problemIDs []string,
	acknowledgedAt time.Time,
) error {
	s.insertedPrincipalID = principalID
	s.insertedProblemIDs = slices.Clone(problemIDs)
	s.acknowledgedAt = acknowledgedAt
	return nil
}

func newTestService(store *serviceStore, now time.Time) *Service {
	service := NewService(Dependencies{State: store, Acknowledgements: store, Promotions: store})
	service.now = func() time.Time { return now }
	return service
}

func failedRepository(id uuid.UUID, name string, attemptedAt time.Time) repository.Repository {
	failureReason := "Target definition is invalid."
	return repository.Repository{
		ID:   id,
		Name: name,
		Refresh: repository.RefreshState{
			Status:           repository.RefreshStatusFailed,
			AttemptedAt:      &attemptedAt,
			FailureStartedAt: &attemptedAt,
			FailureReason:    &failureReason,
		},
	}
}

func activeRun(id uuid.UUID, phase run.Phase, clusterName string, observedAt time.Time) ActiveRun {
	record := ActiveRun{
		ID:          id,
		TargetName:  "network",
		ActionName:  "sync",
		ClusterName: clusterName,
		CreatedAt:   observedAt.Add(-time.Minute),
		Status: run.ObservationStatus{
			Phase: phase,
		},
	}
	if phase != run.PhaseQueued {
		record.Status.AssignedAt = &observedAt
	}
	if phase == run.PhasePreparing || phase == run.PhaseRunning {
		record.Status.LastObservedAt = &observedAt
	}
	return record
}

func inboxPromotion(id uuid.UUID, status promotion.PromotionStatus, at time.Time) PromotionState {
	message := "Diff collection failed."
	return PromotionState{
		ID:              id,
		StageName:       "production",
		Application:     "api",
		CandidateBranch: "kargo/promotion/api",
		TargetBranch:    "main",
		Status:          status,
		Message:         &message,
		CreatedAt:       at,
		UpdatedAt:       at,
	}
}

func findProblem(t *testing.T, problems []Item, kind Kind) Item {
	t.Helper()
	for _, problem := range problems {
		if problem.Kind == kind {
			return problem
		}
	}
	t.Fatalf("problem kind %q not found in %#v", kind, problems)
	return Item{}
}

func mustRepositoryProblem(t *testing.T, record repository.Repository) Item {
	t.Helper()
	item, err := repositoryRefreshItem(record)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func newestInboxItemFirst(left Item, right Item) int {
	if timeOrder := right.OccurredAt.Compare(left.OccurredAt); timeOrder != 0 {
		return timeOrder
	}
	return strings.Compare(left.ID, right.ID)
}
