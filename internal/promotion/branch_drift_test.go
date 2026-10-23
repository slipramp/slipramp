package promotion

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
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
	if result.Total != 2 || len(result.Items) != 1 || len(result.Items[0].Stages) != 2 {
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

func TestBranchDriftServicePaginatesStagesAcrossRepositoryGroups(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	store := &branchDriftTestStore{}
	for _, name := range []string{"alpha", "beta", "missing"} {
		for _, suffix := range []string{"one", "two", "three"} {
			store.stages = append(store.stages, branchDriftTestStage(
				"prod", name, name+"-"+suffix,
				strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40), now,
			))
		}
		if name != "missing" {
			store.repositories = append(store.repositories, repository.Repository{
				ID:     uuid.New(),
				Name:   name,
				Branch: "main",
			})
		}
	}
	archivedStage := store.stages[0]
	archivedStage.ID = uuid.New()
	archivedStage.ArchivedAt = &now
	unconfiguredStage := store.stages[0]
	unconfiguredStage.ID = uuid.New()
	unconfiguredStage.Integration = "unconfigured"
	store.stages = append(store.stages, archivedStage, unconfiguredStage)
	slices.Reverse(store.stages)

	for _, test := range []struct {
		name    string
		filter  BranchDriftListFilter
		total   int
		stages  []string
		scanned []string
	}{
		{
			name:    "first page within one repository",
			filter:  BranchDriftListFilter{Limit: 2},
			total:   9,
			stages:  []string{"alpha-one", "alpha-three"},
			scanned: []string{"alpha"},
		},
		{
			name:    "page spans repository boundary",
			filter:  BranchDriftListFilter{Limit: 2, Offset: 2},
			total:   9,
			stages:  []string{"alpha-two", "beta-one"},
			scanned: []string{"alpha", "beta"},
		},
		{
			name:    "page continues within repository",
			filter:  BranchDriftListFilter{Limit: 2, Offset: 4},
			total:   9,
			stages:  []string{"beta-three", "beta-two"},
			scanned: []string{"beta"},
		},
		{
			name:   "last page retains missing repository error",
			filter: BranchDriftListFilter{Limit: 2, Offset: 8},
			total:  9,
			stages: []string{"missing-two"},
		},
		{
			name:   "offset beyond results",
			filter: BranchDriftListFilter{Limit: 2, Offset: 9},
			total:  9,
		},
		{
			name:    "repository filter counts stages",
			filter:  BranchDriftListFilter{Repository: "beta", Limit: 2, Offset: 2},
			total:   3,
			stages:  []string{"beta-two"},
			scanned: []string{"beta"},
		},
		{
			name:    "application filter before pagination",
			filter:  BranchDriftListFilter{Application: "beta-two", Limit: 2},
			total:   1,
			stages:  []string{"beta-two"},
			scanned: []string{"beta"},
		},
		{
			name:    "status filter before pagination",
			filter:  BranchDriftListFilter{Status: BranchDriftStatusCurrent, Limit: 2, Offset: 1},
			total:   4,
			stages:  []string{"alpha-two", "beta-one"},
			scanned: []string{"alpha", "beta"},
		},
		{
			name:    "status filtered last page",
			filter:  BranchDriftListFilter{Status: BranchDriftStatusCurrent, Limit: 2, Offset: 3},
			total:   4,
			stages:  []string{"beta-two"},
			scanned: []string{"alpha", "beta"},
		},
		{
			name:    "status filtered offset beyond results",
			filter:  BranchDriftListFilter{Status: BranchDriftStatusCurrent, Limit: 2, Offset: 4},
			total:   4,
			scanned: []string{"alpha", "beta"},
		},
		{
			name:    "unknown status paginates missing repository",
			filter:  BranchDriftListFilter{Status: BranchDriftStatusUnknown, Limit: 2, Offset: 1},
			total:   3,
			stages:  []string{"missing-three", "missing-two"},
			scanned: []string{"alpha", "beta"},
		},
		{
			name:   "no matching stages",
			filter: BranchDriftListFilter{Project: "absent", Limit: 2},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			scanner := &recordingBranchScanner{results: []repository.BranchComparison{
				{}, {SourceDrift: true}, {},
			}}
			service := NewBranchDriftService(
				IntegrationSet{"prod": {}}, store, scanner, time.Minute, 30*time.Second,
			)
			result, err := service.List(t.Context(), test.filter)
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}
			if result.Total != test.total || result.Limit != test.filter.Limit || result.Offset != test.filter.Offset {
				t.Errorf("List() pagination = total %d, limit %d, offset %d", result.Total, result.Limit, result.Offset)
			}
			var names []string
			for _, item := range result.Items {
				if len(item.Stages) == 0 {
					t.Error("List() returned an empty repository group")
				}
				if item.Repository.Name == "missing" && item.Error == "" {
					t.Error("List() lost missing repository error")
				}
				for _, stage := range item.Stages {
					if stage.Stage.RepositoryName != item.Repository.Name {
						t.Errorf("Stage %s is in the wrong repository group", stage.Stage.Name)
					}
					names = append(names, stage.Stage.Name)
				}
			}
			if !slices.Equal(names, test.stages) {
				t.Errorf("List() Stages = %v, want %v", names, test.stages)
			}
			var scanned []string
			for index, record := range scanner.calls {
				scanned = append(scanned, record.Name)
				if len(scanner.requests[index]) != 3 {
					t.Errorf("repository %s scan did not include all Stages", record.Name)
				}
			}
			if !slices.Equal(scanned, test.scanned) {
				t.Errorf("scanned repositories = %v, want %v", scanned, test.scanned)
			}

			if _, err := service.List(t.Context(), test.filter); err != nil {
				t.Fatalf("cached List() error = %v", err)
			}
			if len(scanner.calls) != len(test.scanned) {
				t.Error("cached List() rescanned repositories")
			}
		})
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
