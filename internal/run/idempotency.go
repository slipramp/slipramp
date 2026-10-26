package run

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/productidentity"
)

const maximumIdempotencyKeyLength = 128

func (s *Service) idempotentRun(
	ctx context.Context,
	source Source,
	actor *productidentity.Principal,
	idempotencyKey string,
	digest string,
) (Run, error) {
	existing, err := s.creation.GetRunByIdempotencyKey(
		ctx,
		actorPrincipalID(actor),
		source.Type,
		idempotencyKey,
	)
	if err != nil {
		if errors.Is(err, ErrRunNotFound) {
			return Run{}, ErrRunNotFound
		}

		return Run{}, fmt.Errorf("get idempotent run: %w", err)
	}
	if existing.RequestDigest != digest {
		return Run{}, ErrIdempotencyConflict
	}

	return existing, nil
}

func validateIdempotencyKey(idempotencyKey *string) error {
	if idempotencyKey == nil {
		return nil
	}

	key := *idempotencyKey
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("%w: idempotency key must not be blank", ErrInvalidRequest)
	}
	if len(key) > maximumIdempotencyKeyLength {
		return fmt.Errorf(
			"%w: idempotency key must not exceed %d characters",
			ErrInvalidRequest,
			maximumIdempotencyKeyLength,
		)
	}
	if strings.IndexFunc(key, unicode.IsControl) >= 0 {
		return fmt.Errorf("%w: idempotency key must not contain control characters", ErrInvalidRequest)
	}

	return nil
}

func requestDigest(
	request CreateRequest,
	parameters map[string]any,
	provenance *rerunProvenance,
) (string, error) {
	inputs := request.Inputs
	if inputs == nil {
		inputs = map[string]uuid.UUID{}
	}
	payload := struct {
		TargetID          uuid.UUID            `json:"targetId"`
		CatalogRevisionID *uuid.UUID           `json:"catalogRevisionId,omitempty"`
		Action            string               `json:"action"`
		Parameters        map[string]any       `json:"parameters"`
		Inputs            map[string]uuid.UUID `json:"inputs"`
		Source            Source               `json:"source"`
		ReviewRequired    bool                 `json:"reviewRequired"`
		RerunOfRunID      *uuid.UUID           `json:"rerunOfRunId,omitempty"`
		RerunMode         *RerunMode           `json:"rerunMode,omitempty"`
	}{
		TargetID:          request.TargetID,
		CatalogRevisionID: request.CatalogRevisionID,
		Action:            request.ActionName,
		Parameters:        parameters,
		Inputs:            inputs,
		Source:            request.Source,
		ReviewRequired:    request.ReviewRequired,
	}
	if provenance != nil {
		payload.RerunOfRunID = &provenance.runID
		payload.RerunMode = &provenance.mode
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	digest := sha256.Sum256(encoded)

	return hex.EncodeToString(digest[:]), nil
}
