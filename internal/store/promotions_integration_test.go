package store

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/argodiff"
	"github.com/slipramp/slipramp/internal/artifact"
	"github.com/slipramp/slipramp/internal/notification"
	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/repository"
	"github.com/slipramp/slipramp/internal/run"
)

func TestAcceptPromotionRequestIntegration(t *testing.T) {
	createdAt := time.Date(2026, time.October, 21, 12, 0, 0, 0, time.UTC)
	fixture := newPromotionIntegrationFixture(t, createdAt)
	request := fixture.request("external-dns-01jbe4q8")
	acceptance := fixture.acceptance(t, request, createdAt.Add(2*time.Minute))
	automatic, err := fixture.database.AcceptPromotionRequest(fixture.ctx, acceptance)
	if err != nil {
		t.Fatalf("AcceptPromotionRequest() automatic error = %v", err)
	}
	if automatic.ID != acceptance.ID || automatic.Status != promotion.StatusApproved ||
		automatic.ReviewRequired || automatic.RepositoryID != fixture.repositoryID ||
		automatic.PreparationRunID != nil {
		t.Errorf("automatic Promotion = %#v", automatic)
	}
	var storedSnapshotShape bool
	if err := fixture.database.pool.QueryRow(fixture.ctx, `
SELECT stage_snapshot->'freight' = '{"items":[]}'::jsonb
    AND stage_snapshot->'requestedFreight' = '[]'::jsonb
    AND NOT (stage_snapshot ? 'successfulRenderAcknowledgement')
    AND selected_freight_snapshot IS NULL
FROM promotions WHERE id = $1`, automatic.ID).Scan(&storedSnapshotShape); err != nil || !storedSnapshotShape {
		t.Errorf("stored snapshot shape = %v, %v", storedSnapshotShape, err)
	}
	for _, sha := range []string{automatic.BaseRevision, automatic.CandidateRevision} {
		known, commitErr := fixture.database.GetCommit(fixture.ctx, repository.CommitKey{
			RepositoryID: fixture.repositoryID,
			SHA:          sha,
		})
		if commitErr != nil || known.Commit.SHA != sha {
			t.Errorf("Promotion commit %s = %#v, %v", sha, known, commitErr)
		}
	}

	repeatedAcceptance := acceptance
	repeatedAcceptance.ID = uuid.New()
	repeated, err := fixture.database.AcceptPromotionRequest(fixture.ctx, repeatedAcceptance)
	if err != nil || repeated.ID != automatic.ID {
		t.Errorf("repeated request = %#v, %v", repeated, err)
	}
	repeatedAcceptance.Request.CandidateRevision = "a4cb21a88e1d0000000000000000000000000000"
	repeatedAcceptance.RequestDigest, err = repeatedAcceptance.Request.Digest()
	if err != nil {
		t.Fatalf("digest conflicting request: %v", err)
	}
	if _, err := fixture.database.AcceptPromotionRequest(
		fixture.ctx,
		repeatedAcceptance,
	); !errors.Is(err, promotion.ErrIdentityConflict) {
		t.Errorf("conflicting request error = %v", err)
	}

	policy := promotion.StagePolicy{
		StageID:              fixture.stage.ID,
		Mode:                 promotion.PolicyModeReviewRequired,
		UpdatedByPrincipalID: fixture.principal.ID,
		UpdatedBySnapshot:    fixture.actor,
		UpdatedAt:            createdAt.Add(3 * time.Minute),
	}
	if _, err := fixture.database.UpsertPromotionStagePolicy(fixture.ctx, policy); err != nil {
		t.Fatalf("create review policy: %v", err)
	}
	guardedRequest := request
	guardedRequest.ExternalPromotionID = "external-dns-01jbe4q9"
	guardedRequest.CandidateBranch = "kargo/promotion/external-dns-01jbe4q9"
	guardedDigest, err := guardedRequest.Digest()
	if err != nil {
		t.Fatalf("digest guarded request: %v", err)
	}
	guardedAcceptance := acceptance
	guardedAcceptance.ID = uuid.New()
	guardedAcceptance.Request = guardedRequest
	guardedAcceptance.RequestDigest = guardedDigest
	guardedAcceptance.AcceptedAt = createdAt.Add(4 * time.Minute)
	guarded, err := fixture.database.AcceptPromotionRequest(fixture.ctx, guardedAcceptance)
	if err != nil {
		t.Fatalf("AcceptPromotionRequest() guarded error = %v", err)
	}
	if guarded.Status != promotion.StatusPreparing || !guarded.ReviewRequired || guarded.ExpiresAt == nil ||
		!guarded.ExpiresAt.Equal(guardedAcceptance.AcceptedAt.Add(8*24*time.Hour)) {
		t.Errorf("guarded Promotion = %#v", guarded)
	}
	stageSnapshot := guarded.StageSnapshot
	if stageSnapshot.ResourceVersion != fixture.stage.ResourceVersion ||
		stageSnapshot.RepositoryName != fixture.stage.RepositoryName {
		t.Errorf("Stage snapshot = %#v", stageSnapshot)
	}
	policySnapshot := guarded.PolicySnapshot
	if policySnapshot.ConfiguredMode != promotion.PolicyModeReviewRequired ||
		policySnapshot.EffectiveMode != promotion.PolicyModeReviewRequired {
		t.Errorf("policy snapshot = %#v", policySnapshot)
	}

	staleRequest := guardedRequest
	staleRequest.ExternalPromotionID = "external-dns-01jbe4qa"
	staleRequest.CandidateBranch = "kargo/promotion/external-dns-01jbe4qa"
	staleDigest, err := staleRequest.Digest()
	if err != nil {
		t.Fatalf("digest stale request: %v", err)
	}
	staleAcceptance := guardedAcceptance
	staleAcceptance.ID = uuid.New()
	staleAcceptance.Request = staleRequest
	staleAcceptance.RequestDigest = staleDigest
	staleAcceptance.AcceptedAt = createdAt.Add(10 * time.Minute)
	if _, err := fixture.database.AcceptPromotionRequest(
		fixture.ctx,
		staleAcceptance,
	); !errors.Is(err, promotion.ErrStageStale) {
		t.Errorf("stale Stage request error = %v", err)
	}
}

func TestCreateOperatorPromotionIntegration(t *testing.T) {
	createdAt := time.Date(2026, time.October, 23, 12, 0, 0, 0, time.UTC)
	fixture := newPromotionIntegrationFixture(t, createdAt)
	fixture.stage.ResourceVersion = "2"
	fixture.stage.FirstObservedAt = createdAt.Add(2 * time.Minute)
	fixture.stage.LastObservedAt = createdAt.Add(2 * time.Minute)
	fixture.stage.Freight = promotion.Freight{Items: []promotion.FreightItem{{
		Origin: "Warehouse/external-dns",
		Name:   "freight-42",
		Artifacts: []promotion.FreightArtifact{{
			Kind: "image", Repository: new("ghcr.io/example/external-dns"), Version: "1.2.3",
		}},
	}}}
	fixture.stage.SuccessfulRenderAcknowledgement = &promotion.SuccessfulRenderAcknowledgement{
		SourceRevision: strings.Repeat("a", 40),
	}
	if err := fixture.database.ReplacePromotionStageSnapshot(
		fixture.ctx,
		promotionTestSnapshot(uuid.New(), "prod", "colo", createdAt.Add(2*time.Minute)),
		[]promotion.Stage{fixture.stage},
	); err != nil {
		t.Fatalf("update observed Stage Freight: %v", err)
	}

	acceptedAt := createdAt.Add(3 * time.Minute)
	acceptance := promotion.OperatorPromotionAcceptance{
		ID:                         uuid.New(),
		Integration:                "prod",
		TargetStageID:              fixture.stage.ID,
		SourceStageID:              fixture.stage.ID,
		FreightName:                "freight-42",
		RequestedByPrincipalID:     fixture.principal.ID,
		RequestedBySnapshot:        fixture.actor,
		AcceptedAt:                 acceptedAt,
		MaximumStageObservationAge: 5 * time.Minute,
		PendingReviewRetention:     8 * 24 * time.Hour,
	}
	record, err := fixture.database.CreateOperatorPromotion(fixture.ctx, acceptance)
	if err != nil {
		t.Fatalf("CreateOperatorPromotion() error = %v", err)
	}
	if record.Source != promotion.SourceSlipramp || record.Status != promotion.StatusCreating ||
		record.ExternalPromotionID != fixture.stage.Name+"-slipramp-"+record.ID.String() || record.SourceStageID == nil ||
		*record.SourceStageID != fixture.stage.ID || record.RequestDigest != (promotion.RequestDigest{}) ||
		record.BaseRevision != "" || record.CandidateRevision != "" || record.CandidateBranch != "" ||
		record.NextDispatchAt == nil || !record.NextDispatchAt.Equal(acceptedAt) {
		t.Errorf("operator Promotion = %#v", record)
	}
	selected := record.SelectedFreightSnapshot
	if selected == nil || selected.Name != "freight-42" || selected.Origin != "Warehouse/external-dns" {
		t.Errorf("selected Freight = %#v", selected)
	}

	loaded, err := fixture.database.GetPromotion(fixture.ctx, record.ID)
	if err != nil || loaded.Source != promotion.SourceSlipramp || loaded.Status != promotion.StatusCreating ||
		loaded.SelectedFreightSnapshot == nil || loaded.SelectedFreightSnapshot.Name != "freight-42" ||
		loaded.StageSnapshot.SuccessfulRenderAcknowledgement == nil ||
		loaded.StageSnapshot.SuccessfulRenderAcknowledgement.SourceRevision != strings.Repeat("a", 40) {
		t.Errorf("GetPromotion() = %#v, %v", loaded, err)
	}

	staleAcceptance := acceptance
	staleAcceptance.ID = uuid.New()
	staleAcceptance.AcceptedAt = createdAt.Add(10 * time.Minute)
	if _, err := fixture.database.CreateOperatorPromotion(
		fixture.ctx,
		staleAcceptance,
	); !errors.Is(err, promotion.ErrStageStale) {
		t.Errorf("stale Stage creation error = %v", err)
	}

	archivedAt := createdAt.Add(4 * time.Minute)
	if err := fixture.database.ReplacePromotionStageSnapshot(
		fixture.ctx,
		promotionTestSnapshot(uuid.New(), "prod", "colo", archivedAt),
		nil,
	); err != nil {
		t.Fatalf("archive observed Stage: %v", err)
	}
	archivedAcceptance := acceptance
	archivedAcceptance.ID = uuid.New()
	archivedAcceptance.AcceptedAt = archivedAt.Add(time.Second)
	if _, err := fixture.database.CreateOperatorPromotion(
		fixture.ctx,
		archivedAcceptance,
	); !errors.Is(err, promotion.ErrStageArchived) {
		t.Errorf("archived Stage creation error = %v", err)
	}
}

func TestHydrateOperatorPromotionRequestIntegration(t *testing.T) {
	createdAt := time.Date(2026, time.October, 23, 14, 0, 0, 0, time.UTC)
	fixture := newPromotionIntegrationFixture(t, createdAt)
	fixture.stage.ResourceVersion = "2"
	fixture.stage.FirstObservedAt = createdAt.Add(2 * time.Minute)
	fixture.stage.LastObservedAt = createdAt.Add(2 * time.Minute)
	fixture.stage.Freight = promotion.Freight{Items: []promotion.FreightItem{{
		Origin: "Warehouse/external-dns",
		Name:   "freight-42",
		Artifacts: []promotion.FreightArtifact{{
			Kind: "image", Repository: new("ghcr.io/example/external-dns"), Version: "1.2.3",
		}},
	}}}
	if err := fixture.database.ReplacePromotionStageSnapshot(
		fixture.ctx,
		promotionTestSnapshot(uuid.New(), "prod", "colo", fixture.stage.LastObservedAt),
		[]promotion.Stage{fixture.stage},
	); err != nil {
		t.Fatalf("update observed Stage Freight: %v", err)
	}

	operator := productidentity.Principal{
		ID:          uuid.New(),
		Kind:        productidentity.KindUser,
		DisplayName: "Promotion operator",
		Role:        productidentity.RoleOperator,
		CreatedAt:   createdAt,
		UpdatedAt:   createdAt,
	}
	if _, err := fixture.database.CreatePrincipal(fixture.ctx, operator); err != nil {
		t.Fatalf("create Promotion operator: %v", err)
	}
	operatorActor, err := operator.ActorSnapshot()
	if err != nil {
		t.Fatalf("snapshot Promotion operator: %v", err)
	}
	createOperatorPromotion := func(acceptedAt time.Time) promotion.Promotion {
		t.Helper()

		record, createErr := fixture.database.CreateOperatorPromotion(
			fixture.ctx,
			promotion.OperatorPromotionAcceptance{
				ID:                         uuid.New(),
				Integration:                fixture.stage.Integration,
				TargetStageID:              fixture.stage.ID,
				SourceStageID:              fixture.stage.ID,
				FreightName:                "freight-42",
				RequestedByPrincipalID:     operator.ID,
				RequestedBySnapshot:        operatorActor,
				AcceptedAt:                 acceptedAt,
				MaximumStageObservationAge: 5 * time.Minute,
				PendingReviewRetention:     8 * 24 * time.Hour,
			},
		)
		if createErr != nil {
			t.Fatalf("create operator Promotion: %v", createErr)
		}

		return record
	}

	automatic := createOperatorPromotion(createdAt.Add(150 * time.Second))
	claimed, err := fixture.database.ClaimPromotionCreation(
		fixture.ctx,
		promotion.CreationDispatchClaim{
			ClusterName:      fixture.stage.ClusterName,
			ClaimedAt:        createdAt.Add(3 * time.Minute),
			RetryDelays:      []time.Duration{time.Minute},
			ExhaustedMessage: "Promotion creation exhausted its retry budget.",
		},
	)
	if err != nil || claimed.ID != automatic.ID || claimed.DispatchAttemptCount != 1 {
		t.Fatalf("claim operator Promotion = %#v, %v", claimed, err)
	}
	callbackAt := createdAt.Add(190 * time.Second)
	callback := fixture.acceptance(t, fixture.request(automatic.ExternalPromotionID), callbackAt)
	hydrated, err := fixture.database.AcceptPromotionRequest(fixture.ctx, callback)
	if err != nil {
		t.Fatalf("hydrate automatic operator Promotion: %v", err)
	}
	if hydrated.ID != automatic.ID || hydrated.Source != promotion.SourceSlipramp ||
		hydrated.Status != promotion.StatusApproved || hydrated.RequestDigest != callback.RequestDigest ||
		hydrated.BaseRevision != callback.Request.BaseRevision || hydrated.NextDispatchAt != nil ||
		hydrated.RequestedByPrincipalID != operator.ID || hydrated.RequestedBySnapshot != operatorActor {
		t.Errorf("hydrated automatic Promotion = %#v", hydrated)
	}
	knownCandidate, err := fixture.database.GetCommit(fixture.ctx, repository.CommitKey{
		RepositoryID: fixture.repositoryID,
		SHA:          hydrated.CandidateRevision,
	})
	if err != nil || knownCandidate.Commit.SHA != hydrated.CandidateRevision {
		t.Errorf("hydrated Promotion candidate = %#v, %v", knownCandidate, err)
	}

	uid := "kargo-promotion-uid"
	kargoName := automatic.ExternalPromotionID
	reported, err := fixture.database.ReportPromotionCreation(
		fixture.ctx,
		promotion.CreationDispatchReport{
			PromotionID: automatic.ID,
			ClusterName: fixture.stage.ClusterName,
			Attempt:     claimed.DispatchAttemptCount,
			Result:      promotion.CreationDispatchCreated,
			KargoName:   &kargoName,
			KargoUID:    &uid,
			ReportedAt:  createdAt.Add(200 * time.Second),
		},
	)
	if err != nil || reported.Status != promotion.StatusApproved || reported.KargoPromotionUID == nil ||
		*reported.KargoPromotionUID != uid {
		t.Errorf("late successful dispatch report = %#v, %v", reported, err)
	}

	repeated := callback
	repeated.ID = uuid.New()
	repeated.AcceptedAt = createdAt.Add(210 * time.Second)
	recovered, err := fixture.database.AcceptPromotionRequest(fixture.ctx, repeated)
	if err != nil || recovered.ID != automatic.ID || recovered.KargoPromotionUID == nil {
		t.Errorf("repeated hydrated callback = %#v, %v", recovered, err)
	}
	conflicting := callback
	conflicting.ID = uuid.New()
	conflicting.Request.CandidateRevision = "a4cb21a88e1d0000000000000000000000000000"
	conflicting.RequestDigest, err = conflicting.Request.Digest()
	if err != nil {
		t.Fatalf("digest conflicting callback: %v", err)
	}
	if _, err := fixture.database.AcceptPromotionRequest(
		fixture.ctx,
		conflicting,
	); !errors.Is(err, promotion.ErrIdentityConflict) {
		t.Errorf("conflicting hydrated callback error = %v", err)
	}

	mismatched := createOperatorPromotion(createdAt.Add(220 * time.Second))
	mismatchedCallback := fixture.acceptance(
		t,
		fixture.request(mismatched.ExternalPromotionID),
		createdAt.Add(230*time.Second),
	)
	mismatchedCallback.Request.Stage = "another-stage"
	mismatchedCallback.RequestDigest, err = mismatchedCallback.Request.Digest()
	if err != nil {
		t.Fatalf("digest mismatched callback: %v", err)
	}
	if _, err := fixture.database.AcceptPromotionRequest(
		fixture.ctx,
		mismatchedCallback,
	); !errors.Is(err, promotion.ErrIdentityConflict) {
		t.Errorf("mismatched Stage callback error = %v", err)
	}
	unchanged, err := fixture.database.GetPromotion(fixture.ctx, mismatched.ID)
	if err != nil || unchanged.Status != promotion.StatusCreating ||
		unchanged.RequestDigest != (promotion.RequestDigest{}) {
		t.Errorf("mismatched callback changed Promotion = %#v, %v", unchanged, err)
	}

	concurrent := createOperatorPromotion(createdAt.Add(235 * time.Second))
	concurrentCallback := fixture.acceptance(
		t,
		fixture.request(concurrent.ExternalPromotionID),
		createdAt.Add(238*time.Second),
	)
	type callbackResult struct {
		record promotion.Promotion
		err    error
	}
	callbackResults := make(chan callbackResult, 2)
	for range 2 {
		acceptance := concurrentCallback
		acceptance.ID = uuid.New()
		go func() {
			record, acceptErr := fixture.database.AcceptPromotionRequest(fixture.ctx, acceptance)
			callbackResults <- callbackResult{record: record, err: acceptErr}
		}()
	}
	firstCallback := <-callbackResults
	secondCallback := <-callbackResults
	if firstCallback.err != nil || secondCallback.err != nil || firstCallback.record.ID != concurrent.ID ||
		secondCallback.record.ID != concurrent.ID || firstCallback.record.Status != promotion.StatusApproved ||
		secondCallback.record.Status != promotion.StatusApproved {
		t.Errorf("concurrent callbacks = %#v and %#v", firstCallback, secondCallback)
	}

	policyUpdatedAt := createdAt.Add(4 * time.Minute)
	if _, err := fixture.database.UpsertPromotionStagePolicy(fixture.ctx, promotion.StagePolicy{
		StageID:              fixture.stage.ID,
		Mode:                 promotion.PolicyModeReviewRequired,
		UpdatedByPrincipalID: operator.ID,
		UpdatedBySnapshot:    operatorActor,
		UpdatedAt:            policyUpdatedAt,
	}); err != nil {
		t.Fatalf("create guarded policy: %v", err)
	}
	guardedCreatedAt := createdAt.Add(250 * time.Second)
	guarded := createOperatorPromotion(guardedCreatedAt)
	guardedCallback := fixture.acceptance(
		t,
		fixture.request(guarded.ExternalPromotionID),
		createdAt.Add(260*time.Second),
	)
	guarded, err = fixture.database.AcceptPromotionRequest(fixture.ctx, guardedCallback)
	if err != nil {
		t.Fatalf("hydrate guarded operator Promotion: %v", err)
	}
	if guarded.Status != promotion.StatusPreparing || !guarded.ReviewRequired || guarded.ExpiresAt == nil ||
		!guarded.ExpiresAt.Equal(guardedCreatedAt.Add(8*24*time.Hour)) ||
		guarded.RequestedByPrincipalID != operator.ID || guarded.NextDispatchAt != nil {
		t.Errorf("hydrated guarded Promotion = %#v", guarded)
	}
}

func TestConcurrentPromotionRequestPollsRecoverOneRecordIntegration(t *testing.T) {
	createdAt := time.Date(2026, time.October, 21, 14, 0, 0, 0, time.UTC)
	fixture := newPromotionIntegrationFixture(t, createdAt)
	base := fixture.acceptance(t, fixture.request("metrics-01jbe4q8"), createdAt.Add(2*time.Minute))

	type result struct {
		record promotion.Promotion
		err    error
	}
	results := make(chan result, 2)
	for range 2 {
		acceptance := base
		acceptance.ID = uuid.New()
		go func() {
			record, acceptErr := fixture.database.AcceptPromotionRequest(fixture.ctx, acceptance)
			results <- result{record: record, err: acceptErr}
		}()
	}
	first := <-results
	second := <-results
	if first.err != nil {
		t.Fatalf("first concurrent poll error = %v", first.err)
	}
	if second.err != nil {
		t.Fatalf("second concurrent poll error = %v", second.err)
	}
	if first.record.ID == uuid.Nil || first.record.ID != second.record.ID {
		t.Errorf("concurrent Promotion IDs = %s and %s", first.record.ID, second.record.ID)
	}
}

func TestPromotionLifecycleIntegration(t *testing.T) {
	createdAt := time.Date(2026, time.October, 21, 12, 0, 0, 0, time.UTC)
	fixture := newPromotionIntegrationFixture(t, createdAt)
	ctx := fixture.ctx
	database := fixture.database

	reason := "DNS migration"
	policy := promotion.StagePolicy{
		StageID:              fixture.stage.ID,
		Mode:                 promotion.PolicyModeReviewRequired,
		ExpiresAt:            new(createdAt.Add(24 * time.Hour)),
		Reason:               &reason,
		UpdatedByPrincipalID: fixture.principal.ID,
		UpdatedBySnapshot:    fixture.actor,
		UpdatedAt:            createdAt.Add(3 * time.Minute),
	}
	storedPolicy, err := database.UpsertPromotionStagePolicy(ctx, policy)
	if err != nil {
		t.Fatalf("UpsertPromotionStagePolicy() error = %v", err)
	}
	if storedPolicy.Mode != promotion.PolicyModeReviewRequired || storedPolicy.Reason == nil ||
		*storedPolicy.Reason != reason {
		t.Errorf("stored policy = %#v", storedPolicy)
	}
	request := fixture.request("external-dns-01jbe4q8")
	acceptance := fixture.acceptance(t, request, createdAt.Add(4*time.Minute))
	promotionRecord, err := database.AcceptPromotionRequest(ctx, acceptance)
	if err != nil {
		t.Fatalf("accept guarded Promotion: %v", err)
	}

	preparationRun, err := database.GetRun(ctx, fixture.runID)
	if err != nil {
		t.Fatalf("get preparation Run template: %v", err)
	}
	preparationRun.ID = uuid.New()
	preparationRun.Source = run.Source{
		Type:      run.SourceTypePromotion,
		Promotion: &run.PromotionRunSource{PromotionID: promotionRecord.ID},
	}
	preparationRunKey := promotionRecord.ID.String()
	preparationRun.IdempotencyKey = &preparationRunKey
	preparationRun.RequestDigest = strings.Repeat("a", 64)
	preparationRun.CreatedBy = &fixture.actor
	preparationRun.CreatedAt = createdAt.Add(4*time.Minute + time.Second)
	promotionRecord, err = database.ReservePromotionPreparationRevision(
		ctx,
		promotionRecord.ID,
		preparationRun.RepositoryID,
		preparationRun.CatalogRevisionID,
		createdAt.Add(4*time.Minute+time.Second),
	)
	if err != nil {
		t.Fatalf("reserve preparation revision: %v", err)
	}
	_, err = database.pool.Exec(ctx, `CREATE FUNCTION reject_preparation_link() RETURNS trigger LANGUAGE plpgsql AS
        $$ BEGIN RAISE EXCEPTION 'simulated preparation link failure'; END; $$;
        CREATE TRIGGER reject_preparation_link BEFORE UPDATE OF preparation_run_id ON promotions
        FOR EACH ROW EXECUTE FUNCTION reject_preparation_link();`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateRun(ctx, preparationRun, nil); err == nil {
		t.Fatal("expected preparation link failure")
	}
	assertRunCounts(t, database, 1, 1)
	if _, err := database.pool.Exec(
		ctx, "DROP TRIGGER reject_preparation_link ON promotions; DROP FUNCTION reject_preparation_link()",
	); err != nil {
		t.Fatal(err)
	}
	preparationRun, err = database.CreateRun(ctx, preparationRun, nil)
	if err != nil {
		t.Fatalf("create preparation Run: %v", err)
	}
	runID := preparationRun.ID
	promotionRecord, err = database.GetPromotion(ctx, promotionRecord.ID)
	if err != nil || promotionRecord.PreparationRunID == nil || *promotionRecord.PreparationRunID != runID {
		t.Fatalf("atomically linked preparation Run = %#v, %v", promotionRecord, err)
	}
	gitArtifactID := createPromotionArtifact(
		t,
		database,
		runID,
		"git-diff",
		"text/x-diff; charset=utf-8",
		createdAt.Add(5*time.Minute),
	)
	argoArtifactID := createPromotionArtifact(
		t,
		database,
		runID,
		"argo-diff",
		"application/vnd.slipramp.argocd-diff+json",
		createdAt.Add(5*time.Minute),
	)
	preparationCompletedAt := createdAt.Add(6 * time.Minute)
	promotionRecord, err = database.CompletePromotionPreparation(
		ctx,
		promotionRecord.ID,
		runID,
		gitArtifactID,
		argoArtifactID,
		preparationCompletedAt,
		nil,
	)
	if err != nil {
		t.Fatalf("complete Promotion preparation: %v", err)
	}
	if promotionRecord.Status != promotion.StatusPending {
		t.Errorf("prepared Promotion = %#v", promotionRecord)
	}
	if _, err := database.CancelRun(ctx, runID, createdAt.Add(7*time.Minute), nil); err != nil {
		t.Fatalf("finish preparation Run for retention check: %v", err)
	}
	expired, err := database.ExpireArtifacts(ctx, createdAt.Add(24*time.Hour), 10)
	if err != nil {
		t.Fatalf("expire pending Promotion artifacts: %v", err)
	}
	if len(expired) != 0 {
		t.Errorf("pending Promotion artifacts expired = %#v", expired)
	}

	failedRequest := fixture.request("external-dns-01jbe4q9")
	failedAcceptance := fixture.acceptance(t, failedRequest, createdAt.Add(4*time.Minute+30*time.Second))
	preparing, err := database.AcceptPromotionRequest(ctx, failedAcceptance)
	if err != nil {
		t.Fatalf("accept second guarded Promotion: %v", err)
	}
	preparationRun.ID = uuid.New()
	preparationRun.Source.Promotion.PromotionID = preparing.ID
	preparationRunKey = preparing.ID.String()
	preparationRun.IdempotencyKey = &preparationRunKey
	preparationRun.RequestDigest = strings.Repeat("b", 64)
	preparationRun.CreatedAt = createdAt.Add(8 * time.Minute)
	preparing, err = database.ReservePromotionPreparationRevision(
		ctx,
		preparing.ID,
		preparationRun.RepositoryID,
		preparationRun.CatalogRevisionID,
		preparationRun.CreatedAt,
	)
	if err != nil {
		t.Fatalf("reserve second preparation revision: %v", err)
	}
	preparationRun, err = database.CreateRun(ctx, preparationRun, nil)
	if err != nil {
		t.Fatalf("create second preparation Run: %v", err)
	}
	failed, err := database.FailPromotionPreparation(
		ctx,
		preparing.ID,
		preparationRun.ID,
		"Argo CD diff failed",
		createdAt.Add(8*time.Minute+3*time.Second),
	)
	if err != nil || failed.Status != promotion.StatusError {
		t.Errorf("FailPromotionPreparation() = %#v, %v", failed, err)
	}
	comment := "Validated the RBAC migration"
	decision := promotion.Decision{
		PromotionID:          promotionRecord.ID,
		Decision:             promotion.DecisionApproved,
		Comment:              &comment,
		DecidedByPrincipalID: fixture.principal.ID,
		DecidedBySnapshot:    fixture.actor,
		DecidedAt:            createdAt.Add(7 * time.Minute),
	}
	decided, storedDecision, err := database.DecidePromotion(ctx, decision)
	if err != nil {
		t.Fatalf("DecidePromotion() error = %v", err)
	}
	if decided.Status != promotion.StatusApproved || storedDecision.Decision != promotion.DecisionApproved {
		t.Errorf("decision result = %#v, %#v", decided, storedDecision)
	}
	expired, err = database.ExpireArtifacts(ctx, createdAt.Add(24*time.Hour), 10)
	if err != nil {
		t.Fatalf("expire terminal Promotion artifacts: %v", err)
	}
	if len(expired) != 2 {
		t.Errorf("terminal Promotion artifacts expired = %#v, want 2", expired)
	}

	repeated, _, err := database.DecidePromotion(ctx, decision)
	if err != nil || repeated.Status != promotion.StatusApproved {
		t.Errorf("repeated DecidePromotion() = %#v, %v", repeated, err)
	}
	conflictingDecision := decision
	conflictingDecision.Decision = promotion.DecisionRejected
	if _, _, err := database.DecidePromotion(ctx, conflictingDecision); !errors.Is(err, promotion.ErrDecisionConflict) {
		t.Errorf("conflicting DecidePromotion() error = %v", err)
	}
}

func TestPromotionPreparationCompletionEnqueuesRoutedNotificationAndKeepsInboxIndependentIntegration(t *testing.T) {
	createdAt := time.Date(2026, time.October, 23, 12, 0, 0, 0, time.UTC)
	fixture := newPromotionIntegrationFixture(t, createdAt)
	service := notificationServiceForDestinations(t, fixture.database, "operations")
	preparation := preparePromotionForCompletion(
		t,
		fixture,
		"external-dns-notification",
		createdAt.Add(10*time.Second),
	)
	event := promotionReviewNotification(t, preparation.record, preparation.completedAt, "Promotion needs review")
	plan, err := service.Plan(event)
	if err != nil || plan == nil {
		t.Fatalf("Plan() = %#v, %v", plan, err)
	}

	pending, err := fixture.database.CompletePromotionPreparation(
		fixture.ctx,
		preparation.record.ID,
		preparation.runID,
		preparation.gitArtifactID,
		preparation.argoArtifactID,
		preparation.completedAt,
		plan,
	)
	if err != nil || pending.Status != promotion.StatusPending {
		t.Fatalf("CompletePromotionPreparation() = %#v, %v", pending, err)
	}
	page, err := service.ListDeliveries(fixture.ctx, notification.DeliveryHistoryFilter{Limit: 25})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].EventID != event.ID ||
		page.Items[0].SubjectID != pending.ID || page.Items[0].DestinationName != "operations" {
		t.Fatalf("notification delivery history = %#v", page)
	}
	routedPending := pending

	if _, err := fixture.database.CompletePromotionPreparation(
		fixture.ctx,
		preparation.record.ID,
		preparation.runID,
		preparation.gitArtifactID,
		preparation.argoArtifactID,
		preparation.completedAt,
		plan,
	); !errors.Is(err, promotion.ErrTransitionConflict) {
		t.Fatalf("repeated completion error = %v", err)
	}
	page, err = service.ListDeliveries(fixture.ctx, notification.DeliveryHistoryFilter{Limit: 25})
	if err != nil || page.Total != 1 {
		t.Fatalf("delivery history after repeated completion = %#v, %v", page, err)
	}

	unrouted, err := notification.NewService(notification.DefaultConfig(), fixture.database)
	if err != nil {
		t.Fatal(err)
	}
	unroutedPreparation := preparePromotionForCompletion(
		t,
		fixture,
		"external-dns-unrouted",
		createdAt.Add(30*time.Second),
	)
	unroutedEvent := promotionReviewNotification(
		t,
		unroutedPreparation.record,
		unroutedPreparation.completedAt,
		"Promotion needs review",
	)
	unroutedPlan, err := unrouted.Plan(unroutedEvent)
	if err != nil || unroutedPlan != nil {
		t.Fatalf("unrouted Plan() = %#v, %v", unroutedPlan, err)
	}
	pending, err = fixture.database.CompletePromotionPreparation(
		fixture.ctx,
		unroutedPreparation.record.ID,
		unroutedPreparation.runID,
		unroutedPreparation.gitArtifactID,
		unroutedPreparation.argoArtifactID,
		unroutedPreparation.completedAt,
		unroutedPlan,
	)
	if err != nil || pending.Status != promotion.StatusPending {
		t.Fatalf("unrouted CompletePromotionPreparation() = %#v, %v", pending, err)
	}
	unroutedPending := pending
	page, err = service.ListDeliveries(fixture.ctx, notification.DeliveryHistoryFilter{Limit: 25})
	if err != nil || page.Total != 1 {
		t.Fatalf("delivery history after unrouted completion = %#v, %v", page, err)
	}

	inboxPromotions, err := fixture.database.ListInboxPromotions(fixture.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(inboxPromotions) != 2 || inboxPromotions[0].ID != unroutedPending.ID ||
		inboxPromotions[1].ID != routedPending.ID {
		t.Fatalf("Inbox Promotions with routed and unrouted notifications = %#v", inboxPromotions)
	}

	attempt, err := fixture.database.ClaimNextNotificationDelivery(
		fixture.ctx,
		notification.DeliveryClaim{
			AttemptedAt:    createdAt.Add(time.Minute),
			MaximumBackoff: time.Minute,
			Jitter:         1,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if updated, failErr := fixture.database.FailNotificationDelivery(
		fixture.ctx,
		attempt.ID,
		attempt.AttemptCount,
		createdAt.Add(time.Minute+time.Second),
		"Slack temporarily rejected the delivery (HTTP 503).",
	); failErr != nil || !updated {
		t.Fatalf("FailNotificationDelivery() = %t, %v", updated, failErr)
	}

	inboxPromotions, err = fixture.database.ListInboxPromotions(fixture.ctx)
	if err != nil || len(inboxPromotions) != 2 {
		t.Fatalf("Inbox Promotions after notification failure = %#v, %v", inboxPromotions, err)
	}
	if _, _, err := fixture.database.DecidePromotion(fixture.ctx, promotion.Decision{
		PromotionID:          routedPending.ID,
		Decision:             promotion.DecisionApproved,
		DecidedByPrincipalID: fixture.principal.ID,
		DecidedBySnapshot:    fixture.actor,
		DecidedAt:            createdAt.Add(2 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	inboxPromotions, err = fixture.database.ListInboxPromotions(fixture.ctx)
	if err != nil || len(inboxPromotions) != 1 || inboxPromotions[0].ID != unroutedPending.ID {
		t.Fatalf("Inbox Promotions after resolving routed review = %#v, %v", inboxPromotions, err)
	}
	page, err = service.ListDeliveries(fixture.ctx, notification.DeliveryHistoryFilter{Limit: 25})
	if err != nil || page.Total != 1 || page.Items[0].State != notification.DeliveryStateFailed {
		t.Fatalf("delivery history after resolving Inbox decision = %#v, %v", page, err)
	}
}

func TestPromotionPreparationNotificationFailureRollsBackTransitionIntegration(t *testing.T) {
	createdAt := time.Date(2026, time.October, 23, 13, 0, 0, 0, time.UTC)
	fixture := newPromotionIntegrationFixture(t, createdAt)
	service := notificationServiceForDestinations(t, fixture.database, "operations")
	preparation := preparePromotionForCompletion(
		t,
		fixture,
		"external-dns-atomicity",
		createdAt.Add(10*time.Second),
	)
	event := promotionReviewNotification(t, preparation.record, preparation.completedAt, "Promotion needs review")
	plan, err := service.Plan(event)
	if err != nil || plan == nil {
		t.Fatalf("Plan() = %#v, %v", plan, err)
	}

	_, err = fixture.database.pool.Exec(fixture.ctx, `CREATE FUNCTION reject_notification_delivery() RETURNS trigger LANGUAGE plpgsql AS
        $$ BEGIN RAISE EXCEPTION 'simulated notification delivery storage failure'; END; $$;
        CREATE TRIGGER reject_notification_delivery BEFORE INSERT ON notification_deliveries
        FOR EACH ROW EXECUTE FUNCTION reject_notification_delivery();`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.CompletePromotionPreparation(
		fixture.ctx,
		preparation.record.ID,
		preparation.runID,
		preparation.gitArtifactID,
		preparation.argoArtifactID,
		preparation.completedAt,
		plan,
	); err == nil || !strings.Contains(err.Error(), "simulated notification delivery storage failure") {
		t.Fatalf("completion error = %v", err)
	}

	stored, err := fixture.database.GetPromotion(fixture.ctx, preparation.record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != promotion.StatusPreparing || stored.PreparationCompletedAt != nil {
		t.Fatalf("Promotion after notification rollback = %#v", stored)
	}
	var eventCount, deliveryCount int
	if err := fixture.database.pool.QueryRow(
		fixture.ctx,
		"SELECT (SELECT count(*) FROM notification_events), (SELECT count(*) FROM notification_deliveries)",
	).Scan(&eventCount, &deliveryCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 0 || deliveryCount != 0 {
		t.Fatalf("notification rows after rollback = events %d, deliveries %d", eventCount, deliveryCount)
	}
}

func promotionReviewNotification(
	t *testing.T,
	record promotion.Promotion,
	occurredAt time.Time,
	title string,
) notification.Event {
	t.Helper()

	event, err := notification.NewReviewRequestedEvent(notification.ReviewRequestedInput{
		ID:               uuid.New(),
		SubjectType:      notification.SubjectTypePromotion,
		SubjectID:        record.ID,
		OccurredAt:       occurredAt,
		DeduplicationKey: "promotion:" + record.ID.String() + ":review-requested",
		Title:            title,
		Summary:          "kargo-core/external-dns-prod is ready for review",
		Facts:            []notification.Fact{{Label: "Repository", Value: "kcloud-ops"}},
		Action: notification.Action{
			Label: "Review in Slipramp",
			Path:  "/promotions/" + record.ID.String(),
		},
		ExpiresAt: record.ExpiresAt,
		CreatedAt: occurredAt,
	})
	if err != nil {
		t.Fatal(err)
	}

	return event
}

func TestPromotionDecisionAtExpiryPersistsExpiredStateIntegration(t *testing.T) {
	createdAt := time.Date(2026, time.October, 21, 12, 0, 0, 0, time.UTC)
	fixture := newPromotionIntegrationFixture(t, createdAt)
	pending := preparePromotionForReview(
		t,
		fixture,
		"expiry-race-01jbe4q8",
		createdAt.Add(2*time.Minute),
	)
	if pending.ExpiresAt == nil {
		t.Fatal("prepared Promotion expiry is nil")
	}

	decision := promotion.Decision{
		PromotionID:          pending.ID,
		Decision:             promotion.DecisionApproved,
		DecidedByPrincipalID: fixture.principal.ID,
		DecidedBySnapshot:    fixture.actor,
		DecidedAt:            *pending.ExpiresAt,
	}
	if _, _, err := fixture.database.DecidePromotion(
		fixture.ctx,
		decision,
	); !errors.Is(err, promotion.ErrTransitionConflict) {
		t.Fatalf("DecidePromotion() expiry error = %v", err)
	}

	stored, err := fixture.database.GetPromotion(fixture.ctx, pending.ID)
	if err != nil {
		t.Fatalf("GetPromotion() error = %v", err)
	}
	if stored.Status != promotion.StatusExpired || stored.Message == nil ||
		!strings.Contains(*stored.Message, "expired") {
		t.Errorf("expired Promotion = %#v", stored)
	}
	if _, err := fixture.database.GetPromotionDecision(
		fixture.ctx,
		pending.ID,
	); !errors.Is(err, promotion.ErrDecisionNotFound) {
		t.Errorf("GetPromotionDecision() error = %v, want not found", err)
	}
}

func TestRepeatedPromotionRequestExpiresOnlyMatchingReviewIntegration(t *testing.T) {
	createdAt := time.Date(2026, time.October, 24, 12, 0, 0, 0, time.UTC)
	fixture := newPromotionIntegrationFixture(t, createdAt)
	pending := preparePromotionForReview(t, fixture, "callback-expiry", createdAt.Add(2*time.Minute))
	unrelated := preparePromotionForReview(t, fixture, "unrelated-expiry", pending.CreatedAt)
	if pending.ExpiresAt == nil || unrelated.ExpiresAt == nil || !pending.ExpiresAt.Equal(*unrelated.ExpiresAt) {
		t.Fatal("prepared Promotions must share a review deadline")
	}

	callback := fixture.acceptance(t, fixture.request(pending.ExternalPromotionID), *pending.ExpiresAt)
	beforeDeadline := callback
	beforeDeadline.AcceptedAt = pending.ExpiresAt.Add(-time.Microsecond)
	unchanged, err := fixture.database.AcceptPromotionRequest(fixture.ctx, beforeDeadline)
	if err != nil || unchanged.ID != pending.ID || unchanged.Status != promotion.StatusPending ||
		!unchanged.UpdatedAt.Equal(pending.UpdatedAt) || unchanged.Message != nil {
		t.Fatalf("callback before expiry = %#v, %v", unchanged, err)
	}

	conflicting := callback
	conflicting.Request.CandidateRevision = strings.Repeat("a", 40)
	conflicting.RequestDigest, err = conflicting.Request.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.AcceptPromotionRequest(
		fixture.ctx, conflicting,
	); !errors.Is(err, promotion.ErrIdentityConflict) {
		t.Fatalf("conflicting callback at expiry error = %v", err)
	}
	unchanged, err = fixture.database.GetPromotion(fixture.ctx, pending.ID)
	if err != nil || unchanged.Status != promotion.StatusPending ||
		!unchanged.UpdatedAt.Equal(pending.UpdatedAt) || unchanged.Message != nil {
		t.Fatalf("conflicting callback changed Promotion = %#v, %v", unchanged, err)
	}

	for _, test := range []struct {
		name string
		at   time.Time
	}{
		{name: "exact deadline", at: *pending.ExpiresAt},
		{name: "repeated expired callback", at: pending.ExpiresAt.Add(time.Minute)},
	} {
		t.Run(test.name, func(t *testing.T) {
			acceptance := callback
			acceptance.AcceptedAt = test.at
			record, err := fixture.database.AcceptPromotionRequest(fixture.ctx, acceptance)
			if err != nil || record.ID != pending.ID || record.Status != promotion.StatusExpired ||
				record.Message == nil || *record.Message != "The review window expired before a decision was recorded." ||
				!record.UpdatedAt.Equal(*pending.ExpiresAt) {
				t.Fatalf("expired callback = %#v, %v", record, err)
			}
			stored, err := fixture.database.GetPromotion(fixture.ctx, pending.ID)
			if err != nil || stored.Status != record.Status || stored.Message == nil ||
				*stored.Message != *record.Message || !stored.UpdatedAt.Equal(record.UpdatedAt) {
				t.Errorf("persisted expired Promotion = %#v, %v", stored, err)
			}
		})
	}

	unchanged, err = fixture.database.GetPromotion(fixture.ctx, unrelated.ID)
	if err != nil || unchanged.Status != promotion.StatusPending ||
		!unchanged.UpdatedAt.Equal(unrelated.UpdatedAt) || unchanged.Message != nil {
		t.Errorf("unrelated eligible Promotion changed = %#v, %v", unchanged, err)
	}
}

func TestConcurrentPromotionDecisionsRecordOneImmutableResultIntegration(t *testing.T) {
	createdAt := time.Date(2026, time.October, 21, 12, 0, 0, 0, time.UTC)
	fixture := newPromotionIntegrationFixture(t, createdAt)
	pending := preparePromotionForReview(
		t,
		fixture,
		"decision-race-01jbe4q8",
		createdAt.Add(2*time.Minute),
	)

	type result struct {
		decision promotion.DecisionValue
		err      error
	}
	results := make(chan result, 2)
	for _, value := range []promotion.DecisionValue{
		promotion.DecisionApproved,
		promotion.DecisionRejected,
	} {
		go func() {
			_, _, err := fixture.database.DecidePromotion(
				fixture.ctx,
				promotion.Decision{
					PromotionID:          pending.ID,
					Decision:             value,
					DecidedByPrincipalID: fixture.principal.ID,
					DecidedBySnapshot:    fixture.actor,
					DecidedAt:            createdAt.Add(10 * time.Minute),
				},
			)
			results <- result{decision: value, err: err}
		}()
	}

	first := <-results
	second := <-results
	succeeded := 0
	conflicted := 0
	for _, item := range []result{first, second} {
		switch {
		case item.err == nil:
			succeeded++
		case errors.Is(item.err, promotion.ErrDecisionConflict):
			conflicted++
		default:
			t.Errorf("decision %s error = %v", item.decision, item.err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("concurrent results = %#v, %#v", first, second)
	}

	stored, err := fixture.database.GetPromotionDecision(fixture.ctx, pending.ID)
	if err != nil {
		t.Fatalf("GetPromotionDecision() error = %v", err)
	}
	current, err := fixture.database.GetPromotion(fixture.ctx, pending.ID)
	if err != nil {
		t.Fatalf("GetPromotion() error = %v", err)
	}
	if promotion.PromotionStatus(stored.Decision) != current.Status {
		t.Errorf("decision = %s, Promotion status = %s", stored.Decision, current.Status)
	}
}

func TestPromotionReviewStateSurvivesStoreRestartIntegration(t *testing.T) {
	createdAt := time.Now().UTC().Truncate(time.Microsecond)
	fixture := newPromotionIntegrationFixture(t, createdAt)
	pending := preparePromotionForReview(
		t,
		fixture,
		"restart-review-01jbe4q8",
		createdAt.Add(2*time.Minute),
	)
	fixture.database.Close()

	reopened, err := Open(fixture.ctx, fixture.config)
	if err != nil {
		t.Fatalf("Open() after restart error = %v", err)
	}
	t.Cleanup(reopened.Close)
	reviewService := promotion.NewReviewService(reopened)
	review, err := reviewService.GetPromotion(fixture.ctx, pending.ID)
	if err != nil {
		t.Fatalf("GetPromotion() after restart error = %v", err)
	}
	if review.Promotion.Status != promotion.StatusPending || review.Decision != nil ||
		review.Promotion.GitDiffArtifactID == nil || review.Promotion.ArgoDiffArtifactID == nil {
		t.Errorf("review after restart = %#v", review)
	}
	policy, err := reopened.GetPromotionStagePolicy(fixture.ctx, fixture.stage.ID)
	if err != nil {
		t.Fatalf("GetPromotionStagePolicy() after restart error = %v", err)
	}
	if policy.Mode != promotion.PolicyModeReviewRequired {
		t.Errorf("policy after restart = %#v", policy)
	}
}

func TestReplacePromotionStageSnapshotIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	observedAt := time.Date(2026, time.October, 21, 12, 0, 0, 0, time.UTC)
	firstSnapshot := promotionTestSnapshot(uuid.New(), "prod", "colo", observedAt)
	firstStages := []promotion.Stage{
		promotionTestStage("prod", "colo", "kargo-core", "external-dns-prod", observedAt),
		promotionTestStage("prod", "colo", "kargo-core", "metrics-server-prod", observedAt),
	}
	if err := database.ReplacePromotionStageSnapshot(ctx, firstSnapshot, firstStages); err != nil {
		t.Fatalf("first ReplacePromotionStageSnapshot() error = %v", err)
	}
	if err := database.ReplacePromotionStageSnapshot(ctx, firstSnapshot, firstStages); err != nil {
		t.Fatalf("repeated ReplacePromotionStageSnapshot() error = %v", err)
	}

	identityConflict := firstSnapshot
	identityConflict.ObservedAt = identityConflict.ObservedAt.Add(time.Second)
	identityConflict.AcceptedAt = identityConflict.AcceptedAt.Add(time.Second)
	if err := database.ReplacePromotionStageSnapshot(ctx, identityConflict, firstStages); !errors.Is(err, promotion.ErrSnapshotConflict) {
		t.Fatalf("snapshot identity conflict error = %v", err)
	}

	secondObservedAt := observedAt.Add(time.Minute)
	secondSnapshot := promotionTestSnapshot(uuid.New(), "prod", "colo", secondObservedAt)
	externalDNS := promotionTestStage("prod", "colo", "kargo-core", "external-dns-prod", secondObservedAt)
	externalDNS.ResourceVersion = "2"
	if err := database.ReplacePromotionStageSnapshot(
		ctx,
		secondSnapshot,
		[]promotion.Stage{externalDNS},
	); err != nil {
		t.Fatalf("second ReplacePromotionStageSnapshot() error = %v", err)
	}

	active, err := database.GetPromotionStageByIdentity(ctx, "prod", "kargo-core", "external-dns-prod")
	if err != nil {
		t.Fatalf("get active Stage: %v", err)
	}
	if active.ID != firstStages[0].ID || active.ResourceVersion != "2" || active.ArchivedAt != nil {
		t.Errorf("active Stage = %#v", active)
	}
	archived, err := database.GetPromotionStageByIdentity(ctx, "prod", "kargo-core", "metrics-server-prod")
	if err != nil {
		t.Fatalf("get archived Stage: %v", err)
	}
	if archived.ID != firstStages[1].ID || archived.ArchivedAt == nil ||
		!archived.ArchivedAt.Equal(secondObservedAt) {
		t.Errorf("archived Stage = %#v", archived)
	}

	staleSnapshot := promotionTestSnapshot(uuid.New(), "prod", "colo", observedAt.Add(30*time.Second))
	if err := database.ReplacePromotionStageSnapshot(ctx, staleSnapshot, firstStages); !errors.Is(err, promotion.ErrStaleSnapshot) {
		t.Fatalf("stale snapshot error = %v", err)
	}

	edgeObservedAt := observedAt.Add(2 * time.Minute)
	edgeSnapshot := promotionTestSnapshot(uuid.New(), "edge", "edge-cluster", edgeObservedAt)
	edgeStage := promotionTestStage("edge", "edge-cluster", "kargo-core", "external-dns-prod", edgeObservedAt)
	if err := database.ReplacePromotionStageSnapshot(
		ctx,
		edgeSnapshot,
		[]promotion.Stage{edgeStage},
	); err != nil {
		t.Fatalf("multi-cluster ReplacePromotionStageSnapshot() error = %v", err)
	}
	edge, err := database.GetPromotionStageByIdentity(ctx, "edge", "kargo-core", "external-dns-prod")
	if err != nil || edge.ClusterName != "edge-cluster" || edge.ID != edgeStage.ID {
		t.Errorf("edge Stage = %#v, %v", edge, err)
	}
}

func TestPromotionStageReviewProjectionFiltersAndPaginatesIntegration(t *testing.T) {
	createdAt := time.Date(2026, time.October, 23, 12, 0, 0, 0, time.UTC)
	fixture := newPromotionIntegrationFixture(t, createdAt)
	if _, err := fixture.database.AcceptPromotionRequest(
		fixture.ctx, fixture.acceptance(t, fixture.request("automatic"), createdAt.Add(2*time.Minute)),
	); err != nil {
		t.Fatal(err)
	}
	preparePromotionForReview(t, fixture, "pending", createdAt.Add(3*time.Minute))
	if _, err := fixture.database.AcceptPromotionRequest(
		fixture.ctx, fixture.acceptance(t, fixture.request("preparing"), createdAt.Add(3*time.Minute+time.Second)),
	); err != nil {
		t.Fatal(err)
	}

	base := fixture.stage
	metrics := promotionTestStage("prod", "colo", "kargo-core", "metrics-prod", createdAt)
	platform := promotionTestStage("prod", "colo", "kargo-platform", "external-dns-prod", createdAt)
	archived := promotionTestStage("prod", "colo", "kargo-core", "archived-prod", createdAt)
	edge := promotionTestStage("edge", "edge-cluster", "kargo-core", "external-dns-prod", createdAt)
	replaceSnapshot := func(stages []promotion.Stage, observedAt time.Time) {
		t.Helper()
		for index := range stages {
			stages[index].FirstObservedAt = observedAt
			stages[index].LastObservedAt = observedAt
		}
		if err := fixture.database.ReplacePromotionStageSnapshot(
			fixture.ctx,
			promotionTestSnapshot(uuid.New(), stages[0].Integration, stages[0].ClusterName, observedAt),
			stages,
		); err != nil {
			t.Fatal(err)
		}
	}
	replaceSnapshot([]promotion.Stage{metrics, archived, base, platform}, createdAt.Add(4*time.Minute))
	replaceSnapshot([]promotion.Stage{platform, metrics, base}, createdAt.Add(5*time.Minute))
	replaceSnapshot([]promotion.Stage{edge}, createdAt.Add(5*time.Minute))

	for _, test := range []struct {
		name   string
		filter promotion.StageListFilter
		total  int
		ids    []uuid.UUID
	}{
		{
			name:   "ordered page includes archived by default",
			filter: promotion.StageListFilter{Limit: 2, Offset: 1}, total: 5,
			ids: []uuid.UUID{archived.ID, base.ID},
		},
		{
			name: "active inventory", filter: promotion.StageListFilter{Archived: new(false), Limit: 10}, total: 4,
			ids: []uuid.UUID{edge.ID, base.ID, metrics.ID, platform.ID},
		},
		{
			name: "archived inventory", filter: promotion.StageListFilter{Archived: new(true), Limit: 10}, total: 1,
			ids: []uuid.UUID{archived.ID},
		},
		{
			name: "combined filters", total: 1, ids: []uuid.UUID{base.ID},
			filter: promotion.StageListFilter{
				Integration: "prod", Cluster: "colo", Project: "kargo-core", Application: "external-dns",
				Archived: new(false), Limit: 10,
			},
		},
		{
			name:   "mismatched integration and cluster",
			filter: promotion.StageListFilter{Integration: "prod", Cluster: "edge-cluster", Limit: 10},
		},
		{name: "empty page retains total", filter: promotion.StageListFilter{Limit: 2, Offset: 1 << 31}, total: 5},
	} {
		t.Run(test.name, func(t *testing.T) {
			page, err := fixture.database.ListPromotionStageReviews(fixture.ctx, test.filter)
			if err != nil {
				t.Fatal(err)
			}
			ids := make([]uuid.UUID, 0, len(page.Items))
			for _, item := range page.Items {
				ids = append(ids, item.Stage.ID)
				if item.Stage.ID == base.ID {
					if item.PendingPromotionCount != 1 || item.Policy == nil ||
						item.Policy.Mode != promotion.PolicyModeReviewRequired ||
						item.Policy.UpdatedBySnapshot != fixture.actor {
						t.Errorf("joined review policy and pending count = %#v", item)
					}
				} else if item.PendingPromotionCount != 0 || item.Policy != nil {
					t.Errorf("Stage without reviews or policy = %#v", item)
				}
			}
			if page.Total != test.total || !slices.Equal(ids, test.ids) {
				t.Errorf("Stage page = %v, total %d; want %v, total %d", ids, page.Total, test.ids, test.total)
			}
		})
	}
}

func createPromotionArtifact(
	t *testing.T,
	database *Database,
	runID uuid.UUID,
	name string,
	mediaType string,
	createdAt time.Time,
) uuid.UUID {
	t.Helper()

	artifactID := uuid.New()
	objectKey := "runs/" + runID.String() + "/artifacts/" + name
	record := artifact.Artifact{
		ID:             artifactID,
		RunID:          runID,
		Name:           name,
		Kind:           artifact.KindOutput,
		State:          artifact.StateSealed,
		MediaType:      mediaType,
		SizeBytes:      0,
		SHA256:         new(strings.Repeat("a", 64)),
		Sensitive:      true,
		ObjectKey:      &objectKey,
		RetentionState: artifact.RetentionRetained,
		CreatedAt:      createdAt,
		SealedAt:       new(createdAt),
	}
	if _, err := database.CreateArtifact(t.Context(), record); err != nil {
		t.Fatalf("create Promotion artifact %q: %v", name, err)
	}

	return artifactID
}

func preparePromotionForReview(
	t *testing.T,
	fixture promotionIntegrationFixture,
	externalID string,
	acceptedAt time.Time,
) promotion.Promotion {
	t.Helper()
	preparation := preparePromotionForCompletion(t, fixture, externalID, acceptedAt)

	record, err := fixture.database.CompletePromotionPreparation(
		fixture.ctx,
		preparation.record.ID,
		preparation.runID,
		preparation.gitArtifactID,
		preparation.argoArtifactID,
		preparation.completedAt,
		nil,
	)
	if err != nil {
		t.Fatalf("complete Promotion preparation: %v", err)
	}

	return record
}

type promotionPreparationCompletion struct {
	record         promotion.Promotion
	runID          uuid.UUID
	gitArtifactID  uuid.UUID
	argoArtifactID uuid.UUID
	completedAt    time.Time
}

func preparePromotionForCompletion(
	t *testing.T,
	fixture promotionIntegrationFixture,
	externalID string,
	acceptedAt time.Time,
) promotionPreparationCompletion {
	t.Helper()

	policy := promotion.StagePolicy{
		StageID:              fixture.stage.ID,
		Mode:                 promotion.PolicyModeReviewRequired,
		UpdatedByPrincipalID: fixture.principal.ID,
		UpdatedBySnapshot:    fixture.actor,
		UpdatedAt:            acceptedAt.Add(-time.Second),
	}
	if _, err := fixture.database.UpsertPromotionStagePolicy(fixture.ctx, policy); err != nil {
		t.Fatalf("create review policy: %v", err)
	}
	record, err := fixture.database.AcceptPromotionRequest(
		fixture.ctx,
		fixture.acceptance(t, fixture.request(externalID), acceptedAt),
	)
	if err != nil {
		t.Fatalf("accept guarded Promotion: %v", err)
	}

	preparationRun, err := fixture.database.GetRun(fixture.ctx, fixture.runID)
	if err != nil {
		t.Fatalf("get preparation Run template: %v", err)
	}
	preparationRun.ID = uuid.New()
	preparationRun.Source = run.Source{
		Type:      run.SourceTypePromotion,
		Promotion: &run.PromotionRunSource{PromotionID: record.ID},
	}
	idempotencyKey := record.ID.String()
	preparationRun.IdempotencyKey = &idempotencyKey
	preparationRun.RequestDigest = strings.Repeat("c", 64)
	preparationRun.CreatedBy = &fixture.actor
	preparationRun.CreatedAt = acceptedAt.Add(time.Second)
	if _, err := fixture.database.ReservePromotionPreparationRevision(
		fixture.ctx,
		record.ID,
		preparationRun.RepositoryID,
		preparationRun.CatalogRevisionID,
		preparationRun.CreatedAt,
	); err != nil {
		t.Fatalf("reserve preparation revision: %v", err)
	}
	preparationRun, err = fixture.database.CreateRun(fixture.ctx, preparationRun, nil)
	if err != nil {
		t.Fatalf("create preparation Run: %v", err)
	}
	gitArtifactID := createPromotionArtifact(
		t,
		fixture.database,
		preparationRun.ID,
		promotion.GitDiffArtifactName,
		promotion.GitDiffArtifactMediaType,
		acceptedAt.Add(4*time.Second),
	)
	argoArtifactID := createPromotionArtifact(
		t,
		fixture.database,
		preparationRun.ID,
		promotion.ArgoDiffArtifactName,
		argodiff.MediaType,
		acceptedAt.Add(4*time.Second),
	)
	return promotionPreparationCompletion{
		record:         record,
		runID:          preparationRun.ID,
		gitArtifactID:  gitArtifactID,
		argoArtifactID: argoArtifactID,
		completedAt:    acceptedAt.Add(5 * time.Second),
	}
}

type promotionIntegrationFixture struct {
	ctx          context.Context
	config       Config
	database     *Database
	runID        uuid.UUID
	repositoryID uuid.UUID
	principal    productidentity.Principal
	actor        productidentity.ActorSnapshot
	stage        promotion.Stage
}

func newPromotionIntegrationFixture(t *testing.T, createdAt time.Time) promotionIntegrationFixture {
	t.Helper()

	ctx, cfg, database := openIntegrationDatabase(t)
	t.Cleanup(database.Close)

	fixture := promotionIntegrationFixture{
		ctx:          ctx,
		config:       cfg,
		database:     database,
		runID:        createArtifactTestRun(t, ctx, database, createdAt),
		repositoryID: uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1400"),
		principal: productidentity.Principal{
			ID:          uuid.New(),
			Kind:        productidentity.KindServiceAccount,
			DisplayName: "Kargo production",
			Role:        productidentity.RoleOperator,
			CreatedAt:   createdAt,
			UpdatedAt:   createdAt,
		},
		stage: promotionTestStage(
			"prod",
			"colo",
			"kargo-core",
			"external-dns-prod",
			createdAt.Add(time.Minute),
		),
	}

	if _, err := database.CreatePrincipal(ctx, fixture.principal); err != nil {
		t.Fatalf("create Kargo principal: %v", err)
	}
	actor, err := fixture.principal.ActorSnapshot()
	if err != nil {
		t.Fatalf("snapshot Kargo principal: %v", err)
	}
	fixture.actor = actor
	snapshot := promotion.StageSnapshot{
		SnapshotID:  uuid.New(),
		Integration: fixture.stage.Integration,
		ClusterName: fixture.stage.ClusterName,
		ObservedAt:  fixture.stage.LastObservedAt,
		AcceptedAt:  fixture.stage.LastObservedAt.Add(time.Second),
	}
	if err := database.ReplacePromotionStageSnapshot(
		ctx,
		snapshot,
		[]promotion.Stage{fixture.stage},
	); err != nil {
		t.Fatalf("create observed Stage: %v", err)
	}

	return fixture
}

func promotionTestStage(
	integration string,
	clusterName string,
	project string,
	name string,
	observedAt time.Time,
) promotion.Stage {
	return promotion.Stage{
		ID:              uuid.New(),
		Integration:     integration,
		ClusterName:     clusterName,
		Project:         project,
		Name:            name,
		KubernetesUID:   "uid-" + name,
		ResourceVersion: "1",
		Application:     strings.TrimSuffix(name, "-prod"),
		Environment:     "prod",
		RepositoryName:  "artifact-test",
		RepositoryURL:   "https://github.com/example/artifact-test.git",
		TargetBranch:    "stage/" + name,
		SourcePaths:     []string{"k8s/" + name},
		Freight:         promotion.Freight{Items: []promotion.FreightItem{}},
		FirstObservedAt: observedAt,
		LastObservedAt:  observedAt,
	}
}

func promotionTestSnapshot(
	id uuid.UUID,
	integration string,
	clusterName string,
	observedAt time.Time,
) promotion.StageSnapshot {
	return promotion.StageSnapshot{
		SnapshotID:  id,
		Integration: integration,
		ClusterName: clusterName,
		ObservedAt:  observedAt,
		AcceptedAt:  observedAt.Add(time.Second),
	}
}

func (f promotionIntegrationFixture) request(externalID string) promotion.KargoRequest {
	return promotion.KargoRequest{
		ExternalPromotionID: externalID,
		Project:             f.stage.Project,
		Stage:               f.stage.Name,
		BaseRevision:        "8b7f1d217a6b0000000000000000000000000000",
		CandidateRevision:   "f4cb21a88e1d0000000000000000000000000000",
		CandidateBranch:     "kargo/promotion/" + externalID,
	}
}

func (f promotionIntegrationFixture) acceptance(
	t *testing.T,
	request promotion.KargoRequest,
	acceptedAt time.Time,
) promotion.PromotionAcceptance {
	t.Helper()

	digest, err := request.Digest()
	if err != nil {
		t.Fatalf("digest Promotion request: %v", err)
	}

	return promotion.PromotionAcceptance{
		ID:                         uuid.New(),
		Integration:                f.stage.Integration,
		Request:                    request,
		RequestDigest:              digest,
		AllowedRepositories:        []string{f.stage.RepositoryName},
		RequestedByPrincipalID:     f.principal.ID,
		RequestedBySnapshot:        f.actor,
		AcceptedAt:                 acceptedAt,
		MaximumStageObservationAge: 5 * time.Minute,
		PendingReviewRetention:     8 * 24 * time.Hour,
	}
}
