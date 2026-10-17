// Package run owns immutable action execution records and their creation
// boundary.
package run

import (
	"errors"
	"time"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"

	"github.com/slipramp/slipramp/internal/catalog"
)

const (
	PhaseQueued    Phase = "queued"
	PhaseAssigned  Phase = "assigned"
	PhasePreparing Phase = "preparing"
	PhaseRunning   Phase = "running"
	PhaseFinished  Phase = "finished"

	OutcomeSucceeded Outcome = "succeeded"
	OutcomeFailed    Outcome = "failed"
	OutcomeCanceled  Outcome = "canceled"
	OutcomeTimedOut  Outcome = "timed-out"

	SourceTypeManual SourceType = "manual"
	SourceTypeAPI    SourceType = "api"

	ReasonCancelRequested = "CancelRequested"
)

var (
	ErrInvalidRequest      = errors.New("invalid run request")
	ErrInvalidParameters   = errors.New("invalid run parameters")
	ErrRepositoryNotFound  = errors.New("run repository not found")
	ErrCatalogNotFound     = errors.New("active run catalog not found")
	ErrTargetNotFound      = errors.New("run target not found")
	ErrActionNotFound      = errors.New("run action not found")
	ErrRuntimeNotFound     = errors.New("run runtime not found")
	ErrPlacementNotFound   = errors.New("run placement not found")
	ErrRunNotFound         = errors.New("run not found")
	ErrRunFinished         = errors.New("run already finished")
	ErrNoRunAssignment     = errors.New("no run assignment available")
	ErrNoRunToCancel       = errors.New("no run to cancel")
	ErrObservationConflict = errors.New("run observation conflict")
	ErrIdempotencyConflict = errors.New("run idempotency key conflict")
)

type Phase string

type Outcome string

type SourceType string

type Source struct {
	Type SourceType `json:"type"`
}

type CreateRequest struct {
	RepositoryID   uuid.UUID
	TargetName     string
	ActionName     string
	Parameters     map[string]any
	Source         Source
	IdempotencyKey *string
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
	ID                      uuid.UUID
	Phase                   Phase
	Outcome                 *Outcome
	RepositoryID            uuid.UUID
	CatalogRevisionID       uuid.UUID
	CommitSHA               string
	TargetName              string
	ActionName              string
	Parameters              map[string]any
	PlacementName           string
	ClusterName             string
	Source                  Source
	IdempotencyKey          *string
	RequestDigest           string
	Snapshot                Snapshot
	CreatedAt               time.Time
	AssignedAt              *time.Time
	PreparingAt             *time.Time
	StartedAt               *time.Time
	FinishedAt              *time.Time
	LastObservedAt          *time.Time
	CancellationRequestedAt *time.Time
	JobUID                  *string
	OutcomeReason           *string
	OutcomeMessage          *string
	ExitCode                *int32
}

type Snapshot struct {
	Repository RepositorySnapshot      `json:"repository"`
	Target     catalog.Target          `json:"target"`
	Action     catalog.EffectiveAction `json:"action"`
	Runtime    catalog.Runtime         `json:"runtime"`
	Placement  PlacementSnapshot       `json:"placement"`
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
