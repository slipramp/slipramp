// Package artifact owns durable Run output metadata.
package artifact

import (
	"errors"
	"fmt"
	"math"
	"mime"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/kubevalidate"
)

const (
	KindLog    Kind = "log"
	KindOutput Kind = "output"

	StateOpen   State = "open"
	StateSealed State = "sealed"
	StateFailed State = "failed"

	RetentionRetained RetentionState = "retained"
	RetentionExpired  RetentionState = "expired"
	RetentionDeleted  RetentionState = "deleted"
)

var (
	ErrInvalid       = errors.New("invalid artifact")
	ErrNotFound      = errors.New("artifact not found")
	ErrAlreadyExists = errors.New("artifact already exists")
	ErrChunkConflict = errors.New("log chunk conflicts with stored contents")
	ErrChunkTooLarge = errors.New("log chunk exceeds the maximum size")
	ErrLogClosed     = errors.New("log artifact is closed")
	ErrLogFailed     = errors.New("log artifact failed before completion")
	ErrNotSealed     = errors.New("artifact is not sealed")
	ErrSealConflict  = errors.New("log seal conflicts with durable contents")

	sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type Kind string

type State string

type RetentionState string

type Artifact struct {
	ID             uuid.UUID
	RunID          uuid.UUID
	Name           string
	Kind           Kind
	State          State
	MediaType      string
	SizeBytes      int64
	SHA256         *string
	Sensitive      bool
	ObjectKey      *string
	RetentionState RetentionState
	CreatedAt      time.Time
	SealedAt       *time.Time
}

type Provenance struct {
	ProducerRunID     uuid.UUID
	RepositoryID      uuid.UUID
	CatalogRevisionID uuid.UUID
	CommitSHA         string
	TargetName        string
	ActionName        string
}

type Record struct {
	Artifact
	Provenance Provenance
}

type Declaration struct {
	MediaType string
	Sensitive bool
}

type RunContext struct {
	ClusterName string
	Artifacts   map[string]Declaration
}

func (a Artifact) Validate() error {
	if a.ID == uuid.Nil {
		return invalid("ID is required")
	}

	if a.RunID == uuid.Nil {
		return invalid("Run ID is required")
	}

	if err := kubevalidate.DNSLabel("artifact name", a.Name); err != nil {
		return invalid("%s", err)
	}

	switch a.Kind {
	case KindLog, KindOutput:
	default:
		return invalid("kind %q is not supported", a.Kind)
	}

	switch a.State {
	case StateOpen, StateSealed, StateFailed:
	default:
		return invalid("state %q is not supported", a.State)
	}

	if _, _, err := mime.ParseMediaType(a.MediaType); err != nil {
		return invalid("media type: %v", err)
	}

	if a.SizeBytes < 0 {
		return invalid("size must not be negative")
	}

	if a.SHA256 != nil && !sha256Pattern.MatchString(*a.SHA256) {
		return invalid("SHA-256 digest must be 64 lowercase hexadecimal characters")
	}

	if err := validateObjectKey(a.ObjectKey); err != nil {
		return err
	}

	switch a.RetentionState {
	case RetentionRetained, RetentionExpired, RetentionDeleted:
	default:
		return invalid("retention state %q is not supported", a.RetentionState)
	}

	if a.CreatedAt.IsZero() {
		return invalid("creation time is required")
	}

	if a.State == StateSealed {
		if a.SHA256 == nil {
			return invalid("sealed artifact requires a SHA-256 digest")
		}
		if a.SealedAt == nil {
			return invalid("sealed artifact requires a seal time")
		}
		if a.SealedAt.Before(a.CreatedAt) {
			return invalid("seal time must not precede creation time")
		}
	} else if a.SHA256 != nil || a.SealedAt != nil {
		return invalid("only a sealed artifact may have a digest or seal time")
	}

	return nil
}

type LogChunk struct {
	ArtifactID  uuid.UUID
	OffsetBytes int64
	SizeBytes   int64
	SHA256      string
	ObjectKey   string
	CreatedAt   time.Time
}

func (c LogChunk) Validate() error {
	if c.ArtifactID == uuid.Nil {
		return invalid("artifact ID is required")
	}

	if c.OffsetBytes < 0 {
		return invalid("chunk offset must not be negative")
	}

	if c.SizeBytes <= 0 {
		return invalid("chunk size must be positive")
	}

	if c.OffsetBytes > math.MaxInt64-c.SizeBytes {
		return invalid("chunk end offset exceeds the supported range")
	}

	if !sha256Pattern.MatchString(c.SHA256) {
		return invalid("chunk SHA-256 digest must be 64 lowercase hexadecimal characters")
	}

	if err := validateObjectKey(&c.ObjectKey); err != nil {
		return err
	}

	if c.CreatedAt.IsZero() {
		return invalid("chunk creation time is required")
	}

	return nil
}

func (c LogChunk) EndOffset() int64 {
	return c.OffsetBytes + c.SizeBytes
}

func validateObjectKey(value *string) error {
	if value == nil {
		return nil
	}

	if strings.TrimSpace(*value) == "" {
		return invalid("object key must not be blank")
	}

	if strings.HasPrefix(*value, "/") {
		return invalid("object key must be relative")
	}

	return nil
}

func invalid(format string, arguments ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, arguments...))
}
