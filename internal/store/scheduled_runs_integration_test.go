package store

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/repository"
	"github.com/slipramp/slipramp/internal/run"
)

func TestScheduledRunOccurrencePersistenceAndOverlapIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1680")
	createdAt := time.Date(2026, time.November, 3, 2, 0, 0, 0, time.UTC)
	_, err := database.CreateRepository(ctx, repository.Repository{
		ID:             repositoryID,
		Name:           "scheduled-actions",
		CloneURL:       "https://github.com/example/scheduled-actions.git",
		Branch:         "main",
		CommitIndexing: repository.CommitIndexingObservedHeads,
		CreatedAt:      createdAt,
	})
	if err != nil {
		t.Fatalf("CreateRepository() error = %v", err)
	}

	revisionID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1681")
	refresh := successfulCatalogRefresh(
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		createdAt.Add(time.Minute),
		revisionID,
	)
	refresh.Revision.Catalog = scheduledRunCatalog()
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, refresh); err != nil {
		t.Fatalf("record catalog refresh: %v", err)
	}

	targetID := activeTargetID(t, database, repositoryID, "network")
	runs, _ := newGitTriggerTestServices(t, database)
	if _, err := runs.CreateRun(ctx, run.CreateRequest{
		TargetID:   targetID,
		ActionName: "sync",
		Parameters: gitTriggerParameters("production"),
		Source:     run.Source{Type: run.SourceTypeAPI},
	}); err != nil {
		t.Fatalf("create queued API Run: %v", err)
	}
	firstScheduledAt := time.Date(2026, time.November, 3, 3, 0, 0, 0, time.UTC)
	first := scheduledCreateRequest(targetID, firstScheduledAt)
	created, err := runs.CreateRun(ctx, first)
	if err != nil {
		t.Fatalf("create first scheduled Run: %v", err)
	}
	if created.CatalogRevisionID != revisionID || created.CreatedBy != nil ||
		created.Source.Schedule == nil ||
		!created.Source.Schedule.ScheduledAt.Equal(firstScheduledAt) {
		t.Errorf("created scheduled Run = %#v", created)
	}

	duplicate, err := runs.CreateRun(ctx, first)
	if err != nil {
		t.Fatalf("create duplicate occurrence: %v", err)
	}
	if duplicate.ID != created.ID {
		t.Errorf("duplicate occurrence Run = %s, want %s", duplicate.ID, created.ID)
	}

	secondScheduledAt := firstScheduledAt.Add(24 * time.Hour)
	second := scheduledCreateRequest(targetID, secondScheduledAt)
	if _, err := runs.CreateRun(ctx, second); !errors.Is(err, run.ErrScheduleOverlap) {
		t.Errorf("overlapping scheduled Run error = %v", err)
	}

	if _, err := database.pool.Exec(
		ctx,
		"UPDATE runs SET phase = 'finished', outcome = 'succeeded', finished_at = $2 WHERE id = $1",
		created.ID,
		secondScheduledAt.Add(-time.Minute),
	); err != nil {
		t.Fatalf("finish first scheduled Run: %v", err)
	}
	createdSecond, err := runs.CreateRun(ctx, second)
	if err != nil {
		t.Fatalf("create next scheduled Run: %v", err)
	}
	if createdSecond.ID == created.ID || createdSecond.Source.Schedule == nil ||
		!createdSecond.Source.Schedule.ScheduledAt.Equal(secondScheduledAt) {
		t.Errorf("next scheduled Run = %#v", createdSecond)
	}

	persisted, err := database.GetRun(ctx, createdSecond.ID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if persisted.Source.Type != run.SourceTypeSchedule || persisted.Source.Schedule == nil ||
		persisted.Source.Schedule.TriggerName != "nightly-preview" ||
		!persisted.Source.Schedule.ScheduledAt.Equal(secondScheduledAt) {
		t.Errorf("persisted scheduled source = %#v", persisted.Source)
	}
}

func scheduledRunCatalog() catalog.Catalog {
	result := gitTriggeredCatalog()
	action := result.Targets[0].Spec.Actions["sync"]
	action.Triggers = []catalog.Trigger{{
		Name:       "nightly-preview",
		Schedule:   &catalog.ScheduleTrigger{Cron: "0 3 * * *"},
		Parameters: gitTriggerCatalogParameters("production"),
	}}
	result.Targets[0].Spec.Actions["sync"] = action

	return result
}

func scheduledCreateRequest(targetID uuid.UUID, scheduledAt time.Time) run.CreateRequest {
	idempotencyKey := run.ScheduleOccurrenceIdempotencyKey(
		targetID,
		"sync",
		"nightly-preview",
		scheduledAt,
	)

	return run.CreateRequest{
		TargetID:   targetID,
		ActionName: "sync",
		Parameters: gitTriggerParameters("production"),
		Inputs:     map[string]uuid.UUID{},
		Source: run.Source{
			Type: run.SourceTypeSchedule,
			Schedule: &run.ScheduleRunSource{
				TriggerName: "nightly-preview",
				ScheduledAt: scheduledAt,
			},
		},
		IdempotencyKey: &idempotencyKey,
	}
}
