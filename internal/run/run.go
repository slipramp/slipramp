// Package run owns immutable action execution records and their creation
// boundary.
package run

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/slipramp/slipramp/internal/catalog"
	corev1 "k8s.io/api/core/v1"
)

const (
	StateQueued    State = "queued"
	StatePreparing State = "preparing"
	StateRunning   State = "running"
	StateSucceeded State = "succeeded"
	StateFailed    State = "failed"
	StateCanceling State = "canceling"
	StateCanceled  State = "canceled"
	StateTimedOut  State = "timed-out"
	StateUnknown   State = "unknown"

	SourceTypeManual SourceType = "manual"
	SourceTypeAPI    SourceType = "api"
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
	ErrNoControllerWork    = errors.New("no controller work available")
	ErrIdempotencyConflict = errors.New("run idempotency key conflict")
)

type State string

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

type Run struct {
	ID                uuid.UUID
	State             State
	RepositoryID      uuid.UUID
	CatalogRevisionID uuid.UUID
	CommitSHA         string
	TargetName        string
	ActionName        string
	Parameters        map[string]any
	PlacementName     string
	ClusterName       string
	Source            Source
	IdempotencyKey    *string
	RequestDigest     string
	Snapshot          Snapshot
	CreatedAt         time.Time
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
