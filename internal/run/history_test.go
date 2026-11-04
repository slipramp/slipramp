package run

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestListRunsAppliesDefaultsAndFilters(t *testing.T) {
	t.Parallel()

	service, runs := testService(t)
	want := []Run{{ID: uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1400")}}
	runs.listed = want
	repositoryID := testRepositoryID
	inputArtifactID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1410")

	listed, err := service.ListRuns(
		context.Background(),
		RunListFilter{
			RepositoryID:    &repositoryID,
			TargetID:        &testTargetID,
			ActionName:      "sync",
			SourceType:      SourceTypeSchedule,
			TriggerName:     "nightly-sync",
			CommitSHA:       testCommitSHA,
			InputArtifactID: &inputArtifactID,
			ClusterName:     "colo",
			Phase:           PhaseRunning,
			ReviewState:     ReviewStatePending,
		},
	)
	if err != nil {
		t.Fatalf("ListRuns() error = %v", err)
	}
	if len(listed.Items) != 1 || listed.Items[0].ID != want[0].ID || listed.Total != 1 {
		t.Errorf("ListRuns() = %#v, want %#v", listed, want)
	}
	if runs.listFilter.RepositoryID == nil ||
		*runs.listFilter.RepositoryID != repositoryID ||
		runs.listFilter.CommitSHA != testCommitSHA ||
		runs.listFilter.InputArtifactID == nil ||
		*runs.listFilter.InputArtifactID != inputArtifactID ||
		runs.listFilter.TargetID == nil ||
		*runs.listFilter.TargetID != testTargetID ||
		runs.listFilter.ActionName != "sync" ||
		runs.listFilter.SourceType != SourceTypeSchedule ||
		runs.listFilter.TriggerName != "nightly-sync" ||
		runs.listFilter.ClusterName != "colo" ||
		runs.listFilter.Phase != PhaseRunning ||
		runs.listFilter.ReviewState != ReviewStatePending ||
		runs.listFilter.Limit != DefaultRunListLimit {
		t.Errorf("ListRuns() filter = %#v", runs.listFilter)
	}
}

func TestListRunsRejectsInvalidSource(t *testing.T) {
	t.Parallel()

	service, _ := testService(t)
	_, err := service.ListRuns(context.Background(), RunListFilter{SourceType: "webhook"})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("ListRuns() error = %v, want invalid request", err)
	}
}

func TestListRunsRejectsTriggerWithoutNamedAutomaticSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sourceType SourceType
	}{
		{name: "missing source"},
		{name: "manual source", sourceType: SourceTypeManual},
		{name: "API source", sourceType: SourceTypeAPI},
		{name: "MCP source", sourceType: SourceTypeMCP},
		{name: "Promotion source", sourceType: SourceTypePromotion},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			service, _ := testService(t)
			_, err := service.ListRuns(context.Background(), RunListFilter{
				SourceType:  test.sourceType,
				TriggerName: "nightly-sync",
			})
			if !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("ListRuns() error = %v, want invalid request", err)
			}
		})
	}
}

func TestListRunsAcceptsTriggerWithNamedAutomaticSource(t *testing.T) {
	t.Parallel()

	for _, sourceType := range []SourceType{SourceTypeGit, SourceTypeSchedule} {
		service, _ := testService(t)
		_, err := service.ListRuns(context.Background(), RunListFilter{
			SourceType:  sourceType,
			TriggerName: "nightly-sync",
		})
		if err != nil {
			t.Fatalf("ListRuns() source %q error = %v", sourceType, err)
		}
	}
}

func TestListRunsRejectsInvalidReviewState(t *testing.T) {
	t.Parallel()

	service, _ := testService(t)
	_, err := service.ListRuns(context.Background(), RunListFilter{ReviewState: "waiting"})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("ListRuns() error = %v, want invalid request", err)
	}
}

func TestListRunsRejectsEmptyInputArtifactID(t *testing.T) {
	t.Parallel()

	service, _ := testService(t)
	emptyID := uuid.Nil
	_, err := service.ListRuns(context.Background(), RunListFilter{InputArtifactID: &emptyID})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("ListRuns() error = %v, want invalid request", err)
	}
}

func TestListRunsRejectsInvalidPhase(t *testing.T) {
	t.Parallel()

	service, _ := testService(t)
	_, err := service.ListRuns(context.Background(), RunListFilter{Phase: "waiting"})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("ListRuns() error = %v, want invalid request", err)
	}
}

func TestListRunsRejectsInvalidLimit(t *testing.T) {
	t.Parallel()

	service, _ := testService(t)
	_, err := service.ListRuns(context.Background(), RunListFilter{Limit: MaximumRunListLimit + 1})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("ListRuns() error = %v, want invalid request", err)
	}
}

func TestListRunsRejectsNegativeOffset(t *testing.T) {
	t.Parallel()

	service, _ := testService(t)
	_, err := service.ListRuns(context.Background(), RunListFilter{Offset: -1})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("ListRuns() error = %v, want invalid request", err)
	}
}

func TestListRunsRejectsEmptyTargetID(t *testing.T) {
	t.Parallel()

	service, _ := testService(t)
	emptyID := uuid.Nil
	_, err := service.ListRuns(context.Background(), RunListFilter{TargetID: &emptyID})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("ListRuns() error = %v, want invalid request", err)
	}
}

func TestListRunsRejectsCommitWithoutRepository(t *testing.T) {
	t.Parallel()

	service, _ := testService(t)
	_, err := service.ListRuns(context.Background(), RunListFilter{CommitSHA: testCommitSHA})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("ListRuns() error = %v, want invalid request", err)
	}
}

func TestListRunsRejectsInvalidCommit(t *testing.T) {
	t.Parallel()

	service, _ := testService(t)
	repositoryID := testRepositoryID
	_, err := service.ListRuns(context.Background(), RunListFilter{
		RepositoryID: &repositoryID,
		CommitSHA:    "0123456",
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("ListRuns() error = %v, want invalid request", err)
	}
}
