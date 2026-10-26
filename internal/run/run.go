// Package run owns immutable action execution records and their creation
// boundary.
package run

import (
	"errors"
	"time"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/productidentity"
)

const (
	DefaultRunListLimit  = 50
	MaximumRunListLimit  = 100
	MaximumRunListOffset = 2_147_483_647

	PhaseQueued    Phase = "queued"
	PhaseAssigned  Phase = "assigned"
	PhasePreparing Phase = "preparing"
	PhaseRunning   Phase = "running"
	PhaseFinished  Phase = "finished"

	OutcomeSucceeded Outcome = "succeeded"
	OutcomeFailed    Outcome = "failed"
	OutcomeCanceled  Outcome = "canceled"
	OutcomeTimedOut  Outcome = "timed-out"

	SourceTypeManual    SourceType = "manual"
	SourceTypeAPI       SourceType = "api"
	SourceTypeGit       SourceType = "git"
	SourceTypePromotion SourceType = "promotion"

	RerunModeExact  RerunMode = "exact"
	RerunModeLatest RerunMode = "latest"

	ReviewStateNotRequired ReviewState = "not-required"
	ReviewStatePending     ReviewState = "pending"
	ReviewStateApproved    ReviewState = "approved"
	ReviewStateRejected    ReviewState = "rejected"

	ReviewDecisionApproved ReviewDecision = "approved"
	ReviewDecisionRejected ReviewDecision = "rejected"

	ReasonCancelRequested       = "CancelRequested"
	ReasonSupersededByGitChange = "SupersededByGitChange"
	ReasonReviewRejected        = "ReviewRejected"
)

var (
	ErrInvalidRequest      = errors.New("invalid run request")
	ErrInvalidParameters   = errors.New("invalid run parameters")
	ErrInvalidInputs       = errors.New("invalid run artifact inputs")
	ErrArtifactNotFound    = errors.New("run artifact input not found")
	ErrRepositoryNotFound  = errors.New("run repository not found")
	ErrCatalogNotFound     = errors.New("active run catalog not found")
	ErrTargetNotFound      = errors.New("run target not found")
	ErrActionNotFound      = errors.New("run action not found")
	ErrRuntimeNotFound     = errors.New("run runtime not found")
	ErrPlacementNotFound   = errors.New("run placement not found")
	ErrRunNotFound         = errors.New("run not found")
	ErrRunNotFinished      = errors.New("run is not finished")
	ErrLatestRerunInputs   = errors.New("latest rerun does not support artifact inputs")
	ErrRerunInputsMissing  = errors.New("rerun artifact inputs are no longer available")
	ErrRunFinished         = errors.New("run already finished")
	ErrNoRunAssignment     = errors.New("no run assignment available")
	ErrNoRunToCancel       = errors.New("no run to cancel")
	ErrObservationConflict = errors.New("run observation conflict")
	ErrReviewConflict      = errors.New("run review decision conflicts with current state")
	ErrIdempotencyConflict = errors.New("run idempotency key conflict")
	ErrGitRunSuperseded    = errors.New("git-triggered run superseded by a newer desired commit")
)

type Phase string

type Outcome string

type SourceType string

type RerunMode string

type ReviewState string

type ReviewDecision string

type ObservationState string

const (
	ObservationStateCurrent ObservationState = "current"
	ObservationStatePending ObservationState = "pending"
	ObservationStateUnknown ObservationState = "unknown"
)

type Source struct {
	Type      SourceType          `json:"type"`
	Git       *GitRunSource       `json:"git,omitempty"`
	Promotion *PromotionRunSource `json:"promotion,omitempty"`
}

type GitRunSource struct {
	TriggerName string    `json:"triggerName"`
	DeliveryID  uuid.UUID `json:"deliveryId"`
}

type PromotionRunSource struct {
	PromotionID uuid.UUID `json:"promotionId"`
}

type CreateRequest struct {
	TargetID   uuid.UUID
	ActionName string
	Parameters map[string]any
	Inputs     map[string]uuid.UUID
	// CatalogRevisionID pins internal automated requests to an immutable
	// accepted catalog revision. HTTP-created Runs leave it unset.
	CatalogRevisionID *uuid.UUID
	// ReviewRequired is an internal Git-delivery snapshot. Product Run creation
	// requests never set it.
	ReviewRequired bool
	Source         Source
	IdempotencyKey *string
	Actor          *productidentity.Principal
}

type CancelRequest struct {
	RunID uuid.UUID
	Actor *productidentity.Principal
}

type ReviewDecisionRequest struct {
	RunID    uuid.UUID
	Decision ReviewDecision
	Comment  *string
	Actor    *productidentity.Principal
}

type RerunRequest struct {
	RunID          uuid.UUID
	Mode           RerunMode
	Source         Source
	IdempotencyKey *string
	Actor          *productidentity.Principal
}

type RunListFilter struct {
	RepositoryID    *uuid.UUID
	TargetID        *uuid.UUID
	CommitSHA       string
	InputArtifactID *uuid.UUID
	ClusterName     string
	Phase           Phase
	ReviewState     ReviewState
	Limit           int32
	Offset          int32
}

type RunList struct {
	Items []Run
	Total int64
}

type Observation struct {
	ID          uuid.UUID
	ClusterName string
	Phase       Phase
	Outcome     *Outcome
	JobUID      *string
	Reason      *string
	Message     *string
	ExitCode    *int32
	ObservedAt  time.Time
}

type Run struct {
	ID                       uuid.UUID
	Phase                    Phase
	Outcome                  *Outcome
	TargetID                 uuid.UUID
	RepositoryID             uuid.UUID
	CatalogRevisionID        uuid.UUID
	CommitSHA                string
	TargetRelevanceCommitSHA *string
	TargetName               string
	ActionName               string
	Parameters               map[string]any
	PlacementName            string
	ClusterName              string
	Source                   Source
	IdempotencyKey           *string
	RequestDigest            string
	RerunOfRunID             *uuid.UUID
	RerunMode                *RerunMode
	Snapshot                 Snapshot
	CreatedBy                *productidentity.ActorSnapshot
	CanceledBy               *productidentity.ActorSnapshot
	Review                   Review
	CreatedAt                time.Time
	AssignedAt               *time.Time
	PreparingAt              *time.Time
	StartedAt                *time.Time
	FinishedAt               *time.Time
	LastObservedAt           *time.Time
	CancellationRequestedAt  *time.Time
	JobUID                   *string
	OutcomeReason            *string
	OutcomeMessage           *string
	ExitCode                 *int32
}

type Review struct {
	State     ReviewState
	DecidedBy *productidentity.ActorSnapshot
	DecidedAt *time.Time
	Comment   *string
}

type Snapshot struct {
	Repository RepositorySnapshot       `json:"repository"`
	Target     catalog.Target           `json:"target"`
	Action     catalog.EffectiveAction  `json:"action"`
	Runtime    catalog.Runtime          `json:"runtime"`
	Placement  PlacementSnapshot        `json:"placement"`
	Inputs     map[string]ArtifactInput `json:"inputs"`
}

type ArtifactInput struct {
	ArtifactID               uuid.UUID `json:"artifactId"`
	ProducerRunID            uuid.UUID `json:"producerRunId"`
	Name                     string    `json:"name"`
	MediaType                string    `json:"mediaType"`
	SizeBytes                int64     `json:"sizeBytes"`
	SHA256                   string    `json:"sha256"`
	Sensitive                bool      `json:"sensitive"`
	RetentionState           string    `json:"retentionState"`
	TargetID                 uuid.UUID `json:"targetId"`
	RepositoryID             uuid.UUID `json:"repositoryId"`
	CatalogRevisionID        uuid.UUID `json:"catalogRevisionId"`
	CommitSHA                string    `json:"commitSha"`
	TargetRelevanceCommitSHA *string   `json:"targetRelevanceCommitSha,omitempty"`
	TargetName               string    `json:"targetName"`
	ActionName               string    `json:"actionName"`
}

type RepositorySnapshot struct {
	Name                   string  `json:"name"`
	CloneURL               string  `json:"cloneUrl"`
	Branch                 string  `json:"branch"`
	CheckoutCredentialName *string `json:"checkoutCredentialName,omitempty"`
}

type PlacementSnapshot struct {
	Name               string                        `json:"name"`
	ClusterName        string                        `json:"cluster"`
	JobNamespace       string                        `json:"jobNamespace"`
	ServiceAccountName string                        `json:"serviceAccountName,omitempty"`
	Timeout            string                        `json:"timeout,omitempty"`
	Resources          catalog.ResourceRequirements  `json:"resources,omitzero"`
	NodeSelector       map[string]string             `json:"nodeSelector,omitempty"`
	Credentials        map[string]CredentialSnapshot `json:"credentials,omitempty"`
	PodTemplate        *corev1.PodTemplateSpec       `json:"podTemplate,omitempty"`
}

type CredentialSnapshot struct {
	SecretName string `json:"secretName"`
}
