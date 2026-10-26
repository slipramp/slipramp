package run

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/catalog"
)

func (s *Service) resolveInputCandidates(
	ctx context.Context,
	request CreateRequest,
) (map[string]ArtifactInput, uuid.UUID, *string, error) {
	result := make(map[string]ArtifactInput, len(request.Inputs))
	var revisionID uuid.UUID
	var relevanceCommitSHA *string
	relevanceMarkerSet := false
	for _, name := range slices.Sorted(maps.Keys(request.Inputs)) {
		if strings.TrimSpace(name) == "" {
			return nil, uuid.Nil, nil, fmt.Errorf("%w: input name must not be blank", ErrInvalidInputs)
		}

		artifactID := request.Inputs[name]
		if artifactID == uuid.Nil {
			return nil, uuid.Nil, nil, fmt.Errorf("%w: input %q artifact ID is required", ErrInvalidInputs, name)
		}

		candidate, err := s.creation.GetArtifactInputCandidate(ctx, artifactID)
		if errors.Is(err, ErrArtifactNotFound) {
			return nil, uuid.Nil, nil, ErrArtifactNotFound
		}
		if err != nil {
			return nil, uuid.Nil, nil, fmt.Errorf("get artifact input %q: %w", name, err)
		}

		if candidate.TargetID != request.TargetID ||
			candidate.SHA256 == "" ||
			candidate.RetentionState != "retained" {
			return nil, uuid.Nil, nil, fmt.Errorf(
				"%w: artifact for input %q is not a retained, sealed artifact from this target",
				ErrInvalidInputs,
				name,
			)
		}

		if revisionID == uuid.Nil {
			revisionID = candidate.CatalogRevisionID
		} else if revisionID != candidate.CatalogRevisionID {
			return nil, uuid.Nil, nil, fmt.Errorf(
				"%w: all inputs must come from the same catalog revision",
				ErrInvalidInputs,
			)
		}

		if !relevanceMarkerSet {
			relevanceCommitSHA = cloneString(candidate.TargetRelevanceCommitSHA)
			relevanceMarkerSet = true
		} else if !sameOptionalString(relevanceCommitSHA, candidate.TargetRelevanceCommitSHA) {
			return nil, uuid.Nil, nil, fmt.Errorf(
				"%w: all inputs must inherit the same target relevance commit",
				ErrInvalidInputs,
			)
		}

		result[name] = candidate
	}

	return result, revisionID, relevanceCommitSHA, nil
}

func sameOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}

	return *left == *right
}

func validateArtifactInputs(action catalog.EffectiveAction, inputs map[string]ArtifactInput) error {
	if len(inputs) != len(action.Inputs) {
		return fmt.Errorf("%w: action requires %d explicit input bindings", ErrInvalidInputs, len(action.Inputs))
	}

	for _, name := range slices.Sorted(maps.Keys(action.Inputs)) {
		declaration := action.Inputs[name]
		input, found := inputs[name]
		if !found {
			return fmt.Errorf("%w: required input %q was not supplied", ErrInvalidInputs, name)
		}

		if input.Name != name || input.ActionName != declaration.Artifact.FromAction {
			return fmt.Errorf(
				"%w: input %q must select artifact %q from action %q",
				ErrInvalidInputs,
				name,
				name,
				declaration.Artifact.FromAction,
			)
		}
	}

	for name := range inputs {
		if _, declared := action.Inputs[name]; !declared {
			return fmt.Errorf("%w: input %q is not declared by the action", ErrInvalidInputs, name)
		}
	}

	return nil
}
