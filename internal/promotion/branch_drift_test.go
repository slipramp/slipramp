package promotion

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
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
	mutex    sync.Mutex
	calls    []repository.Repository
	requests [][]repository.BranchComparisonRequest
	results  []repository.BranchComparison
	err      error
}

type blockingBranchScanner struct {
	started chan string
	release chan struct{}
}

func (s *blockingBranchScanner) ScanBranches(
	ctx context.Context,
	record repository.Repository,
	requests []repository.BranchComparisonRequest,
) ([]repository.BranchComparison, error) {
	select {
	case s.started <- record.Name:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case <-s.release:
		return make([]repository.BranchComparison, len(requests)), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *recordingBranchScanner) ScanBranches(
	_ context.Context,
	record repository.Repository,
	requests []repository.BranchComparisonRequest,
) ([]repository.BranchComparison, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
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
		branchDriftTestLogger(),
	)
	clock := now
	service.now = func() time.Time { return clock }

	if _, err := service.Refresh(t.Context()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
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
	if _, err := service.Refresh(t.Context()); err != nil {
		t.Fatalf("cached Refresh() error = %v", err)
	}
	if len(scanner.calls) != 1 {
		t.Errorf("cached repository scans = %d, want 1", len(scanner.calls))
	}

	clock = now.Add(time.Minute)
	if _, err := service.Refresh(t.Context()); err != nil {
		t.Fatalf("expired-cache Refresh() error = %v", err)
	}
	if len(scanner.calls) != 2 {
		t.Errorf("expired-cache repository scans = %d, want 2", len(scanner.calls))
	}
}

func TestBranchDriftServiceRefreshesRepositoriesWithoutBlockingReads(t *testing.T) {
	now := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	store := &branchDriftTestStore{}
	for _, name := range []string{"alpha", "beta"} {
		store.stages = append(store.stages, branchDriftTestStage(
			"prod", name, name+"-prod",
			strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40), now,
		))
		store.repositories = append(store.repositories, repository.Repository{
			ID:     uuid.New(),
			Name:   name,
			Branch: "main",
		})
	}
	scanner := &blockingBranchScanner{
		started: make(chan string, 2),
		release: make(chan struct{}),
	}
	service := NewBranchDriftService(
		IntegrationSet{"prod": {}},
		store,
		scanner,
		time.Minute,
		30*time.Second,
		branchDriftTestLogger(),
	)
	service.now = func() time.Time { return now }

	refreshResult := make(chan error, 1)
	go func() {
		_, err := service.Refresh(t.Context())
		refreshResult <- err
	}()

	started := make([]string, 0, 2)
	for range 2 {
		select {
		case name := <-scanner.started:
			started = append(started, name)
		case <-time.After(time.Second):
			t.Fatal("repository scans did not start concurrently")
		}
	}
	slices.Sort(started)
	if !slices.Equal(started, []string{"alpha", "beta"}) {
		t.Fatalf("concurrent scans started for %v", started)
	}

	secondCount, err := service.Refresh(t.Context())
	if err != nil {
		t.Fatalf("concurrent Refresh() error = %v", err)
	}
	if secondCount != 0 {
		t.Errorf("concurrent Refresh() scans = %d, want shared in-flight scans", secondCount)
	}

	listResult := make(chan BranchDriftList, 1)
	listError := make(chan error, 1)
	go func() {
		result, listErr := service.List(t.Context(), BranchDriftListFilter{Limit: DefaultListLimit})
		listResult <- result
		listError <- listErr
	}()
	select {
	case result := <-listResult:
		if err := <-listError; err != nil {
			t.Fatalf("List() during refresh error = %v", err)
		}
		if result.Total != 2 || len(result.Items) != 2 {
			t.Fatalf("List() during refresh = %#v", result)
		}
		for _, item := range result.Items {
			if item.Stages[0].Status != BranchDriftStatusUnknown ||
				!strings.Contains(item.Stages[0].Message, "refresh is pending") {
				t.Errorf("pending branch drift = %#v", item.Stages[0])
			}
		}
	case <-time.After(time.Second):
		t.Fatal("List() waited for repository scans")
	}

	close(scanner.release)
	if err := <-refreshResult; err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	result, err := service.List(t.Context(), BranchDriftListFilter{Limit: DefaultListLimit})
	if err != nil {
		t.Fatalf("List() after refresh error = %v", err)
	}
	for _, item := range result.Items {
		if item.Stages[0].Status != BranchDriftStatusCurrent {
			t.Errorf("refreshed branch drift = %#v", item.Stages[0])
		}
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
		branchDriftTestLogger(),
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
		name   string
		filter BranchDriftListFilter
		total  int
		stages []string
	}{
		{
			name:   "first page within one repository",
			filter: BranchDriftListFilter{Limit: 2},
			total:  9,
			stages: []string{"alpha-one", "alpha-three"},
		},
		{
			name:   "page spans repository boundary",
			filter: BranchDriftListFilter{Limit: 2, Offset: 2},
			total:  9,
			stages: []string{"alpha-two", "beta-one"},
		},
		{
			name:   "page continues within repository",
			filter: BranchDriftListFilter{Limit: 2, Offset: 4},
			total:  9,
			stages: []string{"beta-three", "beta-two"},
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
			name:   "repository filter counts stages",
			filter: BranchDriftListFilter{Repository: "beta", Limit: 2, Offset: 2},
			total:  3,
			stages: []string{"beta-two"},
		},
		{
			name:   "application filter before pagination",
			filter: BranchDriftListFilter{Application: "beta-two", Limit: 2},
			total:  1,
			stages: []string{"beta-two"},
		},
		{
			name:   "status filter before pagination",
			filter: BranchDriftListFilter{Status: BranchDriftStatusCurrent, Limit: 2, Offset: 1},
			total:  4,
			stages: []string{"alpha-two", "beta-one"},
		},
		{
			name:   "status filtered last page",
			filter: BranchDriftListFilter{Status: BranchDriftStatusCurrent, Limit: 2, Offset: 3},
			total:  4,
			stages: []string{"beta-two"},
		},
		{
			name:   "status filtered offset beyond results",
			filter: BranchDriftListFilter{Status: BranchDriftStatusCurrent, Limit: 2, Offset: 4},
			total:  4,
		},
		{
			name:   "unknown status paginates missing repository",
			filter: BranchDriftListFilter{Status: BranchDriftStatusUnknown, Limit: 2, Offset: 1},
			total:  3,
			stages: []string{"missing-three", "missing-two"},
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
				branchDriftTestLogger(),
			)
			if _, err := service.Refresh(t.Context()); err != nil {
				t.Fatalf("Refresh() error = %v", err)
			}
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
			slices.Sort(scanned)
			if !slices.Equal(scanned, []string{"alpha", "beta"}) {
				t.Errorf("scanned repositories = %v, want [alpha beta]", scanned)
			}

			if _, err := service.List(t.Context(), test.filter); err != nil {
				t.Fatalf("cached List() error = %v", err)
			}
			if len(scanner.calls) != 2 {
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
		branchDriftTestLogger(),
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
		branchDriftTestLogger(),
	)
	service.now = func() time.Time { return now }

	if _, err := service.Refresh(t.Context()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
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
	store.stages[0].SuccessfulRenderAcknowledgement = &SuccessfulRenderAcknowledgement{SourceRevision: "invalid"}
	if _, err := service.Refresh(t.Context()); err != nil {
		t.Fatalf("refresh invalid acknowledgement: %v", err)
	}
	result, err = service.List(t.Context(), BranchDriftListFilter{Application: "wanted-app", Limit: DefaultListLimit})
	if err != nil || len(result.Items) != 1 || len(result.Items[0].Stages) != 1 ||
		result.Items[0].Stages[0].Status != BranchDriftStatusUnknown ||
		!strings.Contains(result.Items[0].Stages[0].Message, "acknowledgement is invalid") {
		t.Errorf("invalid acknowledgement = %#v, %v", result, err)
	}
	if len(scanner.requests) != 2 || len(scanner.requests[1]) != 1 {
		t.Errorf("invalid acknowledgement was scanned: %#v", scanner.requests)
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
	acknowledgement := &SuccessfulRenderAcknowledgement{
		SourceRevision: sourceRevision,
		TargetRevision: targetRevision,
		ResultRevision: resultRevision,
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

func branchDriftTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func slicesClone(values []repository.BranchComparison) []repository.BranchComparison {
	return append([]repository.BranchComparison(nil), values...)
}
