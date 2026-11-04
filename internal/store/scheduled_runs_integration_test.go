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
	apiRun, err := runs.CreateRun(ctx, run.CreateRequest{
		TargetID:   targetID,
		ActionName: "sync",
		Parameters: gitTriggerParameters("production"),
		Source:     run.Source{Type: run.SourceTypeAPI},
	})
	if err != nil {
		t.Fatalf("create queued API Run: %v", err)
	}
	firstScheduledAt := time.Date(2026, time.November, 3, 3, 0, 0, 0, time.UTC)
	first := scheduledCreateRequest(targetID, "sync", "nightly-preview", firstScheduledAt)
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
	second := scheduledCreateRequest(targetID, "sync", "nightly-preview", secondScheduledAt)
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

	weeklyScheduledAt := firstScheduledAt.Add((6 * 24 * time.Hour) + (6 * time.Hour))
	weekly, err := runs.CreateRun(
		ctx,
		scheduledCreateRequest(targetID, "sync", "weekly-preview", weeklyScheduledAt),
	)
	if err != nil {
		t.Fatalf("create weekly scheduled Run: %v", err)
	}
	previewScheduledAt := firstScheduledAt.Add(48 * time.Hour)
	preview, err := runs.CreateRun(
		ctx,
		scheduledCreateRequest(targetID, "preview", "nightly-preview", previewScheduledAt),
	)
	if err != nil {
		t.Fatalf("create same-named trigger for second Action: %v", err)
	}

	latestNightlySync, err := database.ListRuns(ctx, run.RunListFilter{
		TargetID:    &targetID,
		ActionName:  "sync",
		SourceType:  run.SourceTypeSchedule,
		TriggerName: "nightly-preview",
		Limit:       1,
	})
	if err != nil {
		t.Fatalf("list latest nightly sync Run: %v", err)
	}
	if len(latestNightlySync.Items) != 1 || latestNightlySync.Items[0].ID != createdSecond.ID ||
		latestNightlySync.Total != 2 {
		t.Errorf("latest nightly sync Runs = %#v, want second occurrence only", latestNightlySync)
	}

	weeklySync, err := database.ListRuns(ctx, run.RunListFilter{
		TargetID:    &targetID,
		ActionName:  "sync",
		SourceType:  run.SourceTypeSchedule,
		TriggerName: "weekly-preview",
		Limit:       1,
	})
	if err != nil {
		t.Fatalf("list weekly sync Run: %v", err)
	}
	if len(weeklySync.Items) != 1 || weeklySync.Items[0].ID != weekly.ID || weeklySync.Total != 1 {
		t.Errorf("weekly sync Runs = %#v, want weekly Run", weeklySync)
	}

	previewNightly, err := database.ListRuns(ctx, run.RunListFilter{
		TargetID:    &targetID,
		ActionName:  "preview",
		SourceType:  run.SourceTypeSchedule,
		TriggerName: "nightly-preview",
		Limit:       1,
	})
	if err != nil {
		t.Fatalf("list second Action nightly Run: %v", err)
	}
	if len(previewNightly.Items) != 1 || previewNightly.Items[0].ID != preview.ID ||
		previewNightly.Total != 1 {
		t.Errorf("preview nightly Runs = %#v, want preview Run", previewNightly)
	}

	apiSync, err := database.ListRuns(ctx, run.RunListFilter{
		TargetID:   &targetID,
		ActionName: "sync",
		SourceType: run.SourceTypeAPI,
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("list API sync Run: %v", err)
	}
	if len(apiSync.Items) != 1 || apiSync.Items[0].ID != apiRun.ID || apiSync.Total != 1 {
		t.Errorf("API sync Runs = %#v, want API Run", apiSync)
	}
}

func scheduledRunCatalog() catalog.Catalog {
	result := gitTriggeredCatalog()
	action := result.Targets[0].Spec.Actions["sync"]
	action.Triggers = []catalog.Trigger{
		{
			Name:       "nightly-preview",
			Schedule:   &catalog.ScheduleTrigger{Cron: "0 3 * * *"},
			Parameters: gitTriggerCatalogParameters("production"),
		},
		{
			Name:       "weekly-preview",
			Schedule:   &catalog.ScheduleTrigger{Cron: "0 9 * * 1"},
			Parameters: gitTriggerCatalogParameters("production"),
		},
	}
	result.Targets[0].Spec.Actions["sync"] = action
	preview := action
	preview.Command = []string{"./preview.sh"}
	preview.Title = "Preview network changes"
	preview.Triggers = action.Triggers[:1]
	result.Targets[0].Spec.Actions["preview"] = preview

	return result
}

func scheduledCreateRequest(
	targetID uuid.UUID,
	actionName string,
	triggerName string,
	scheduledAt time.Time,
) run.CreateRequest {
	idempotencyKey := run.ScheduleOccurrenceIdempotencyKey(
		targetID,
		actionName,
		triggerName,
		scheduledAt,
	)

	return run.CreateRequest{
		TargetID:   targetID,
		ActionName: actionName,
		Parameters: gitTriggerParameters("production"),
		Inputs:     map[string]uuid.UUID{},
		Source: run.Source{
			Type: run.SourceTypeSchedule,
			Schedule: &run.ScheduleRunSource{
				TriggerName: triggerName,
				ScheduledAt: scheduledAt,
			},
		},
		IdempotencyKey: &idempotencyKey,
	}
}
