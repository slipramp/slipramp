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

	listed, err := service.ListRuns(
		context.Background(),
		RunListFilter{
			RepositoryID: &repositoryID,
			CommitSHA:    testCommitSHA,
			TargetName:   "network",
			ClusterName:  "colo",
			Phase:        PhaseRunning,
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
		runs.listFilter.TargetName != "network" ||
		runs.listFilter.ClusterName != "colo" ||
		runs.listFilter.Phase != PhaseRunning ||
		runs.listFilter.Limit != DefaultRunListLimit {
		t.Errorf("ListRuns() filter = %#v", runs.listFilter)
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

func TestListRunsRejectsTargetWithoutRepository(t *testing.T) {
	t.Parallel()

	service, _ := testService(t)
	_, err := service.ListRuns(context.Background(), RunListFilter{TargetName: "network"})
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
