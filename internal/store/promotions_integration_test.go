package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/artifact"
	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/promotion"
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
	var stageSnapshot promotion.PromotionStageSnapshot
	if err := json.Unmarshal(guarded.StageSnapshot, &stageSnapshot); err != nil ||
		stageSnapshot.ResourceVersion != fixture.stage.ResourceVersion ||
		stageSnapshot.RepositoryName != fixture.stage.RepositoryName {
		t.Errorf("Stage snapshot = %#v, %v", stageSnapshot, err)
	}
	var policySnapshot promotion.PromotionPolicySnapshot
	if err := json.Unmarshal(guarded.PolicySnapshot, &policySnapshot); err != nil ||
		policySnapshot.ConfiguredMode != promotion.PolicyModeReviewRequired ||
		policySnapshot.EffectiveMode != promotion.PolicyModeReviewRequired {
		t.Errorf("policy snapshot = %#v, %v", policySnapshot, err)
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
	preparationRun, err = database.CreateRun(ctx, preparationRun)
	if err != nil {
		t.Fatalf("create preparation Run: %v", err)
	}
	runID := preparationRun.ID
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
	promotionRecord, err = database.AttachPromotionPreparationRun(
		ctx,
		promotionRecord.ID,
		runID,
		createdAt.Add(4*time.Minute+2*time.Second),
	)
	if err != nil {
		t.Fatalf("attach preparation Run: %v", err)
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
	preparationRun, err = database.CreateRun(ctx, preparationRun)
	if err != nil {
		t.Fatalf("create second preparation Run: %v", err)
	}
	preparing, err = database.ReservePromotionPreparationRevision(
		ctx,
		preparing.ID,
		preparationRun.RepositoryID,
		preparationRun.CatalogRevisionID,
		createdAt.Add(8*time.Minute+time.Second),
	)
	if err != nil {
		t.Fatalf("reserve second preparation revision: %v", err)
	}
	if _, err := database.AttachPromotionPreparationRun(
		ctx,
		preparing.ID,
		preparationRun.ID,
		createdAt.Add(8*time.Minute+2*time.Second),
	); err != nil {
		t.Fatalf("AttachPromotionPreparationRun() error = %v", err)
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
	if _, err := database.AttachPromotionPreparationRun(
		ctx,
		preparing.ID,
		preparationRun.ID,
		createdAt.Add(8*time.Minute+4*time.Second),
	); !errors.Is(err, promotion.ErrTransitionConflict) {
		t.Errorf("attach after failure error = %v", err)
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

type promotionIntegrationFixture struct {
	ctx          context.Context
	database     *Database
	runID        uuid.UUID
	repositoryID uuid.UUID
	principal    productidentity.Principal
	actor        productidentity.ActorSnapshot
	stage        promotion.Stage
}

func newPromotionIntegrationFixture(t *testing.T, createdAt time.Time) promotionIntegrationFixture {
	t.Helper()

	ctx, _, database := openIntegrationDatabase(t)
	t.Cleanup(database.Close)

	fixture := promotionIntegrationFixture{
		ctx:          ctx,
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
		Freight:         json.RawMessage(`{"items":[]}`),
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
