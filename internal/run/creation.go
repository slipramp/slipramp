package run

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/placement"
	"github.com/slipramp/slipramp/internal/repository"
)

type CreationStore interface {
	CreateRun(ctx context.Context, record Run) (Run, error)
	GetRunByIdempotencyKey(
		ctx context.Context,
		principalID *uuid.UUID,
		sourceType SourceType,
		idempotencyKey string,
	) (Run, error)
	GetArtifactInputCandidate(context.Context, uuid.UUID) (ArtifactInput, error)
}

type RepositoryService interface {
	GetRepository(ctx context.Context, repositoryID uuid.UUID) (repository.Repository, error)
}

type CatalogService interface {
	GetTarget(context.Context, uuid.UUID) (catalog.TargetSummary, error)
	GetActiveRevision(ctx context.Context, repositoryID uuid.UUID) (catalog.Revision, error)
	GetRevision(context.Context, uuid.UUID, uuid.UUID) (catalog.Revision, error)
}

func (s *Service) CreateRun(ctx context.Context, request CreateRequest) (Run, error) {
	return s.createRun(ctx, request, nil)
}

func (s *Service) createRun(
	ctx context.Context,
	request CreateRequest,
	provenance *rerunProvenance,
) (Run, error) {
	if request.Inputs == nil {
		request.Inputs = map[string]uuid.UUID{}
	}
	if err := validateRequest(request); err != nil {
		return Run{}, err
	}

	parameters, err := catalog.NormalizeParameterValues(request.Parameters)
	if err != nil {
		return Run{}, invalidParameters(err)
	}

	digest, err := requestDigest(request, parameters, provenance)
	if err != nil {
		return Run{}, fmt.Errorf("digest run request: %w", err)
	}

	if request.IdempotencyKey != nil {
		existing, err := s.creation.GetRunByIdempotencyKey(
			ctx,
			actorPrincipalID(request.Actor),
			request.Source.Type,
			*request.IdempotencyKey,
		)
		if err == nil {
			if existing.RequestDigest != digest {
				return Run{}, ErrIdempotencyConflict
			}

			return existing, nil
		}
		if !errors.Is(err, ErrRunNotFound) {
			return Run{}, fmt.Errorf("get idempotent run: %w", err)
		}
	}

	targetRecord, err := s.catalogs.GetTarget(ctx, request.TargetID)
	if errors.Is(err, catalog.ErrTargetNotFound) || targetRecord.ArchivedAt != nil {
		return Run{}, ErrTargetNotFound
	}
	if err != nil {
		return Run{}, fmt.Errorf("get run Target: %w", err)
	}

	repositoryRecord, err := s.repositories.GetRepository(ctx, targetRecord.RepositoryID)
	if errors.Is(err, repository.ErrNotFound) {
		return Run{}, ErrRepositoryNotFound
	}
	if err != nil {
		return Run{}, fmt.Errorf("get run repository: %w", err)
	}
	inputs, pinnedRevisionID, err := s.resolveInputCandidates(ctx, request)
	if err != nil {
		return Run{}, err
	}

	if request.CatalogRevisionID != nil {
		if pinnedRevisionID != uuid.Nil && pinnedRevisionID != *request.CatalogRevisionID {
			return Run{}, fmt.Errorf("%w: artifact inputs do not match the pinned catalog revision", ErrInvalidInputs)
		}
		pinnedRevisionID = *request.CatalogRevisionID
	}

	var revision catalog.Revision
	if pinnedRevisionID == uuid.Nil {
		revision, err = s.catalogs.GetActiveRevision(ctx, targetRecord.RepositoryID)
	} else {
		revision, err = s.catalogs.GetRevision(ctx, targetRecord.RepositoryID, pinnedRevisionID)
	}
	if errors.Is(err, catalog.ErrNotFound) {
		return Run{}, ErrCatalogNotFound
	}
	if err != nil {
		return Run{}, fmt.Errorf("get active run catalog: %w", err)
	}

	target, found := findTarget(revision.Catalog.Targets, targetRecord.Target.Metadata.Name)
	if !found {
		return Run{}, ErrTargetNotFound
	}

	action, found := target.EffectiveAction(request.ActionName)
	if !found {
		return Run{}, ErrActionNotFound
	}

	if err := validateArtifactInputs(action, inputs); err != nil {
		return Run{}, err
	}

	runtime, found := findRuntime(revision.Catalog.Runtimes, action.Runtime)
	if !found {
		return Run{}, ErrRuntimeNotFound
	}

	placementConfig, found := s.placements.Lookup(action.Placement)
	if !found {
		return Run{}, ErrPlacementNotFound
	}

	parameters, err = catalog.ValidateParameterValues(action.Parameters, parameters)
	if err != nil {
		return Run{}, invalidParameters(err)
	}

	createdAt := s.now().UTC()
	createdBy, err := actorSnapshot(request.Actor)
	if err != nil {
		return Run{}, err
	}
	record := Run{
		ID:                s.newID(),
		Phase:             PhaseQueued,
		TargetID:          targetRecord.ID,
		RepositoryID:      targetRecord.RepositoryID,
		CatalogRevisionID: revision.ID,
		CommitSHA:         revision.CommitSHA,
		TargetName:        targetRecord.Target.Metadata.Name,
		ActionName:        request.ActionName,
		Parameters:        parameters,
		PlacementName:     action.Placement,
		ClusterName:       placementConfig.ClusterName,
		Source:            request.Source,
		IdempotencyKey:    cloneString(request.IdempotencyKey),
		RequestDigest:     digest,
		CreatedBy:         createdBy,
		Snapshot: Snapshot{
			Repository: RepositorySnapshot{
				Name:                   repositoryRecord.Name,
				CloneURL:               repositoryRecord.CloneURL,
				Branch:                 repositoryRecord.Branch,
				CheckoutCredentialName: cloneString(repositoryRecord.CheckoutCredentialName),
			},
			Target:    target,
			Action:    action,
			Runtime:   runtime,
			Placement: placementSnapshot(action.Placement, placementConfig),
			Inputs:    inputs,
		},
		CreatedAt: createdAt,
	}
	if provenance != nil {
		rerunOfRunID := provenance.runID
		rerunMode := provenance.mode
		record.RerunOfRunID = &rerunOfRunID
		record.RerunMode = &rerunMode
	}

	created, err := s.creation.CreateRun(ctx, record)
	if err != nil {
		return Run{}, fmt.Errorf("create run: %w", err)
	}

	return created, nil
}

func validateRequest(request CreateRequest) error {
	if request.TargetID == uuid.Nil {
		return fmt.Errorf("%w: Target ID is required", ErrInvalidRequest)
	}

	if strings.TrimSpace(request.ActionName) == "" {
		return fmt.Errorf("%w: action is required", ErrInvalidRequest)
	}

	if err := validateSource(request.Source); err != nil {
		return fmt.Errorf("%w: run source is invalid", ErrInvalidRequest)
	}
	if request.CatalogRevisionID != nil && *request.CatalogRevisionID == uuid.Nil {
		return fmt.Errorf("%w: catalog revision ID must not be empty", ErrInvalidRequest)
	}
	if err := validateSourceRequest(request); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}

	if _, err := actorSnapshot(request.Actor); err != nil {
		return err
	}

	if err := validateIdempotencyKey(request.IdempotencyKey); err != nil {
		return err
	}

	return nil
}

func invalidParameters(err error) error {
	return fmt.Errorf("%w: %w", ErrInvalidParameters, err)
}

func findTarget(targets []catalog.Target, name string) (catalog.Target, bool) {
	for _, target := range targets {
		if target.Metadata.Name == name {
			return target, true
		}
	}

	return catalog.Target{}, false
}

func findRuntime(runtimes []catalog.Runtime, name string) (catalog.Runtime, bool) {
	for _, runtime := range runtimes {
		if runtime.Metadata.Name == name {
			return runtime, true
		}
	}

	return catalog.Runtime{}, false
}

func placementSnapshot(name string, cfg placement.Config) PlacementSnapshot {
	credentials := make(map[string]CredentialSnapshot, len(cfg.Credentials))
	for credentialName, source := range cfg.Credentials {
		credentials[credentialName] = CredentialSnapshot{
			SecretName: source.SecretRef.Name,
		}
	}

	timeout := ""
	if cfg.Timeout > 0 {
		timeout = cfg.Timeout.String()
	}

	return PlacementSnapshot{
		Name:               name,
		ClusterName:        cfg.ClusterName,
		JobNamespace:       cfg.JobNamespace,
		ServiceAccountName: cfg.ServiceAccountName,
		Timeout:            timeout,
		Resources: catalog.ResourceRequirements{
			Requests: maps.Clone(cfg.Resources.Requests),
			Limits:   maps.Clone(cfg.Resources.Limits),
		},
		NodeSelector: cfg.NodeSelector,
		Credentials:  credentials,
		PodTemplate:  cfg.PodTemplate,
	}
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}

	cloned := *value

	return &cloned
}
