package promotion

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/repository"
)

type candidateBranchTestStore struct {
	repositories []repository.Repository
	references   map[uuid.UUID][]CandidateBranchReference
	cleanups     []CandidateBranchCleanup
}

func (s *candidateBranchTestStore) ListRepositories(context.Context) ([]repository.Repository, error) {
	return s.repositories, nil
}

func (s *candidateBranchTestStore) ListPromotionCandidateBranchReferences(
	_ context.Context,
	repositoryID uuid.UUID,
) ([]CandidateBranchReference, error) {
	return append([]CandidateBranchReference(nil), s.references[repositoryID]...), nil
}

func (s *candidateBranchTestStore) UpsertPromotionCandidateBranchCleanup(
	_ context.Context,
	cleanup CandidateBranchCleanup,
) error {
	s.cleanups = append(s.cleanups, cleanup)

	return nil
}

type candidateBranchTestRemote struct {
	branches map[string]string
	listErr  error
	deleted  []string
}

func (r *candidateBranchTestRemote) ListCandidateBranches(
	context.Context,
	repository.Repository,
	string,
) (map[string]string, error) {
	return r.branches, r.listErr
}

func (r *candidateBranchTestRemote) DeleteCandidateBranch(
	_ context.Context,
	_ repository.Repository,
	_ string,
	branch string,
	expectedRevision string,
) (repository.CandidateBranchDeleteResult, error) {
	r.deleted = append(r.deleted, branch+"@"+expectedRevision)

	return repository.CandidateBranchDeleted, nil
}

func TestCandidateBranchServiceDeletesOnlyExpiredExactTerminalBranches(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 24, 12, 0, 0, 0, time.UTC)
	repositoryID := uuid.New()
	record := repository.Repository{
		Config: repository.Config{Name: "ops"}, //nolint:modernize // A keyed embedded Config keeps this fixture focused.
		ID:     repositoryID,
	}
	sha := func(character string) string { return strings.Repeat(character, 40) }
	reference := func(branch, revision string, status PromotionStatus, updatedAt time.Time) CandidateBranchReference {
		var kargoOutcomeAt *time.Time
		if status == StatusApproved {
			kargoOutcomeAt = &updatedAt
		}
		return CandidateBranchReference{
			PromotionID:       uuid.New(),
			Integration:       "prod",
			RepositoryID:      repositoryID,
			CandidateBranch:   branch,
			CandidateRevision: revision,
			Status:            status,
			UpdatedAt:         updatedAt,
			KargoOutcomeAt:    kargoOutcomeAt,
		}
	}
	store := &candidateBranchTestStore{
		repositories: []repository.Repository{record},
		references: map[uuid.UUID][]CandidateBranchReference{
			repositoryID: {
				reference("kargo/promotion/protected", sha("a"), StatusPending, now.Add(-48*time.Hour)),
				reference("kargo/promotion/waiting", sha("b"), StatusApproved, now.Add(-23*time.Hour)),
				reference("kargo/promotion/expired", sha("c"), StatusRejected, now.Add(-24*time.Hour)),
				reference("kargo/promotion/moved", sha("d"), StatusExpired, now.Add(-48*time.Hour)),
			},
		},
	}
	remote := &candidateBranchTestRemote{branches: map[string]string{
		"kargo/promotion/protected": sha("a"),
		"kargo/promotion/waiting":   sha("b"),
		"kargo/promotion/expired":   sha("c"),
		"kargo/promotion/moved":     sha("f"),
		"kargo/promotion/untracked": sha("1"),
	}}
	service := newCandidateBranchTestService(store, remote)
	service.now = func() time.Time { return now }

	count, err := service.Refresh(t.Context())
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if count != 5 {
		t.Errorf("Refresh() count = %d, want 5", count)
	}
	if len(remote.deleted) != 1 || remote.deleted[0] != "kargo/promotion/expired@"+sha("c") {
		t.Errorf("deleted branches = %#v", remote.deleted)
	}
	if len(store.cleanups) != 2 {
		t.Fatalf("cleanup statuses = %#v", store.cleanups)
	}
	if store.cleanups[0].Status != CandidateBranchCleanupRunning {
		t.Errorf("initial cleanup status = %q", store.cleanups[0].Status)
	}
	completed := store.cleanups[1]
	if completed.Status != CandidateBranchCleanupSucceeded || completed.FinishedAt == nil ||
		completed.ObservedBranches != 5 || completed.DeletedBranches != 1 || completed.RetainedBranches != 4 {
		t.Errorf("completed cleanup = %#v", completed)
	}
}

func TestCandidateBranchServiceRecordsScanFailure(t *testing.T) {
	t.Parallel()

	repositoryID := uuid.New()
	store := &candidateBranchTestStore{
		repositories: []repository.Repository{{
			Config: repository.Config{Name: "ops"}, //nolint:modernize // A keyed embedded Config keeps this fixture focused.
			ID:     repositoryID,
		}},
	}
	remote := &candidateBranchTestRemote{listErr: errors.New("unavailable")}
	service := newCandidateBranchTestService(store, remote)

	if _, err := service.Refresh(t.Context()); err == nil {
		t.Fatal("Refresh() error = nil")
	}
	if len(store.cleanups) != 2 || store.cleanups[1].Status != CandidateBranchCleanupFailed ||
		store.cleanups[1].FinishedAt == nil || store.cleanups[1].Error != candidateBranchCleanupFailureMessage {
		t.Fatalf("cleanup statuses = %#v", store.cleanups)
	}
}

func TestClassifyCandidateBranchProtectsNonterminalAndDisabledIntegrations(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 24, 12, 0, 0, 0, time.UTC)
	revision := strings.Repeat("a", 40)
	tests := []struct {
		name       string
		references []CandidateBranchReference
		reason     string
	}{
		{
			name: "nonterminal",
			references: []CandidateBranchReference{
				{Integration: "prod", CandidateRevision: revision, Status: StatusApproved, UpdatedAt: now.Add(-48 * time.Hour)},
				{Integration: "prod", CandidateRevision: revision, Status: StatusCreating, UpdatedAt: now.Add(-time.Hour)},
			},
			reason: "nonterminal",
		},
		{
			name: "disabled integration",
			references: []CandidateBranchReference{{
				Integration:       "edge",
				CandidateRevision: revision,
				Status:            StatusApproved,
				UpdatedAt:         now.Add(-48 * time.Hour),
			}},
			reason: "disabled",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			decision := classifyCandidateBranch(
				revision,
				test.references,
				map[string]struct{}{"prod": {}},
				24*time.Hour,
				now,
			)
			if decision.cleanup || !strings.Contains(decision.reason, test.reason) {
				t.Fatalf("classifyCandidateBranch() = %#v", decision)
			}
		})
	}
}

func newCandidateBranchTestService(
	store CandidateBranchStore,
	remote repository.CandidateBranchRemote,
) *CandidateBranchService {
	return NewCandidateBranchService(
		IntegrationSet{"prod": {CandidateBranchCleanup: &CandidateBranchCleanupConfig{
			Retention:   24 * time.Hour,
			Credentials: map[string]string{"ops": "cleanup"},
		}}},
		store,
		remote,
		time.Minute,
		30*time.Second,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
}
