// Package inbox builds the role-aware operational Inbox read model.
package inbox

import (
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
)

const (
	KindPromotionReview    Kind = "promotion-review"
	KindRunReview          Kind = "run-review"
	KindPromotionError     Kind = "promotion-error"
	KindGitTriggerFailure  Kind = "git-trigger-failure"
	KindRepositoryRefresh  Kind = "repository-refresh"
	KindClusterUnreachable Kind = "cluster-unreachable"
	KindRunObservation     Kind = "run-observation"

	DestinationPromotion  DestinationType = "promotion"
	DestinationRepository DestinationType = "repository"
	DestinationCluster    DestinationType = "cluster"
	DestinationRun        DestinationType = "run"

	maximumTitleLength          = 200
	maximumDescriptionLength    = 4096
	maximumAcknowledgementBatch = 500
)

var (
	ErrInvalidAcknowledgement = errors.New("invalid Inbox acknowledgement")
	problemIDPattern          = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type Kind string

type DestinationType string

// Query contains the request-specific identity and visibility needed to shape
// an Inbox. Authentication and authorization remain at the API boundary.
type Query struct {
	PrincipalID               uuid.UUID
	IncludePromotionDecisions bool
	IncludeRunDecisions       bool
}

type Snapshot struct {
	GeneratedAt time.Time
	Decisions   []Item
	Problems    []Item
	Activity    Activity
}

type Activity struct {
	Queued   int
	Starting int
	Running  int
}

type Item struct {
	ID          string
	Kind        Kind
	Title       string
	Description string
	OccurredAt  time.Time
	ActionLabel string
	Destination Destination
}

type Destination struct {
	Type DestinationType
	ID   string
}

func validateProblemIDs(problemIDs []string) error {
	if len(problemIDs) == 0 || len(problemIDs) > maximumAcknowledgementBatch {
		return fmt.Errorf(
			"%w: batch must contain between 1 and %d problem IDs",
			ErrInvalidAcknowledgement,
			maximumAcknowledgementBatch,
		)
	}

	seen := make(map[string]struct{}, len(problemIDs))
	for _, problemID := range problemIDs {
		if !problemIDPattern.MatchString(problemID) {
			return fmt.Errorf(
				"%w: problem ID %q is not a lowercase SHA-256 digest",
				ErrInvalidAcknowledgement,
				problemID,
			)
		}
		if _, exists := seen[problemID]; exists {
			return fmt.Errorf("%w: problem ID %q is duplicated", ErrInvalidAcknowledgement, problemID)
		}
		seen[problemID] = struct{}{}
	}

	return nil
}
