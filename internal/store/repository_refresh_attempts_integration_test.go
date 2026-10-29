package store

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/repository"
)

func TestRepositoryRefreshAttemptHistoryIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	repositoryID := uuid.New()
	createdAt := time.Date(2026, time.October, 28, 12, 0, 0, 0, time.UTC)
	_, err := database.CreateRepository(ctx, repository.Repository{
		ID:             repositoryID,
		Name:           "refresh-attempts",
		CloneURL:       "https://github.com/example/infrastructure.git",
		Branch:         "main",
		CommitIndexing: repository.CommitIndexingObservedHeads,
		CreatedAt:      createdAt,
	})
	if err != nil {
		t.Fatalf("CreateRepository() error = %v", err)
	}

	for index := range 48 {
		diagnostic := fmt.Sprintf("Git fetch failed safely %d", index)
		result := repository.RefreshResult{
			AttemptID:     uuid.New(),
			Source:        repository.RefreshSourcePeriodic,
			AttemptedAt:   createdAt.Add(time.Duration(index+1) * time.Minute),
			Duration:      time.Duration(index) * time.Millisecond,
			FailureReason: &diagnostic,
		}
		if err := database.RecordRepositoryRefresh(ctx, repositoryID, result); err != nil {
			t.Fatalf("record fetch failure %d: %v", index, err)
		}
	}

	commitSHA := strings.Repeat("a", 40)
	acceptedAt := createdAt.Add(49 * time.Minute)
	accepted := successfulCatalogRefresh(commitSHA, acceptedAt, uuid.New())
	accepted.Duration = 950 * time.Millisecond
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, accepted); err != nil {
		t.Fatalf("record accepted refresh: %v", err)
	}

	rejectionDiagnostic := ".slipramp/catalog.yaml:1:1: deployment placement is missing"
	rejectedAt := createdAt.Add(50 * time.Minute)
	rejected := repository.RefreshResult{
		AttemptID:     uuid.New(),
		Source:        repository.RefreshSourcePeriodic,
		Commit:        &repository.Commit{SHA: strings.Repeat("b", 40)},
		AttemptedAt:   rejectedAt,
		Duration:      1100 * time.Millisecond,
		FailureReason: &rejectionDiagnostic,
	}
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, rejected); err != nil {
		t.Fatalf("record rejected refresh: %v", err)
	}

	requestedDiagnostic := "Git fetch failed. Check repository access, branch, and refresh credentials."
	requestedAt := createdAt.Add(51 * time.Minute)
	requestedID := uuid.New()
	requested := repository.RefreshResult{
		AttemptID:     requestedID,
		Source:        repository.RefreshSourceRequested,
		AttemptedAt:   requestedAt,
		Duration:      1250 * time.Millisecond,
		FailureReason: &requestedDiagnostic,
	}
	if err := database.RecordRepositoryRefresh(ctx, repositoryID, requested); err != nil {
		t.Fatalf("record requested refresh: %v", err)
	}

	page, err := database.ListRepositoryRefreshAttempts(ctx, repositoryID, 10, 0)
	if err != nil {
		t.Fatalf("ListRepositoryRefreshAttempts() error = %v", err)
	}
	if page.Total != repository.RefreshAttemptRetentionLimit || len(page.Items) != 10 {
		t.Fatalf("refresh attempt page = %d of %d", len(page.Items), page.Total)
	}
	latest := page.Items[0]
	if latest.ID != requestedID || latest.Source != repository.RefreshSourceRequested ||
		latest.Outcome != repository.RefreshOutcomeFetchFailed || latest.CommitSHA != nil ||
		latest.Duration != 1250*time.Millisecond || latest.Diagnostic == nil ||
		*latest.Diagnostic != requestedDiagnostic {
		t.Errorf("latest requested attempt = %#v", latest)
	}
	rejection := page.Items[1]
	if rejection.Outcome != repository.RefreshOutcomeCatalogRejected || rejection.CommitSHA == nil ||
		*rejection.CommitSHA != strings.Repeat("b", 40) {
		t.Errorf("catalog rejection attempt = %#v", rejection)
	}
	acceptance := page.Items[2]
	if acceptance.Outcome != repository.RefreshOutcomeAccepted || acceptance.CommitSHA == nil ||
		*acceptance.CommitSHA != commitSHA || acceptance.Diagnostic != nil {
		t.Errorf("accepted attempt = %#v", acceptance)
	}
}
