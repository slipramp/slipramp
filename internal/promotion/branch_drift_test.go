package promotion

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/repository"
)

type branchDriftTestStore struct {
	stages       []Stage
	repositories []repository.Repository
}

func (s *branchDriftTestStore) ListPromotionStages(context.Context) ([]Stage, error) {
	return s.stages, nil
}

func (s *branchDriftTestStore) ListRepositories(context.Context) ([]repository.Repository, error) {
	return s.repositories, nil
}

type recordingBranchScanner struct {
	calls    []repository.Repository
	requests [][]repository.BranchComparisonRequest
	results  []repository.BranchComparison
	err      error
}

func (s *recordingBranchScanner) ScanBranches(
	_ context.Context,
	record repository.Repository,
	requests []repository.BranchComparisonRequest,
) ([]repository.BranchComparison, error) {
	s.calls = append(s.calls, record)
	s.requests = append(s.requests, requests)

	return slicesClone(s.results), s.err
}

func TestBranchDriftServiceGroupsStagesByRepositoryAndCachesScans(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	repositoryID := uuid.New()
	noOpStage := branchDriftTestStage(
		"prod",
		"ops",
		"external-dns-prod",
		strings.Repeat("a", 40),
		strings.Repeat("b", 40),
		strings.Repeat("b", 40),
		now,
	)
	driftedStage := branchDriftTestStage(
		"edge",
		"ops",
		"cert-manager-prod",
		strings.Repeat("c", 40),
		strings.Repeat("d", 40),
		strings.Repeat("e", 40),
		now,
	)
	store := &branchDriftTestStore{
		stages: []Stage{driftedStage, noOpStage},
		repositories: []repository.Repository{{
			ID:       repositoryID,
			Name:     "ops",
			CloneURL: "https://example.com/ops.git",
			Branch:   "main",
		}},
	}
	scanner := &recordingBranchScanner{
		results: []repository.BranchComparison{
			{
				SourceBranchCommit: strings.Repeat("f", 40),
				TargetBranchCommit: strings.Repeat("f", 40),
				SourceDrift:        true,
				TargetDrift:        true,
			},
			{
				SourceBranchCommit: strings.Repeat("a", 40),
				TargetBranchCommit: strings.Repeat("b", 40),
			},
		},
	}
	service := NewBranchDriftService(
		IntegrationSet{"prod": {}, "edge": {}},
		store,
		scanner,
		time.Minute,
		30*time.Second,
	)
	clock := now
	service.now = func() time.Time { return clock }

	result, err := service.List(t.Context(), BranchDriftListFilter{Limit: DefaultListLimit})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if result.Total != 1 || len(result.Items) != 1 || len(result.Items[0].Stages) != 2 {
		t.Fatalf("List() = %#v", result)
	}
	if len(scanner.calls) != 1 || len(scanner.requests[0]) != 2 {
		t.Fatalf("repository scans = %d, requests = %#v", len(scanner.calls), scanner.requests)
	}
	if result.Items[0].Stages[0].Stage.ID != driftedStage.ID ||
		result.Items[0].Stages[0].Status != BranchDriftStatusDrifted {
		t.Errorf("drifted Stage = %#v", result.Items[0].Stages[0])
	}
	if result.Items[0].Stages[1].Stage.ID != noOpStage.ID ||
		result.Items[0].Stages[1].Status != BranchDriftStatusCurrent ||
		!result.Items[0].Stages[1].NoOpRender {
		t.Errorf("no-op Stage = %#v", result.Items[0].Stages[1])
	}

	drifted, err := service.List(t.Context(), BranchDriftListFilter{
		Status: BranchDriftStatusDrifted,
		Limit:  DefaultListLimit,
	})
	if err != nil {
		t.Fatalf("status-filtered List() error = %v", err)
	}
	if drifted.Total != 1 || len(drifted.Items) != 1 || len(drifted.Items[0].Stages) != 1 ||
		drifted.Items[0].Stages[0].Stage.ID != driftedStage.ID {
		t.Errorf("status-filtered List() = %#v", drifted)
	}
	if len(scanner.calls) != 1 {
		t.Errorf("status-filtered repository scans = %d, want cached scan", len(scanner.calls))
	}

	store.stages[0].LastObservedAt = now.Add(time.Second)
	clock = now.Add(30 * time.Second)
	if _, err := service.List(t.Context(), BranchDriftListFilter{Limit: DefaultListLimit}); err != nil {
		t.Fatalf("cached List() error = %v", err)
	}
	if len(scanner.calls) != 1 {
		t.Errorf("cached repository scans = %d, want 1", len(scanner.calls))
	}

	clock = now.Add(time.Minute)
	if _, err := service.List(t.Context(), BranchDriftListFilter{Limit: DefaultListLimit}); err != nil {
		t.Fatalf("expired-cache List() error = %v", err)
	}
	if len(scanner.calls) != 2 {
		t.Errorf("expired-cache repository scans = %d, want 2", len(scanner.calls))
	}
}

func TestBranchDriftServiceRejectsInvalidStatusFilter(t *testing.T) {
	t.Parallel()

	service := NewBranchDriftService(
		IntegrationSet{},
		&branchDriftTestStore{},
		&recordingBranchScanner{},
		time.Minute,
		30*time.Second,
	)

	_, err := service.List(t.Context(), BranchDriftListFilter{
		Status: "stale",
		Limit:  DefaultListLimit,
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("List() error = %v, want ErrInvalid", err)
	}
}

func TestBranchDriftServiceAppliesStatusFilterToMissingRepositories(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	store := &branchDriftTestStore{stages: []Stage{branchDriftTestStage(
		"prod",
		"missing",
		"app-prod",
		strings.Repeat("a", 40),
		strings.Repeat("b", 40),
		strings.Repeat("c", 40),
		now,
	)}}
	service := NewBranchDriftService(
		IntegrationSet{"prod": {}},
		store,
		&recordingBranchScanner{},
		time.Minute,
		30*time.Second,
	)
	service.now = func() time.Time { return now }

	current, err := service.List(t.Context(), BranchDriftListFilter{
		Status: BranchDriftStatusCurrent,
		Limit:  DefaultListLimit,
	})
	if err != nil {
		t.Fatalf("current List() error = %v", err)
	}
	if current.Total != 0 || len(current.Items) != 0 {
		t.Errorf("current List() = %#v", current)
	}

	unknown, err := service.List(t.Context(), BranchDriftListFilter{
		Status: BranchDriftStatusUnknown,
		Limit:  DefaultListLimit,
	})
	if err != nil {
		t.Fatalf("unknown List() error = %v", err)
	}
	if unknown.Total != 1 || len(unknown.Items) != 1 || len(unknown.Items[0].Stages) != 1 {
		t.Errorf("unknown List() = %#v", unknown)
	}
}

func TestBranchDriftServiceUsesAcknowledgementsAndFiltersWithoutScanningUnknownStages(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	unknownStage := branchDriftTestStage("prod", "ops", "unknown", "", "", "", now)
	unknownStage.SuccessfulRenderAcknowledgement = nil
	unknownStage.Project = "wanted"
	otherStage := branchDriftTestStage(
		"prod",
		"ops",
		"other",
		strings.Repeat("a", 40),
		strings.Repeat("b", 40),
		strings.Repeat("c", 40),
		now,
	)
	otherStage.Project = "wanted"
	unknownStage.Application = "wanted-app"
	otherStage.Application = "other-app"
	store := &branchDriftTestStore{
		stages: []Stage{unknownStage, otherStage},
		repositories: []repository.Repository{{
			ID:       uuid.New(),
			Name:     "ops",
			CloneURL: "https://example.com/ops.git",
			Branch:   "main",
		}},
	}
	scanner := &recordingBranchScanner{
		results: []repository.BranchComparison{{
			SourceBranchCommit: strings.Repeat("a", 40),
			TargetBranchCommit: strings.Repeat("c", 40),
		}},
	}
	service := NewBranchDriftService(
		IntegrationSet{"prod": {}, "edge": {}},
		store,
		scanner,
		time.Minute,
		30*time.Second,
	)
	service.now = func() time.Time { return now }

	result, err := service.List(t.Context(), BranchDriftListFilter{
		Integration: "prod",
		Project:     "wanted",
		Application: "wanted-app",
		Limit:       DefaultListLimit,
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(scanner.calls) != 1 || len(scanner.requests[0]) != 1 ||
		scanner.requests[0][0].TargetBranch != otherStage.TargetBranch {
		t.Fatalf("full repository scan requests = %#v", scanner.requests)
	}
	if len(result.Items) != 1 || len(result.Items[0].Stages) != 1 {
		t.Fatalf("filtered List() = %#v", result)
	}
	stage := result.Items[0].Stages[0]
	if stage.Stage.ID != unknownStage.ID || stage.Status != BranchDriftStatusUnknown ||
		!strings.Contains(stage.Message, "no successful-render acknowledgement") {
		t.Errorf("unknown Stage = %#v", stage)
	}
}

func branchDriftTestStage(
	integration string,
	repositoryName string,
	name string,
	sourceRevision string,
	targetRevision string,
	resultRevision string,
	now time.Time,
) Stage {
	acknowledgement, err := json.Marshal(successfulRenderAcknowledgement{
		SourceRevision: sourceRevision,
		TargetRevision: targetRevision,
		ResultRevision: resultRevision,
	})
	if err != nil {
		panic(err)
	}

	return Stage{
		ID:                              uuid.New(),
		Integration:                     integration,
		ClusterName:                     "colo",
		Project:                         "kargo-core",
		Name:                            name,
		Application:                     name,
		Environment:                     "prod",
		RepositoryName:                  repositoryName,
		RepositoryURL:                   "https://example.com/ops.git",
		TargetBranch:                    "stage/" + name + "/prod",
		SourcePaths:                     []string{"apps/" + name},
		SuccessfulRenderAcknowledgement: acknowledgement,
		LastObservedAt:                  now,
	}
}

func slicesClone(values []repository.BranchComparison) []repository.BranchComparison {
	return append([]repository.BranchComparison(nil), values...)
}
