package promotion

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/artifact"
	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/notification"
	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/run"
)

type recordingPreparationStore struct {
	record                  Promotion
	reservedRepositoryID    uuid.UUID
	reservedRevisionID      uuid.UUID
	attachedRunID           uuid.UUID
	completedGitArtifactID  uuid.UUID
	completedArgoArtifactID uuid.UUID
	notificationPlan        *notification.EnqueuePlan
	failureMessage          string
}

func (s *recordingPreparationStore) GetPromotionAwaitingDispatch(context.Context) (Promotion, error) {
	return s.record, nil
}

func (s *recordingPreparationStore) ReservePromotionPreparationRevision(
	_ context.Context,
	_ uuid.UUID,
	repositoryID uuid.UUID,
	revisionID uuid.UUID,
	_ time.Time,
) (Promotion, error) {
	s.reservedRepositoryID = repositoryID
	s.reservedRevisionID = revisionID
	s.record.PreparationRepositoryID = &repositoryID
	s.record.PreparationCatalogRevisionID = &revisionID

	return s.record, nil
}

func (s *recordingPreparationStore) AttachPromotionPreparationRun(
	_ context.Context,
	_ uuid.UUID,
	runID uuid.UUID,
	_ time.Time,
) (Promotion, error) {
	s.attachedRunID = runID
	s.record.PreparationRunID = &runID

	return s.record, nil
}

func (s *recordingPreparationStore) GetPromotionWithFinishedPreparation(context.Context) (Promotion, error) {
	return s.record, nil
}

func (s *recordingPreparationStore) CompletePromotionPreparation(
	_ context.Context,
	_ uuid.UUID,
	_ uuid.UUID,
	gitArtifactID uuid.UUID,
	argoArtifactID uuid.UUID,
	_ time.Time,
	notificationPlan *notification.EnqueuePlan,
) (Promotion, error) {
	s.completedGitArtifactID = gitArtifactID
	s.completedArgoArtifactID = argoArtifactID
	s.notificationPlan = notificationPlan

	return s.record, nil
}

type recordingPreparationNotifications struct {
	event notification.Event
}

func (n *recordingPreparationNotifications) Plan(event notification.Event) (*notification.EnqueuePlan, error) {
	n.event = event

	return &notification.EnqueuePlan{
		Event: event,
		Destinations: []notification.DestinationSnapshot{{
			Name: "operations",
			Type: notification.DestinationTypeSlack,
		}},
	}, nil
}

func (s *recordingPreparationStore) FailPromotionPreparation(
	_ context.Context,
	_ uuid.UUID,
	_ uuid.UUID,
	message string,
	_ time.Time,
) (Promotion, error) {
	s.failureMessage = message

	return s.record, nil
}

type recordingPreparationCatalogs struct {
	revision catalog.Revision
	calls    int
	err      error
}

func (c *recordingPreparationCatalogs) GetActiveRevision(
	context.Context,
	uuid.UUID,
) (catalog.Revision, error) {
	c.calls++

	return c.revision, c.err
}

type recordingPreparationRuns struct {
	request run.CreateRequest
	created run.Run
	loaded  run.Run
	err     error
}

func (r *recordingPreparationRuns) CreateRun(_ context.Context, request run.CreateRequest) (run.Run, error) {
	r.request = request

	return r.created, r.err
}

func (r *recordingPreparationRuns) GetRun(context.Context, uuid.UUID) (run.Run, error) {
	return r.loaded, r.err
}

type recordingPreparationArtifacts struct {
	records  map[string]artifact.Artifact
	contents map[uuid.UUID][]byte
	errors   map[string]error
}

func (a recordingPreparationArtifacts) List(
	_ context.Context,
	_ uuid.UUID,
) ([]artifact.Record, error) {
	if err := a.errors["list"]; err != nil {
		return nil, err
	}
	result := make([]artifact.Record, 0, len(a.records))
	for _, record := range a.records {
		result = append(result, artifact.Record{Artifact: record})
	}

	return result, nil
}

func (a recordingPreparationArtifacts) OpenArtifact(
	_ context.Context,
	artifactID uuid.UUID,
) (artifact.Content, error) {
	if err := a.errors[artifactID.String()]; err != nil {
		return artifact.Content{}, err
	}
	for _, record := range a.records {
		if record.ID == artifactID {
			return artifact.Content{
				Artifact: record,
				Body:     io.NopCloser(bytes.NewReader(a.contents[artifactID])),
			}, nil
		}
	}

	return artifact.Content{}, artifact.ErrNotFound
}

func TestPreparationDispatchReservesRevisionAndCreatesIdempotentRun(t *testing.T) {
	t.Parallel()

	record := preparingPromotion()
	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1920")
	revisionID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1921")
	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1922")
	store := &recordingPreparationStore{record: record}
	catalogs := &recordingPreparationCatalogs{revision: catalog.Revision{ID: revisionID}}
	runs := &recordingPreparationRuns{created: run.Run{ID: runID}}
	coordinator := testPreparationCoordinator(
		staticRepositoryLister{{
			ID:       repositoryID,
			Name:     "ops",
			CloneURL: "https://example.com/ops.git",
			Branch:   "main",
		}},
		catalogs,
		runs,
		recordingPreparationArtifacts{},
		store,
	)

	processed, err := coordinator.DispatchNext(t.Context())
	if err != nil || !processed {
		t.Fatalf("DispatchNext() = (%t, %v), want success", processed, err)
	}
	if store.reservedRepositoryID != repositoryID || store.reservedRevisionID != revisionID ||
		store.attachedRunID != runID {
		t.Errorf("reservation = (%s, %s), attached Run = %s", store.reservedRepositoryID, store.reservedRevisionID, store.attachedRunID)
	}
	request := runs.request
	if request.CatalogRevisionID == nil || *request.CatalogRevisionID != revisionID ||
		request.IdempotencyKey == nil || *request.IdempotencyKey != record.ID.String() ||
		request.Source.Promotion == nil || request.Source.Promotion.PromotionID != record.ID ||
		request.Actor == nil || request.Actor.ID != record.RequestedByPrincipalID {
		t.Errorf("preparation Run request = %#v", request)
	}
	if request.Parameters[PreparationParameterCandidateRevision] != record.CandidateRevision ||
		request.Parameters[PreparationParameterSourcePaths] != `["k8s/external-dns"]` {
		t.Errorf("preparation parameters = %#v", request.Parameters)
	}
}

func TestPreparationDispatchReusesReservedRevisionAfterRestart(t *testing.T) {
	t.Parallel()

	record := preparingPromotion()
	repositoryID := uuid.New()
	revisionID := uuid.New()
	record.PreparationRepositoryID = &repositoryID
	record.PreparationCatalogRevisionID = &revisionID
	store := &recordingPreparationStore{record: record}
	catalogs := &recordingPreparationCatalogs{err: errors.New("must not resolve active catalog")}
	runs := &recordingPreparationRuns{created: run.Run{ID: uuid.New()}}
	coordinator := testPreparationCoordinator(
		staticRepositoryLister(nil),
		catalogs,
		runs,
		recordingPreparationArtifacts{},
		store,
	)

	processed, err := coordinator.DispatchNext(t.Context())
	if err != nil || !processed {
		t.Fatalf("DispatchNext() = (%t, %v), want reserved retry success", processed, err)
	}
	if catalogs.calls != 0 || runs.request.CatalogRevisionID == nil ||
		*runs.request.CatalogRevisionID != revisionID {
		t.Errorf("active catalog calls = %d, Run request = %#v", catalogs.calls, runs.request)
	}
}

func TestPreparationReconciliation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		outcome            run.Outcome
		configureArtifacts func(*recordingPreparationArtifacts)
		wantCompleted      bool
		wantMessage        string
	}{
		{
			name:          "success",
			outcome:       run.OutcomeSucceeded,
			wantCompleted: true,
		},
		{
			name:        "failed Run",
			outcome:     run.OutcomeFailed,
			wantMessage: "outcome failed",
		},
		{
			name:    "missing artifact",
			outcome: run.OutcomeSucceeded,
			configureArtifacts: func(artifacts *recordingPreparationArtifacts) {
				delete(artifacts.records, GitDiffArtifactName)
			},
			wantMessage: "git-diff artifact is missing",
		},
		{
			name:    "deleted artifact",
			outcome: run.OutcomeSucceeded,
			configureArtifacts: func(artifacts *recordingPreparationArtifacts) {
				gitDiff := artifacts.records[GitDiffArtifactName]
				gitDiff.RetentionState = artifact.RetentionDeleted
				artifacts.records[GitDiffArtifactName] = gitDiff
			},
			wantMessage: "not a retained sensitive sealed",
		},
		{
			name:    "invalid Argo diff shape",
			outcome: run.OutcomeSucceeded,
			configureArtifacts: func(artifacts *recordingPreparationArtifacts) {
				record := artifacts.records[ArgoDiffArtifactName]
				contents := []byte("[]")
				digest := sha256.Sum256(contents)
				encodedDigest := hex.EncodeToString(digest[:])
				record.SizeBytes = int64(len(contents))
				record.SHA256 = &encodedDigest
				artifacts.records[ArgoDiffArtifactName] = record
				artifacts.contents[record.ID] = contents
			},
			wantMessage: "artifact is malformed",
		},
		{
			name:    "oversized diff",
			outcome: run.OutcomeSucceeded,
			configureArtifacts: func(artifacts *recordingPreparationArtifacts) {
				record := artifacts.records[GitDiffArtifactName]
				record.SizeBytes = MaximumPreparationArtifactSizeBytes + 1
				artifacts.records[GitDiffArtifactName] = record
			},
			wantMessage: "exceeds the",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			record := preparingPromotion()
			runID := uuid.New()
			record.PreparationRunID = &runID
			store := &recordingPreparationStore{record: record}
			runs := &recordingPreparationRuns{
				loaded: run.Run{
					ID:      runID,
					Phase:   run.PhaseFinished,
					Outcome: &test.outcome,
				},
			}
			artifactSet := validPreparationArtifacts()
			if test.configureArtifacts != nil {
				test.configureArtifacts(&artifactSet)
			}
			coordinator := testPreparationCoordinator(
				nil,
				nil,
				runs,
				artifactSet,
				store,
			)

			processed, err := coordinator.ReconcileNext(t.Context())
			if err != nil || !processed {
				t.Fatalf("ReconcileNext() = (%t, %v), want success", processed, err)
			}
			completed := store.completedGitArtifactID != uuid.Nil && store.completedArgoArtifactID != uuid.Nil
			if completed != test.wantCompleted || !strings.Contains(store.failureMessage, test.wantMessage) {
				t.Errorf("completed = %t, failure = %q", completed, store.failureMessage)
			}
		})
	}
}

func TestPreparationReconciliationPlansReviewRequestedNotification(t *testing.T) {
	t.Parallel()

	record := preparingPromotion()
	runID := uuid.New()
	record.PreparationRunID = &runID
	expiresAt := time.Date(2026, time.October, 31, 18, 0, 0, 0, time.UTC)
	record.ExpiresAt = &expiresAt
	store := &recordingPreparationStore{record: record}
	notifications := &recordingPreparationNotifications{}
	succeeded := run.OutcomeSucceeded
	coordinator := testPreparationCoordinator(
		nil,
		nil,
		&recordingPreparationRuns{loaded: run.Run{
			ID:      runID,
			Phase:   run.PhaseFinished,
			Outcome: &succeeded,
		}},
		validPreparationArtifacts(),
		store,
	)
	coordinator.notifications = notifications
	completedAt := time.Date(2026, time.October, 23, 18, 0, 0, 0, time.UTC)
	coordinator.now = func() time.Time { return completedAt }

	processed, err := coordinator.ReconcileNext(t.Context())
	if err != nil || !processed {
		t.Fatalf("ReconcileNext() = (%t, %v), want success", processed, err)
	}
	if store.notificationPlan == nil || store.notificationPlan.Event.ID != notifications.event.ID {
		t.Fatalf("notification plan = %#v, planned event = %#v", store.notificationPlan, notifications.event)
	}
	if notifications.event.SubjectID != record.ID || !notifications.event.OccurredAt.Equal(completedAt) ||
		notifications.event.DeduplicationKey != "promotion:"+record.ID.String()+":review-requested" {
		t.Errorf("notification event = %#v", notifications.event)
	}

	var payload notification.ReviewRequestedPayload
	if err := json.Unmarshal(notifications.event.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Title != "Promotion needs review" ||
		payload.Summary != "kargo-core/external-dns-prod is ready for review" ||
		payload.Action.Path != "/promotions/"+record.ID.String() ||
		payload.ExpiresAt == nil || !payload.ExpiresAt.Equal(expiresAt) || len(payload.Facts) != 2 {
		t.Errorf("review-requested payload = %#v", payload)
	}
}

func testPreparationCoordinator(
	repositories RepositoryLister,
	catalogs PreparationCatalogs,
	runs PreparationRuns,
	artifacts PreparationArtifacts,
	store PreparationStore,
) *PreparationCoordinator {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	return NewPreparationCoordinator(
		IntegrationSet{
			"prod": {
				Preparation: PreparationConfig{
					Repository: "ops",
					Target:     "kargo-promotions",
					Action:     "prepare-review",
				},
			},
		},
		repositories,
		catalogs,
		runs,
		artifacts,
		nil,
		store,
		time.Second,
		logger,
	)
}

func preparingPromotion() Promotion {
	principalID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1923")
	stageSnapshot, err := json.Marshal(PromotionStageSnapshot{
		ID:             uuid.New(),
		Integration:    "prod",
		ClusterName:    "colo",
		Project:        "kargo-core",
		Name:           "external-dns-prod",
		Application:    "external-dns",
		Environment:    "prod",
		RepositoryName: "ops",
		RepositoryURL:  "https://example.com/ops.git",
		TargetBranch:   "main",
		SourcePaths:    []string{"k8s/external-dns"},
	})
	if err != nil {
		panic(err)
	}

	return Promotion{
		Source:                 SourceKargo,
		ID:                     uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1924"),
		Integration:            "prod",
		ExternalPromotionID:    "external-dns-01jbe4q8",
		StageSnapshot:          stageSnapshot,
		BaseRevision:           "8b7f1d217a6b0000000000000000000000000000",
		CandidateRevision:      "f4cb21a88e1d0000000000000000000000000000",
		CandidateBranch:        "kargo/promotion/external-dns-01jbe4q8",
		TargetBranch:           "main",
		ReviewRequired:         true,
		Status:                 StatusPreparing,
		RequestedByPrincipalID: principalID,
		RequestedBySnapshot: productidentity.ActorSnapshot{
			PrincipalID: principalID,
			Kind:        productidentity.KindServiceAccount,
			DisplayName: "Kargo production",
		},
	}
}

func validPreparationArtifacts() recordingPreparationArtifacts {
	sealedAt := time.Date(2026, time.October, 21, 12, 5, 0, 0, time.UTC)
	contents := map[string][]byte{
		GitDiffArtifactName:  []byte(""),
		ArgoDiffArtifactName: []byte(`{"resources":[]}`),
	}
	contentByID := make(map[uuid.UUID][]byte, len(contents))
	makeArtifact := func(name, mediaType string) artifact.Artifact {
		value := contents[name]
		digest := sha256.Sum256(value)
		encodedDigest := hex.EncodeToString(digest[:])
		artifactID := uuid.New()
		contentByID[artifactID] = value

		return artifact.Artifact{
			ID:             artifactID,
			Name:           name,
			Kind:           artifact.KindOutput,
			State:          artifact.StateSealed,
			MediaType:      mediaType,
			SizeBytes:      int64(len(value)),
			SHA256:         &encodedDigest,
			Sensitive:      true,
			RetentionState: artifact.RetentionRetained,
			SealedAt:       &sealedAt,
		}
	}

	return recordingPreparationArtifacts{
		records: map[string]artifact.Artifact{
			GitDiffArtifactName:  makeArtifact(GitDiffArtifactName, GitDiffArtifactMediaType),
			ArgoDiffArtifactName: makeArtifact(ArgoDiffArtifactName, ArgoDiffArtifactMediaType),
		},
		contents: contentByID,
	}
}
