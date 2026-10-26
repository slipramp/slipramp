package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/gittrigger"
	"github.com/slipramp/slipramp/internal/repository"
	"github.com/slipramp/slipramp/internal/run"
)

func seedGitDelivery(t *testing.T, database *Database) (gittrigger.Delivery, time.Time) {
	t.Helper()

	ctx := t.Context()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	repositoryID := uuid.New()
	_, err := database.CreateRepository(ctx, repository.Repository{
		ID: repositoryID, Name: "retry-test", CloneURL: "https://example.com/infra.git", Branch: "main",
		CommitIndexing: repository.CommitIndexingObservedHeads, CreatedAt: base,
	})
	if err != nil {
		t.Fatal(err)
	}
	for index, sha := range []string{strings.Repeat("a", 40), strings.Repeat("b", 40)} {
		refresh := successfulCatalogRefresh(sha, base.Add(time.Duration(index)*time.Minute), uuid.New())
		refresh.Revision.Catalog = gitTriggeredCatalog()
		refresh.ChangedPaths = []string{"infrastructure/network/main.tf"}
		if err := database.RecordRepositoryRefresh(ctx, repositoryID, refresh); err != nil {
			t.Fatal(err)
		}
	}
	delivery, err := database.GetEligibleGitTriggerDelivery(ctx, base.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return delivery, base.Add(time.Minute)
}

func TestGitDeliveryBackoffPersistsAndTerminatesIntegration(t *testing.T) {
	ctx, cfg, database := openIntegrationDatabase(t)
	delivery, attemptedAt := seedGitDelivery(t, database)
	failure := errors.New("postgres://admin:secret@database private parameter")

	state := recordGitDeliveryFailure(t, database, delivery.ID, attemptedAt, failure)
	if state.Count != 1 || state.Terminal {
		t.Fatalf("first failure state = %#v", state)
	}
	nextAttemptAt := attemptedAt.Add(gittrigger.InitialRetryDelay)
	database.Close()

	// A new Server connection sees the durable schedule and does not select the delivery early.
	database, err := Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.GetEligibleGitTriggerDelivery(ctx, nextAttemptAt.Add(-time.Nanosecond)); !errors.Is(err, gittrigger.ErrNoEligibleDelivery) {
		t.Fatalf("selected before due: %v", err)
	}

	for count := 2; count <= gittrigger.FailureLimit; count++ {
		attemptedAt = nextAttemptAt
		selected, err := database.GetEligibleGitTriggerDelivery(ctx, attemptedAt)
		if err != nil || selected.ID != delivery.ID {
			t.Fatalf("due selection = %#v, %v", selected, err)
		}
		state = recordGitDeliveryFailure(t, database, delivery.ID, attemptedAt, failure)
		if state.Count != count {
			t.Fatalf("failure %d state = %#v", count, state)
		}
		delay := min(
			gittrigger.InitialRetryDelay*time.Duration(1<<(count-1)),
			gittrigger.MaximumRetryDelay,
		)
		nextAttemptAt = attemptedAt.Add(delay)
	}
	if !state.Terminal {
		t.Fatal("failure limit did not make the delivery terminal")
	}
	if _, err := database.GetEligibleGitTriggerDelivery(ctx, attemptedAt.Add(24*time.Hour)); !errors.Is(err, gittrigger.ErrNoEligibleDelivery) {
		t.Fatalf("terminal failure eligible: %v", err)
	}

	page, err := database.ListFailedGitTriggerDeliveries(ctx, 10, 0)
	if err != nil || page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("failed page = %#v, %v", page, err)
	}
	if page.Items[0].Diagnostic != gittrigger.FailureDiagnostic(failure) {
		t.Fatalf("diagnostic = %q", page.Items[0].Diagnostic)
	}

	refresh := successfulCatalogRefresh(strings.Repeat("c", 40), attemptedAt.Add(time.Minute), uuid.New())
	refresh.Revision.Catalog = gitTriggeredCatalog()
	refresh.ChangedPaths = []string{"infrastructure/network/main.tf"}
	if err := database.RecordRepositoryRefresh(ctx, delivery.RepositoryID, refresh); err != nil {
		t.Fatal(err)
	}
	page, err = database.ListFailedGitTriggerDeliveries(ctx, 10, 0)
	if err != nil || page.Total != 1 {
		t.Fatalf("terminal history after refresh = %#v, %v", page, err)
	}
	_, dispatcher := newGitTriggerTestServices(t, database)
	if processed, err := dispatcher.ProcessNext(ctx); err != nil || !processed {
		t.Fatalf("later delivery = %t, %v", processed, err)
	}
}

func TestGitDeliveryBackoffIsSupersededByNewHeadIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	delivery, attemptedAt := seedGitDelivery(t, database)
	recordGitDeliveryFailure(t, database, delivery.ID, attemptedAt, errors.New("failure"))
	refresh := successfulCatalogRefresh(strings.Repeat("c", 40), attemptedAt.Add(time.Second), uuid.New())
	refresh.Revision.Catalog = gitTriggeredCatalog()
	refresh.ChangedPaths = []string{"infrastructure/network/main.tf"}
	if err := database.RecordRepositoryRefresh(ctx, delivery.RepositoryID, refresh); err != nil {
		t.Fatal(err)
	}
	assertGitTriggerDeliveryState(t, database, delivery.ID, true, false)

	_, recorded, err := database.RecordGitTriggerDeliveryFailure(
		ctx,
		delivery.ID,
		attemptedAt.Add(time.Second),
		gittrigger.FailureDiagnostic(errors.New("late failure")),
	)
	if err != nil || recorded {
		t.Fatalf("failure after supersession: recorded=%t error=%v", recorded, err)
	}
}

func TestGitRunAndDeliveryLinkCommitAtomicallyIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	delivery, attemptedAt := seedGitDelivery(t, database)
	record := queuedGitTestRun(uuid.New(), delivery, strings.Repeat("b", 40), attemptedAt.Add(time.Second))
	_, err := database.pool.Exec(ctx, `CREATE FUNCTION reject_delivery_link() RETURNS trigger LANGUAGE plpgsql AS
        $$ BEGIN RAISE EXCEPTION 'simulated link storage failure'; END; $$;
        CREATE TRIGGER reject_delivery_link BEFORE UPDATE OF run_id ON git_trigger_deliveries
        FOR EACH ROW EXECUTE FUNCTION reject_delivery_link();`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateRun(ctx, record, nil); err == nil {
		t.Fatal("expected link storage failure")
	}
	assertRunCounts(t, database, 0, 0)
	assertGitTriggerDeliveryState(t, database, delivery.ID, false, false)

	if _, err := database.pool.Exec(
		ctx,
		"DROP TRIGGER reject_delivery_link ON git_trigger_deliveries; DROP FUNCTION reject_delivery_link()",
	); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateRun(ctx, record, nil); err != nil {
		t.Fatal(err)
	}
	assertRunCounts(t, database, 1, 1)
	assertGitTriggerDeliveryState(t, database, delivery.ID, false, true)
}

func TestGitRunLinkRejectsMismatchedDeliveryIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	delivery, attemptedAt := seedGitDelivery(t, database)
	record := queuedGitTestRun(uuid.New(), delivery, strings.Repeat("b", 40), attemptedAt.Add(time.Second))
	record.Source.Git.TriggerName = "other-trigger"
	if _, err := database.CreateRun(ctx, record, nil); err == nil {
		t.Fatal("mismatched delivery created a Run")
	}
	assertRunCounts(t, database, 0, 0)
	assertGitTriggerDeliveryState(t, database, delivery.ID, false, false)
}

func TestFailedGitDeliveryPaginationOrderIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	delivery, attemptedAt := seedGitDelivery(t, database)
	ids := []uuid.UUID{delivery.ID, uuid.New(), uuid.New()}
	for index, id := range ids {
		if index > 0 {
			_, err := database.pool.Exec(ctx, `INSERT INTO git_trigger_deliveries
				(id, target_id, repository_id, catalog_revision_id, target_name, action_name, trigger_name,
                 review_required, parameters, created_at, next_attempt_at)
				SELECT $1, target_id, repository_id, catalog_revision_id, target_name, action_name, trigger_name,
                       review_required, parameters, created_at, next_attempt_at
                FROM git_trigger_deliveries WHERE id = $2`, id, delivery.ID)
			if err != nil {
				t.Fatal(err)
			}
		}
		exhaustGitDelivery(t, database, id, attemptedAt.Add(time.Duration(index)*time.Hour))
	}
	for offset := range ids {
		page, err := database.ListFailedGitTriggerDeliveries(ctx, 1, offset)
		if err != nil || page.Total != 3 || len(page.Items) != 1 || page.Items[0].ID != ids[2-offset] {
			t.Fatalf("offset=%d page=%#v error=%v", offset, page, err)
		}
	}
}

func recordGitDeliveryFailure(
	t *testing.T,
	database *Database,
	deliveryID uuid.UUID,
	attemptedAt time.Time,
	failure error,
) gittrigger.FailureState {
	t.Helper()

	state, recorded, err := database.RecordGitTriggerDeliveryFailure(
		t.Context(),
		deliveryID,
		attemptedAt,
		gittrigger.FailureDiagnostic(failure),
	)
	if err != nil || !recorded {
		t.Fatalf("record delivery failure: state=%#v recorded=%t error=%v", state, recorded, err)
	}
	return state
}

func exhaustGitDelivery(t *testing.T, database *Database, deliveryID uuid.UUID, attemptedAt time.Time) {
	t.Helper()

	for range gittrigger.FailureLimit {
		recordGitDeliveryFailure(t, database, deliveryID, attemptedAt, run.ErrPlacementNotFound)
	}
}
