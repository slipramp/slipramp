package inbox

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/slipramp/slipramp/internal/cluster"
	"github.com/slipramp/slipramp/internal/gittrigger"
	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/repository"
	"github.com/slipramp/slipramp/internal/run"
)

type StateReader interface {
	ListRepositories(context.Context) ([]repository.Repository, error)
	ListClusterObservations(context.Context) ([]cluster.Observation, error)
	ListInboxActiveRuns(context.Context) ([]ActiveRun, error)
	ListInboxPromotions(context.Context) ([]PromotionState, error)
	ListInboxFailedGitTriggerDeliveries(context.Context) ([]gittrigger.FailedDelivery, error)
}

type AcknowledgementStore interface {
	ListInboxProblemAcknowledgements(context.Context, uuid.UUID) ([]string, error)
	InsertInboxProblemAcknowledgements(context.Context, uuid.UUID, []string, time.Time) error
}

type PromotionExpirer interface {
	ExpirePendingAt(context.Context, time.Time) error
}

type Dependencies struct {
	State            StateReader
	Acknowledgements AcknowledgementStore
	Promotions       PromotionExpirer
}

type Service struct {
	state            StateReader
	acknowledgements AcknowledgementStore
	promotions       PromotionExpirer
	now              func() time.Time
}

type projectionSource struct {
	repositories       []repository.Repository
	clusters           []cluster.Observation
	runs               []ActiveRun
	promotions         []PromotionState
	gitTriggerFailures []gittrigger.FailedDelivery
	acknowledged       []string
}

func NewService(dependencies Dependencies) *Service {
	return &Service{
		state:            dependencies.State,
		acknowledgements: dependencies.Acknowledgements,
		promotions:       dependencies.Promotions,
		now:              time.Now,
	}
}

func (s *Service) Load(ctx context.Context, query Query) (Snapshot, error) {
	if query.PrincipalID == uuid.Nil {
		return Snapshot{}, errors.New("inbox query principal ID is required")
	}

	generatedAt := s.now().UTC()
	source, err := s.loadSource(ctx, generatedAt, &query.PrincipalID)
	if err != nil {
		return Snapshot{}, err
	}

	result, err := buildSnapshot(query, generatedAt, source)
	if err != nil {
		return Snapshot{}, fmt.Errorf("build Inbox snapshot: %w", err)
	}

	return result, nil
}

func (s *Service) AcknowledgeProblems(
	ctx context.Context,
	principalID uuid.UUID,
	problemIDs []string,
) error {
	if principalID == uuid.Nil {
		return fmt.Errorf("%w: principal ID is required", ErrInvalidAcknowledgement)
	}
	if err := validateProblemIDs(problemIDs); err != nil {
		return err
	}

	acknowledgedAt := s.now().UTC()
	source, err := s.loadSource(ctx, acknowledgedAt, nil)
	if err != nil {
		return err
	}
	current, err := buildSnapshot(Query{PrincipalID: principalID}, acknowledgedAt, source)
	if err != nil {
		return fmt.Errorf("build current Inbox snapshot: %w", err)
	}

	currentIDs := make(map[string]struct{}, len(current.Problems))
	for _, problem := range current.Problems {
		currentIDs[problem.ID] = struct{}{}
	}
	matching := make([]string, 0, len(problemIDs))
	for _, problemID := range problemIDs {
		if _, current := currentIDs[problemID]; current {
			matching = append(matching, problemID)
		}
	}
	if len(matching) == 0 {
		return nil
	}

	if err := s.acknowledgements.InsertInboxProblemAcknowledgements(
		ctx,
		principalID,
		matching,
		acknowledgedAt,
	); err != nil {
		return fmt.Errorf("persist Inbox problem acknowledgements: %w", err)
	}

	return nil
}

func (s *Service) loadSource(
	ctx context.Context,
	at time.Time,
	acknowledgementsFor *uuid.UUID,
) (projectionSource, error) {
	if s.state == nil || s.acknowledgements == nil || s.promotions == nil {
		return projectionSource{}, errors.New("inbox dependencies are not configured")
	}
	if err := s.promotions.ExpirePendingAt(ctx, at); err != nil {
		return projectionSource{}, fmt.Errorf("expire pending Promotions for Inbox: %w", err)
	}

	var source projectionSource
	group, groupContext := errgroup.WithContext(ctx)
	group.Go(func() error {
		var err error
		source.repositories, err = s.state.ListRepositories(groupContext)
		return wrapLoadError("Repositories", err)
	})
	group.Go(func() error {
		var err error
		source.clusters, err = s.state.ListClusterObservations(groupContext)
		return wrapLoadError("Cluster observations", err)
	})
	group.Go(func() error {
		var err error
		source.runs, err = s.state.ListInboxActiveRuns(groupContext)
		return wrapLoadError("active Runs", err)
	})
	group.Go(func() error {
		var err error
		source.promotions, err = s.state.ListInboxPromotions(groupContext)
		return wrapLoadError("Promotions", err)
	})
	group.Go(func() error {
		var err error
		source.gitTriggerFailures, err = s.state.ListInboxFailedGitTriggerDeliveries(groupContext)
		return wrapLoadError("Git-trigger failures", err)
	})
	if acknowledgementsFor != nil {
		group.Go(func() error {
			var err error
			source.acknowledged, err = s.acknowledgements.ListInboxProblemAcknowledgements(
				groupContext,
				*acknowledgementsFor,
			)
			return wrapLoadError("problem acknowledgements", err)
		})
	}
	if err := group.Wait(); err != nil {
		return projectionSource{}, err
	}

	return source, nil
}

func wrapLoadError(source string, err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("load Inbox %s: %w", source, err)
}

func buildSnapshot(query Query, generatedAt time.Time, source projectionSource) (Snapshot, error) {
	result := Snapshot{
		GeneratedAt: generatedAt,
		Decisions:   make([]Item, 0),
		Problems:    make([]Item, 0),
	}

	connectedClusters := make(map[string]bool, len(source.clusters))
	unreachableClusters := make(map[string]cluster.Observation)
	for _, observation := range source.clusters {
		connected := cluster.DeriveConnectionState(observation, generatedAt) == cluster.ConnectionStateConnected
		connectedClusters[observation.Name] = connected
		if !connected {
			unreachableClusters[observation.Name] = observation
		}
	}

	unknownRuns := make([]ActiveRun, 0)
	unknownRunCounts := make(map[string]int)
	for _, record := range source.runs {
		switch record.Status.Phase {
		case run.PhaseQueued:
			result.Activity.Queued++
		case run.PhaseAssigned, run.PhasePreparing:
			result.Activity.Starting++
		case run.PhaseRunning:
			result.Activity.Running++
		default:
			return Snapshot{}, fmt.Errorf("active Run %s has phase %q", record.ID, record.Status.Phase)
		}

		observationState, present := run.DeriveObservationState(
			record.Status,
			connectedClusters[record.ClusterName],
			generatedAt,
		)
		if present && observationState == run.ObservationStateUnknown {
			unknownRuns = append(unknownRuns, record)
			unknownRunCounts[record.ClusterName]++
		}
	}

	for _, record := range source.promotions {
		item, err := promotionItem(record)
		if err != nil {
			return Snapshot{}, err
		}
		switch record.Status {
		case promotion.StatusPending:
			if query.IncludePromotionDecisions {
				result.Decisions = append(result.Decisions, item)
			}
		case promotion.StatusError:
			result.Problems = append(result.Problems, item)
		default:
			return Snapshot{}, fmt.Errorf("relevant Promotion %s has status %q", record.ID, record.Status)
		}
	}

	for _, record := range source.repositories {
		if record.Refresh.Status == repository.RefreshStatusFailed {
			problem, err := repositoryRefreshItem(record)
			if err != nil {
				return Snapshot{}, err
			}
			result.Problems = append(result.Problems, problem)
		}
	}
	for _, observation := range source.clusters {
		if _, unreachable := unreachableClusters[observation.Name]; unreachable {
			result.Problems = append(
				result.Problems,
				clusterUnreachableItem(observation, unknownRunCounts[observation.Name]),
			)
		}
	}
	for _, record := range unknownRuns {
		if _, grouped := unreachableClusters[record.ClusterName]; !grouped {
			result.Problems = append(result.Problems, runObservationItem(record))
		}
	}
	for _, failure := range source.gitTriggerFailures {
		result.Problems = append(result.Problems, gitTriggerFailureItem(failure))
	}

	if len(source.acknowledged) > 0 {
		acknowledged := make(map[string]struct{}, len(source.acknowledged))
		for _, problemID := range source.acknowledged {
			acknowledged[problemID] = struct{}{}
		}
		result.Problems = slices.DeleteFunc(result.Problems, func(item Item) bool {
			_, hidden := acknowledged[item.ID]
			return hidden
		})
	}

	sortItems(result.Decisions)
	sortItems(result.Problems)

	return result, nil
}

func sortItems(items []Item) {
	slices.SortFunc(items, func(left Item, right Item) int {
		if timeOrder := right.OccurredAt.Compare(left.OccurredAt); timeOrder != 0 {
			return timeOrder
		}

		return strings.Compare(left.ID, right.ID)
	})
}
