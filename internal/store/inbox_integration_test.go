package store

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/run"
)

func TestInboxAcknowledgementsIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	now := time.Date(2026, time.October, 23, 12, 0, 0, 0, time.UTC)
	firstPrincipal := createInboxTestPrincipal(t, database, "First operator", now)
	secondPrincipal := createInboxTestPrincipal(t, database, "Second operator", now)
	firstProblemID := strings.Repeat("a", 64)
	secondProblemID := strings.Repeat("b", 64)

	if err := database.InsertInboxProblemAcknowledgements(
		ctx,
		firstPrincipal.ID,
		[]string{secondProblemID, firstProblemID, firstProblemID},
		now,
	); err != nil {
		t.Fatalf("insert first principal acknowledgements: %v", err)
	}
	if err := database.InsertInboxProblemAcknowledgements(
		ctx,
		secondPrincipal.ID,
		[]string{firstProblemID},
		now,
	); err != nil {
		t.Fatalf("insert second principal acknowledgement: %v", err)
	}

	first, err := database.ListInboxProblemAcknowledgements(ctx, firstPrincipal.ID)
	if err != nil {
		t.Fatalf("list first principal acknowledgements: %v", err)
	}
	second, err := database.ListInboxProblemAcknowledgements(ctx, secondPrincipal.ID)
	if err != nil {
		t.Fatalf("list second principal acknowledgements: %v", err)
	}
	if !slices.Equal(first, []string{firstProblemID, secondProblemID}) ||
		!slices.Equal(second, []string{firstProblemID}) {
		t.Errorf("acknowledgements = first %v, second %v", first, second)
	}

	if err := database.InsertInboxProblemAcknowledgements(
		ctx,
		firstPrincipal.ID,
		[]string{strings.Repeat("c", 64), strings.Repeat("z", 64)},
		now,
	); err == nil {
		t.Fatal("invalid acknowledgement batch error = nil")
	}
	first, err = database.ListInboxProblemAcknowledgements(ctx, firstPrincipal.ID)
	if err != nil {
		t.Fatalf("list acknowledgements after invalid batch: %v", err)
	}
	if len(first) != 2 {
		t.Errorf("invalid batch partially persisted: %v", first)
	}
}

func TestInboxActiveRunReadIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	createdAt := time.Date(2026, time.October, 23, 13, 0, 0, 0, time.UTC)
	runID := createArtifactTestRun(t, ctx, database, createdAt)
	observedAt := createdAt.Add(time.Minute)
	if _, err := database.pool.Exec(
		ctx,
		`UPDATE runs SET phase = 'running', assigned_at = $2, last_observed_at = $2 WHERE id = $1`,
		runID,
		observedAt,
	); err != nil {
		t.Fatalf("set Run active: %v", err)
	}

	active, err := database.ListInboxActiveRuns(ctx)
	if err != nil {
		t.Fatalf("ListInboxActiveRuns() error = %v", err)
	}
	if len(active) != 1 || active[0].ID != runID || active[0].Status.Phase != run.PhaseRunning ||
		active[0].Status.LastObservedAt == nil || !active[0].Status.LastObservedAt.Equal(observedAt) {
		t.Errorf("active Runs = %#v", active)
	}

	if _, err := database.pool.Exec(
		ctx,
		`UPDATE runs SET phase = 'finished', outcome = 'succeeded', finished_at = $2 WHERE id = $1`,
		runID,
		observedAt.Add(time.Minute),
	); err != nil {
		t.Fatalf("finish Run: %v", err)
	}
	active, err = database.ListInboxActiveRuns(ctx)
	if err != nil || len(active) != 0 {
		t.Errorf("active Runs after finish = %#v, error = %v", active, err)
	}
}

func TestInboxPromotionReadIntegration(t *testing.T) {
	createdAt := time.Date(2026, time.October, 23, 14, 0, 0, 0, time.UTC)
	fixture := newPromotionIntegrationFixture(t, createdAt)

	_, err := fixture.database.AcceptPromotionRequest(
		fixture.ctx,
		fixture.acceptance(t, fixture.request("approved-01jbe4q8"), createdAt.Add(2*time.Second)),
	)
	if err != nil {
		t.Fatalf("accept automatic Promotion: %v", err)
	}
	pending := preparePromotionForReview(t, fixture, "pending-01jbe4q8", createdAt.Add(10*time.Second))
	errored := preparePromotionForReview(t, fixture, "error-01jbe4q8", createdAt.Add(20*time.Second))
	if _, err := fixture.database.pool.Exec(
		fixture.ctx,
		`UPDATE promotions SET status = 'error', message = 'Preparation failed', updated_at = $2 WHERE id = $1`,
		errored.ID,
		createdAt.Add(30*time.Second),
	); err != nil {
		t.Fatalf("set Promotion error: %v", err)
	}

	relevant, err := fixture.database.ListInboxPromotions(fixture.ctx)
	if err != nil {
		t.Fatalf("ListInboxPromotions() error = %v", err)
	}
	if len(relevant) != 2 ||
		relevant[0].ID != errored.ID || relevant[0].Status != promotion.StatusError ||
		relevant[1].ID != pending.ID || relevant[1].Status != promotion.StatusPending ||
		relevant[1].StageName != fixture.stage.Name || relevant[1].Application != fixture.stage.Application {
		t.Errorf("relevant Promotions = %#v", relevant)
	}
}

func TestInboxFailedGitTriggerReadIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	delivery, attemptedAt := seedGitDelivery(t, database)
	exhaustGitDelivery(t, database, delivery.ID, attemptedAt)

	failures, err := database.ListInboxFailedGitTriggerDeliveries(ctx)
	if err != nil {
		t.Fatalf("ListInboxFailedGitTriggerDeliveries() error = %v", err)
	}
	if len(failures) != 1 || failures[0].ID != delivery.ID ||
		failures[0].RepositoryName != "retry-test" || failures[0].FailureCount != 8 ||
		failures[0].Diagnostic != "run placement not found" {
		t.Errorf("failed Git-trigger deliveries = %#v", failures)
	}
}

func createInboxTestPrincipal(
	t *testing.T,
	database *Database,
	displayName string,
	createdAt time.Time,
) productidentity.Principal {
	t.Helper()

	principal, err := database.CreatePrincipal(t.Context(), productidentity.Principal{
		ID:          uuid.New(),
		Kind:        productidentity.KindUser,
		DisplayName: displayName,
		Role:        productidentity.RoleOperator,
		CreatedAt:   createdAt,
		UpdatedAt:   createdAt,
	})
	if err != nil {
		t.Fatalf("create Inbox principal: %v", err)
	}

	return principal
}
