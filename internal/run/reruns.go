package run

import (
	"context"
	"errors"
	"fmt"
	"maps"

	"github.com/google/uuid"
)

type rerunProvenance struct {
	runID uuid.UUID
	mode  RerunMode
}

func (s *Service) RerunRun(ctx context.Context, request RerunRequest) (Run, error) {
	if err := validateRerunRequest(request); err != nil {
		return Run{}, err
	}

	original, err := s.history.GetRun(ctx, request.RunID)
	if err != nil {
		return Run{}, err
	}
	if original.Phase != PhaseFinished {
		return Run{}, ErrRunNotFinished
	}
	if request.Mode == RerunModeLatest && len(original.Snapshot.Inputs) > 0 {
		return Run{}, ErrLatestRerunInputs
	}

	provenance := &rerunProvenance{runID: original.ID, mode: request.Mode}
	createRequest := CreateRequest{
		TargetID:       original.TargetID,
		ActionName:     original.ActionName,
		Parameters:     maps.Clone(original.Parameters),
		Inputs:         map[string]uuid.UUID{},
		Source:         request.Source,
		IdempotencyKey: cloneString(request.IdempotencyKey),
		Actor:          request.Actor,
	}
	if request.Mode == RerunModeLatest {
		return s.createRun(ctx, createRequest, provenance)
	}

	for name, input := range original.Snapshot.Inputs {
		createRequest.Inputs[name] = input.ArtifactID
	}
	revisionID := original.CatalogRevisionID
	createRequest.CatalogRevisionID = &revisionID
	digest, err := requestDigest(createRequest, createRequest.Parameters, provenance)
	if err != nil {
		return Run{}, fmt.Errorf("digest rerun request: %w", err)
	}
	if request.IdempotencyKey != nil {
		existing, err := s.idempotentRun(ctx, request.Source, request.Actor, *request.IdempotencyKey, digest)
		if err == nil {
			return existing, nil
		}
		if !errors.Is(err, ErrRunNotFound) {
			return Run{}, err
		}
	}

	createdBy, err := actorSnapshot(request.Actor)
	if err != nil {
		return Run{}, err
	}
	runID := original.ID
	mode := request.Mode
	record := Run{
		ID:                s.newID(),
		Phase:             PhaseQueued,
		TargetID:          original.TargetID,
		RepositoryID:      original.RepositoryID,
		CatalogRevisionID: original.CatalogRevisionID,
		CommitSHA:         original.CommitSHA,
		TargetName:        original.TargetName,
		ActionName:        original.ActionName,
		Parameters:        maps.Clone(original.Parameters),
		PlacementName:     original.PlacementName,
		ClusterName:       original.ClusterName,
		Source:            request.Source,
		IdempotencyKey:    cloneString(request.IdempotencyKey),
		RequestDigest:     digest,
		RerunOfRunID:      &runID,
		RerunMode:         &mode,
		Snapshot:          original.Snapshot,
		CreatedBy:         createdBy,
		CreatedAt:         s.now().UTC(),
	}
	record.Snapshot.Inputs = maps.Clone(original.Snapshot.Inputs)

	created, err := s.creation.CreateRun(ctx, record)
	if errors.Is(err, ErrInvalidInputs) {
		return Run{}, ErrRerunInputsMissing
	}
	if err != nil {
		return Run{}, fmt.Errorf("create rerun: %w", err)
	}

	return created, nil
}

func validateRerunRequest(request RerunRequest) error {
	if request.RunID == uuid.Nil {
		return fmt.Errorf("%w: Run ID is required", ErrInvalidRequest)
	}
	if request.Mode != RerunModeExact && request.Mode != RerunModeLatest {
		return fmt.Errorf("%w: rerun mode is invalid", ErrInvalidRequest)
	}
	if request.Source.Type != SourceTypeManual && request.Source.Type != SourceTypeAPI {
		return fmt.Errorf("%w: reruns must have a manual or API source", ErrInvalidRequest)
	}
	if err := validateSource(request.Source); err != nil {
		return fmt.Errorf("%w: run source is invalid", ErrInvalidRequest)
	}
	if _, err := actorSnapshot(request.Actor); err != nil {
		return err
	}

	return validateIdempotencyKey(request.IdempotencyKey)
}
