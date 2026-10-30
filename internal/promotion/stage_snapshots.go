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
	DirectFreight                   Freight
	AutoPromotionEnabled            bool
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
		if len(matches) != 1 {
			return fmt.Errorf(
				"%w: repository URL %q matches %d allowed repositories",
				ErrRepositoryNotAllowed,
				observed.RepositoryURL,
				len(matches),
			)
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
			RepositoryName:                  matches[0].Name,
			RepositoryURL:                   observed.RepositoryURL,
			TargetBranch:                    observed.TargetBranch,
			SourcePaths:                     slices.Clone(observed.SourcePaths),
			RequestedFreight:                cloneFreightRequests(observed.RequestedFreight),
			Freight:                         cloneFreight(observed.Freight),
			DirectFreight:                   cloneFreight(observed.DirectFreight),
			AutoPromotionEnabled:            observed.AutoPromotionEnabled,
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
