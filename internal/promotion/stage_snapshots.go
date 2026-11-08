package promotion

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/repository"
)

type StageObservation struct {
	Project                         string
	Name                            string
	KubernetesUID                   string
	ResourceVersion                 string
	Application                     string
	Environment                     string
	RepositoryURL                   string
	TargetBranch                    string
	SourcePaths                     []string
	RequestedFreight                []FreightRequest
	Freight                         Freight
	CandidateFreight                Freight
	AutoPromotionEnabled            bool
	ControlFlow                     bool
	Conditions                      []StageCondition
	CurrentPromotion                *StagePromotionReference
	LastPromotion                   *StagePromotionReference
	HealthStatus                    *string
	ArgoApplicationNamespace        *string
	SuccessfulRenderAcknowledgement *SuccessfulRenderAcknowledgement
}

type StageSnapshotReplacement struct {
	SnapshotID uuid.UUID
	ObservedAt time.Time
	Stages     []StageObservation
}

type StageSnapshotStore interface {
	ReplacePromotionStageSnapshot(context.Context, StageSnapshot, []Stage) error
}

type RepositoryLister interface {
	ListRepositories(context.Context) ([]repository.Repository, error)
}

type StageService struct {
	integrations IntegrationSet
	repositories RepositoryLister
	snapshots    StageSnapshotStore
	newID        func() uuid.UUID
	now          func() time.Time
}

func NewStageService(
	integrations IntegrationSet,
	repositories RepositoryLister,
	snapshots StageSnapshotStore,
) *StageService {
	return &StageService{
		integrations: integrations,
		repositories: repositories,
		snapshots:    snapshots,
		newID:        uuid.New,
		now:          time.Now,
	}
}

func (s *StageService) ReplaceStageSnapshot(
	ctx context.Context,
	integrationName string,
	clusterName string,
	replacement StageSnapshotReplacement,
) error {
	integration, ok := s.integrations.Lookup(integrationName)
	if !ok {
		return ErrIntegrationNotFound
	}
	if integration.ClusterName != clusterName {
		return ErrIntegrationCluster
	}
	if replacement.SnapshotID == uuid.Nil || replacement.ObservedAt.IsZero() {
		return fmt.Errorf("%w: snapshot ID and observation time are required", ErrInvalid)
	}

	repositories, err := s.repositories.ListRepositories(ctx)
	if err != nil {
		return fmt.Errorf("list repositories for Kargo Stage snapshot: %w", err)
	}
	allowedRepositories := make(map[string][]repository.Repository)
	for _, configured := range repositories {
		if slices.Contains(integration.Repositories, configured.Name) {
			allowedRepositories[configured.CloneURL] = append(
				allowedRepositories[configured.CloneURL],
				configured,
			)
		}
	}

	acceptedAt := s.now().UTC().Truncate(time.Microsecond)
	observedAt := replacement.ObservedAt.UTC().Truncate(time.Microsecond)
	if observedAt.After(acceptedAt) {
		observedAt = acceptedAt
	}
	snapshot := StageSnapshot{
		SnapshotID:  replacement.SnapshotID,
		Integration: integrationName,
		ClusterName: clusterName,
		ObservedAt:  observedAt,
		AcceptedAt:  acceptedAt,
	}
	if err := snapshot.Validate(); err != nil {
		return err
	}

	stages := make([]Stage, 0, len(replacement.Stages))
	for _, observed := range replacement.Stages {
		matches := allowedRepositories[observed.RepositoryURL]
		repositoryName := ""
		if len(matches) == 1 {
			repositoryName = matches[0].Name
		}

		stage := Stage{
			ID:                              s.newID(),
			Integration:                     integrationName,
			ClusterName:                     clusterName,
			Project:                         observed.Project,
			Name:                            observed.Name,
			KubernetesUID:                   observed.KubernetesUID,
			ResourceVersion:                 observed.ResourceVersion,
			Application:                     observed.Application,
			Environment:                     observed.Environment,
			RepositoryName:                  repositoryName,
			RepositoryURL:                   observed.RepositoryURL,
			TargetBranch:                    observed.TargetBranch,
			SourcePaths:                     slices.Clone(observed.SourcePaths),
			RequestedFreight:                cloneFreightRequests(observed.RequestedFreight),
			Freight:                         cloneFreight(observed.Freight),
			CandidateFreight:                cloneFreight(observed.CandidateFreight),
			AutoPromotionEnabled:            observed.AutoPromotionEnabled,
			ControlFlow:                     observed.ControlFlow,
			Conditions:                      cloneStageConditions(observed.Conditions),
			CurrentPromotion:                cloneStagePromotionReference(observed.CurrentPromotion),
			LastPromotion:                   cloneStagePromotionReference(observed.LastPromotion),
			HealthStatus:                    cloneOptionalString(observed.HealthStatus),
			ArgoApplicationNamespace:        cloneOptionalString(observed.ArgoApplicationNamespace),
			SuccessfulRenderAcknowledgement: cloneRenderAcknowledgement(observed.SuccessfulRenderAcknowledgement),
			FirstObservedAt:                 snapshot.ObservedAt,
			LastObservedAt:                  snapshot.ObservedAt,
		}
		if err := stage.Validate(); err != nil {
			return err
		}
		stages = append(stages, stage)
	}

	return s.snapshots.ReplacePromotionStageSnapshot(ctx, snapshot, stages)
}

func cloneStageConditions(values []StageCondition) []StageCondition {
	cloned := slices.Clone(values)
	for index := range cloned {
		if values[index].LastTransitionTime != nil {
			cloned[index].LastTransitionTime = new(*values[index].LastTransitionTime)
		}
	}

	return cloned
}

func cloneStagePromotionReference(value *StagePromotionReference) *StagePromotionReference {
	if value == nil {
		return nil
	}
	cloned := *value
	if value.FinishedAt != nil {
		cloned.FinishedAt = new(*value.FinishedAt)
	}

	return &cloned
}
