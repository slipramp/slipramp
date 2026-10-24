package promotion

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/repository"
)

const candidateBranchCleanupFailureMessage = "Candidate branch cleanup failed; check Server logs and Git credentials."

type CandidateBranchCleanupStatus string

const (
	CandidateBranchCleanupRunning   CandidateBranchCleanupStatus = "running"
	CandidateBranchCleanupSucceeded CandidateBranchCleanupStatus = "succeeded"
	CandidateBranchCleanupFailed    CandidateBranchCleanupStatus = "failed"
)

type CandidateBranchReference struct {
	PromotionID       uuid.UUID
	Integration       string
	RepositoryID      uuid.UUID
	CandidateBranch   string
	CandidateRevision string
	Status            PromotionStatus
	UpdatedAt         time.Time
}

type CandidateBranchCleanup struct {
	RepositoryID     uuid.UUID
	Status           CandidateBranchCleanupStatus
	StartedAt        time.Time
	FinishedAt       *time.Time
	ObservedBranches int
	DeletedBranches  int
	RetainedBranches int
	Error            string
}

type CandidateBranchStore interface {
	ListRepositories(context.Context) ([]repository.Repository, error)
	ListPromotionCandidateBranchReferences(context.Context, uuid.UUID) ([]CandidateBranchReference, error)
	UpsertPromotionCandidateBranchCleanup(context.Context, CandidateBranchCleanup) error
}

type candidateBranchPolicy struct {
	credential   string
	retention    time.Duration
	integrations map[string]struct{}
}

type candidateBranchDecision struct {
	cleanup          bool
	expectedRevision string
	reason           string
}

type CandidateBranchService struct {
	store        CandidateBranchStore
	remote       repository.CandidateBranchRemote
	policies     map[string]candidateBranchPolicy
	interval     time.Duration
	fetchTimeout time.Duration
	logger       *slog.Logger
	now          func() time.Time
}

func NewCandidateBranchService(
	integrations IntegrationSet,
	store CandidateBranchStore,
	remote repository.CandidateBranchRemote,
	interval time.Duration,
	fetchTimeout time.Duration,
	logger *slog.Logger,
) *CandidateBranchService {
	policies := make(map[string]candidateBranchPolicy)
	for _, integrationName := range integrations.Names() {
		integration, _ := integrations.Lookup(integrationName)
		if integration.CandidateBranchCleanup == nil {
			continue
		}
		for repositoryName, credential := range integration.CandidateBranchCleanup.Credentials {
			policy, ok := policies[repositoryName]
			if !ok {
				policy = candidateBranchPolicy{
					credential:   credential,
					retention:    integration.CandidateBranchCleanup.Retention,
					integrations: make(map[string]struct{}),
				}
			}
			policy.integrations[integrationName] = struct{}{}
			policies[repositoryName] = policy
		}
	}

	return &CandidateBranchService{
		store:        store,
		remote:       remote,
		policies:     policies,
		interval:     interval,
		fetchTimeout: fetchTimeout,
		logger:       logger.With("component", "promotion_candidate_branches"),
		now:          time.Now,
	}
}

func (s *CandidateBranchService) Run(ctx context.Context) {
	s.refreshAndLog(ctx)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.refreshAndLog(ctx)
		}
	}
}

func (s *CandidateBranchService) Refresh(ctx context.Context) (int, error) {
	repositories, err := s.store.ListRepositories(ctx)
	if err != nil {
		return 0, err
	}

	count := 0
	var refreshErr error
	for _, record := range repositories {
		policy, ok := s.policies[record.Name]
		if !ok {
			continue
		}
		observed, scanErr := s.scanRepository(ctx, record, policy)
		count += observed
		if scanErr != nil {
			refreshErr = errors.Join(refreshErr, fmt.Errorf("scan repository %q: %w", record.Name, scanErr))
		}
	}

	return count, refreshErr
}

func (s *CandidateBranchService) scanRepository(
	ctx context.Context,
	record repository.Repository,
	policy candidateBranchPolicy,
) (int, error) {
	startedAt := s.currentTime()
	cleanup := CandidateBranchCleanup{
		RepositoryID: record.ID,
		Status:       CandidateBranchCleanupRunning,
		StartedAt:    startedAt,
	}
	if err := s.store.UpsertPromotionCandidateBranchCleanup(ctx, cleanup); err != nil {
		return 0, err
	}

	fetchContext, cancelFetch := context.WithTimeout(ctx, s.fetchTimeout)
	branches, err := s.remote.ListCandidateBranches(fetchContext, record, policy.credential)
	cancelFetch()
	if err != nil {
		return 0, s.finishCandidateBranchCleanup(ctx, &cleanup, err)
	}
	cleanup.ObservedBranches = len(branches)

	references, err := s.store.ListPromotionCandidateBranchReferences(ctx, record.ID)
	if err != nil {
		return cleanup.ObservedBranches, s.finishCandidateBranchCleanup(ctx, &cleanup, err)
	}
	referencesByBranch := make(map[string][]CandidateBranchReference)
	for _, reference := range references {
		referencesByBranch[reference.CandidateBranch] = append(
			referencesByBranch[reference.CandidateBranch],
			reference,
		)
	}

	var cleanupErr error
	for _, branch := range slices.Sorted(maps.Keys(branches)) {
		decision := classifyCandidateBranch(
			branches[branch],
			referencesByBranch[branch],
			policy.integrations,
			policy.retention,
			startedAt,
		)
		if !decision.cleanup {
			cleanup.RetainedBranches++
			s.logger.Debug(
				"retained candidate branch",
				"repository", record.Name,
				"branch", branch,
				"reason", decision.reason,
			)

			continue
		}

		deleteContext, cancelDelete := context.WithTimeout(ctx, s.fetchTimeout)
		result, deleteErr := s.remote.DeleteCandidateBranch(
			deleteContext,
			record,
			policy.credential,
			branch,
			decision.expectedRevision,
		)
		cancelDelete()
		if deleteErr != nil {
			cleanup.RetainedBranches++
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("delete candidate branch %q: %w", branch, deleteErr))
			s.logger.Error(
				"delete expired candidate branch",
				"repository", record.Name,
				"branch", branch,
				"error", deleteErr,
			)

			continue
		}

		switch result {
		case repository.CandidateBranchDeleted, repository.CandidateBranchAbsent:
			cleanup.DeletedBranches++
			s.logger.Info(
				"cleaned candidate branch",
				"repository", record.Name,
				"branch", branch,
				"result", result,
			)
		case repository.CandidateBranchMoved:
			cleanup.RetainedBranches++
			s.logger.Info(
				"retained moved candidate branch",
				"repository", record.Name,
				"branch", branch,
			)
		default:
			cleanup.RetainedBranches++
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("delete candidate branch %q: unexpected result %q", branch, result))
		}
	}

	return cleanup.ObservedBranches, s.finishCandidateBranchCleanup(ctx, &cleanup, cleanupErr)
}

func (s *CandidateBranchService) finishCandidateBranchCleanup(
	ctx context.Context,
	cleanup *CandidateBranchCleanup,
	cleanupErr error,
) error {
	finishedAt := s.currentTime()
	cleanup.FinishedAt = &finishedAt
	cleanup.Status = CandidateBranchCleanupSucceeded
	if cleanupErr != nil {
		cleanup.Status = CandidateBranchCleanupFailed
		cleanup.Error = candidateBranchCleanupFailureMessage
	}

	storeErr := s.store.UpsertPromotionCandidateBranchCleanup(ctx, *cleanup)

	return errors.Join(cleanupErr, storeErr)
}

func classifyCandidateBranch(
	remoteRevision string,
	references []CandidateBranchReference,
	cleanupIntegrations map[string]struct{},
	retention time.Duration,
	scannedAt time.Time,
) candidateBranchDecision {
	if len(references) == 0 {
		return candidateBranchDecision{reason: "untracked"}
	}
	for _, reference := range references {
		if _, ok := cleanupIntegrations[reference.Integration]; !ok {
			return candidateBranchDecision{reason: "cleanup disabled for referencing integration"}
		}
		if !reference.Status.Terminal() {
			return candidateBranchDecision{reason: "referenced by nonterminal Promotion"}
		}
	}

	latestTerminalAt := references[0].UpdatedAt
	matchingRevision := ""
	for _, reference := range references {
		if reference.UpdatedAt.After(latestTerminalAt) {
			latestTerminalAt = reference.UpdatedAt
		}
		if reference.CandidateRevision == remoteRevision {
			matchingRevision = reference.CandidateRevision
		}
	}
	if matchingRevision == "" {
		return candidateBranchDecision{reason: "remote revision does not match Promotion"}
	}
	if scannedAt.Before(latestTerminalAt.Add(retention)) {
		return candidateBranchDecision{reason: "retention window has not elapsed"}
	}

	return candidateBranchDecision{
		cleanup:          true,
		expectedRevision: matchingRevision,
	}
}

func (s *CandidateBranchService) currentTime() time.Time {
	return s.now().UTC().Truncate(time.Microsecond)
}

func (s *CandidateBranchService) refreshAndLog(ctx context.Context) {
	count, err := s.Refresh(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.Error("refresh candidate branches", "error", err)
		}

		return
	}
	s.logger.Debug("refreshed candidate branches", "count", count)
}
