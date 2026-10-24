// Package promotion owns Kargo promotion-review state and validation without
// depending on HTTP or Kubernetes representations.
package promotion

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/kubevalidate"
	"github.com/slipramp/slipramp/internal/productidentity"
)

const (
	PolicyModeAutomatic      PolicyMode = "automatic"
	PolicyModeReviewRequired PolicyMode = "review-required"

	SourceKargo    PromotionSource = "kargo"
	SourceSlipramp PromotionSource = "slipramp"

	StatusCreating  PromotionStatus = "creating"
	StatusPreparing PromotionStatus = "preparing"
	StatusPending   PromotionStatus = "pending"
	StatusApproved  PromotionStatus = "approved"
	StatusRejected  PromotionStatus = "rejected"
	StatusExpired   PromotionStatus = "expired"
	StatusError     PromotionStatus = "error"

	DecisionApproved DecisionValue = "approved"
	DecisionRejected DecisionValue = "rejected"

	maximumTextLength          = 200
	maximumURLLength           = 2048
	maximumBranchLength        = 255
	maximumFreightOriginLength = 512
	maximumSourcePathLength    = 1024
	maximumMessageLength       = 4096
)

var (
	ErrInvalid                    = errors.New("invalid promotion")
	ErrStageNotFound              = errors.New("promotion Stage not found")
	ErrPolicyNotFound             = errors.New("promotion policy not found")
	ErrIntegrationNotFound        = errors.New("kargo integration not found")
	ErrIntegrationCluster         = errors.New("kargo integration belongs to another cluster")
	ErrRepositoryNotAllowed       = errors.New("kargo Stage repository is not allowed by the integration")
	ErrStageArchived              = errors.New("promotion Stage is archived")
	ErrStageStale                 = errors.New("promotion Stage observation is stale")
	ErrRepositoryUnavailable      = errors.New("promotion repository is unavailable")
	ErrSnapshotConflict           = errors.New("kargo Stage snapshot identity conflicts with durable state")
	ErrStaleSnapshot              = errors.New("kargo Stage snapshot is stale")
	ErrPromotionNotFound          = errors.New("promotion not found")
	ErrNoPreparationWork          = errors.New("no Promotion preparation work is available")
	ErrIdentityConflict           = errors.New("external Promotion identity already exists")
	ErrTransitionConflict         = errors.New("promotion state transition conflicts with current state")
	ErrDecisionConflict           = errors.New("promotion already has a different decision")
	ErrDecisionNotFound           = errors.New("promotion decision not found")
	ErrInvalidPreparationArtifact = errors.New("promotion preparation artifact is invalid")
	ErrFreightNotFound            = errors.New("promotion Freight was not found on the source Stage")
	ErrFreightSource              = errors.New("promotion Freight source is not accepted by the target Stage")
	ErrStageScope                 = errors.New("promotion source and target Stages must share integration, cluster, and project")
)

type PolicyMode string

type PromotionSource string

type PromotionStatus string

type DecisionValue string

type RequestDigest [sha256.Size]byte

type Stage struct {
	ID                              uuid.UUID
	Integration                     string
	ClusterName                     string
	Project                         string
	Name                            string
	KubernetesUID                   string
	ResourceVersion                 string
	Application                     string
	Environment                     string
	RepositoryName                  string
	RepositoryURL                   string
	TargetBranch                    string
	SourcePaths                     []string
	RequestedFreight                []FreightRequest
	Freight                         json.RawMessage
	SuccessfulRenderAcknowledgement json.RawMessage
	FirstObservedAt                 time.Time
	LastObservedAt                  time.Time
	ArchivedAt                      *time.Time
}

type FreightRequest struct {
	Origin       string   `json:"origin"`
	Direct       bool     `json:"direct"`
	SourceStages []string `json:"sourceStages"`
}

type StagePolicy struct {
	StageID              uuid.UUID
	Mode                 PolicyMode
	ExpiresAt            *time.Time
	Reason               *string
	UpdatedByPrincipalID uuid.UUID
	UpdatedBySnapshot    productidentity.ActorSnapshot
	UpdatedAt            time.Time
}

type StageSnapshot struct {
	SnapshotID  uuid.UUID
	Integration string
	ClusterName string
	ObservedAt  time.Time
	AcceptedAt  time.Time
}

type Promotion struct {
	ID                           uuid.UUID
	Source                       PromotionSource
	Integration                  string
	ExternalPromotionID          string
	RequestDigest                RequestDigest
	StageID                      uuid.UUID
	SourceStageID                *uuid.UUID
	StageSnapshot                json.RawMessage
	RepositoryID                 uuid.UUID
	RepositorySnapshot           json.RawMessage
	BaseRevision                 string
	CandidateRevision            string
	CandidateBranch              string
	TargetBranch                 string
	PolicySnapshot               json.RawMessage
	SelectedFreightSnapshot      json.RawMessage
	ReviewRequired               bool
	Status                       PromotionStatus
	Message                      *string
	DispatchAttemptCount         int
	NextDispatchAt               *time.Time
	LastDispatchAt               *time.Time
	DispatchedAt                 *time.Time
	KargoPromotionUID            *string
	PreparationRepositoryID      *uuid.UUID
	PreparationCatalogRevisionID *uuid.UUID
	PreparationRunID             *uuid.UUID
	GitDiffArtifactID            *uuid.UUID
	ArgoDiffArtifactID           *uuid.UUID
	RequestedByPrincipalID       uuid.UUID
	RequestedBySnapshot          productidentity.ActorSnapshot
	CreatedAt                    time.Time
	PreparationCompletedAt       *time.Time
	DecidedAt                    *time.Time
	ExpiresAt                    *time.Time
	UpdatedAt                    time.Time
}

type Decision struct {
	PromotionID          uuid.UUID
	Decision             DecisionValue
	Comment              *string
	DecidedByPrincipalID uuid.UUID
	DecidedBySnapshot    productidentity.ActorSnapshot
	DecidedAt            time.Time
}

func (m PolicyMode) Valid() bool {
	return m == PolicyModeAutomatic || m == PolicyModeReviewRequired
}

func (m PolicyMode) Effective(expiresAt *time.Time, at time.Time) PolicyMode {
	if m == PolicyModeReviewRequired && expiresAt != nil && !at.Before(*expiresAt) {
		return PolicyModeAutomatic
	}

	return m
}

func (s PromotionSource) Valid() bool {
	return s == SourceKargo || s == SourceSlipramp
}

func (s PromotionStatus) Valid() bool {
	switch s {
	case StatusCreating, StatusPreparing, StatusPending, StatusApproved, StatusRejected, StatusExpired, StatusError:
		return true
	default:
		return false
	}
}

func (s PromotionStatus) Terminal() bool {
	return s == StatusApproved || s == StatusRejected || s == StatusExpired || s == StatusError
}

func (d DecisionValue) Valid() bool {
	return d == DecisionApproved || d == DecisionRejected
}

func (s Stage) Validate() error {
	if s.ID == uuid.Nil {
		return fmt.Errorf("%w: Stage ID is required", ErrInvalid)
	}
	if err := kubevalidate.DNSLabel("Kargo integration name", s.Integration); err != nil {
		return invalid(err)
	}
	if err := kubevalidate.DNSLabel("cluster name", s.ClusterName); err != nil {
		return invalid(err)
	}
	if err := kubevalidate.DNSLabel("Kargo project", s.Project); err != nil {
		return invalid(err)
	}
	if err := kubevalidate.DNSSubdomain("Kargo Stage name", s.Name); err != nil {
		return invalid(err)
	}
	for label, value := range map[string]string{
		"Kubernetes UID":   s.KubernetesUID,
		"resource version": s.ResourceVersion,
		"application":      s.Application,
		"environment":      s.Environment,
		"repository URL":   s.RepositoryURL,
		"target branch":    s.TargetBranch,
	} {
		maximum := maximumTextLength
		if label == "repository URL" {
			maximum = maximumURLLength
		}
		if label == "target branch" {
			maximum = maximumBranchLength
		}
		if err := validateText(label, value, maximum); err != nil {
			return err
		}
	}
	if err := kubevalidate.DNSLabel("repository name", s.RepositoryName); err != nil {
		return invalid(err)
	}
	if len(s.SourcePaths) == 0 {
		return fmt.Errorf("%w: at least one source path is required", ErrInvalid)
	}
	seenFreightOrigins := make(map[string]struct{}, len(s.RequestedFreight))
	for _, request := range s.RequestedFreight {
		if err := validateText("requested Freight origin", request.Origin, maximumFreightOriginLength); err != nil {
			return err
		}
		if _, ok := seenFreightOrigins[request.Origin]; ok {
			return fmt.Errorf("%w: requested Freight origin %q is duplicated", ErrInvalid, request.Origin)
		}
		seenFreightOrigins[request.Origin] = struct{}{}
		seenSources := make(map[string]struct{}, len(request.SourceStages))
		for _, source := range request.SourceStages {
			if err := kubevalidate.DNSSubdomain("requested Freight source Stage", source); err != nil {
				return invalid(err)
			}
			if _, ok := seenSources[source]; ok {
				return fmt.Errorf("%w: requested Freight source Stage %q is duplicated", ErrInvalid, source)
			}
			seenSources[source] = struct{}{}
		}
	}
	seenPaths := make(map[string]struct{}, len(s.SourcePaths))
	for _, path := range s.SourcePaths {
		if err := validateText("source path", path, maximumSourcePathLength); err != nil {
			return err
		}
		if _, ok := seenPaths[path]; ok {
			return fmt.Errorf("%w: source path %q is duplicated", ErrInvalid, path)
		}
		seenPaths[path] = struct{}{}
	}
	if !validJSONObject(s.Freight) {
		return fmt.Errorf("%w: Freight must be a JSON object", ErrInvalid)
	}
	if len(s.SuccessfulRenderAcknowledgement) > 0 &&
		!validJSONObject(s.SuccessfulRenderAcknowledgement) {
		return fmt.Errorf("%w: successful-render acknowledgement must be a JSON object", ErrInvalid)
	}
	if s.FirstObservedAt.IsZero() || s.LastObservedAt.IsZero() || s.LastObservedAt.Before(s.FirstObservedAt) {
		return fmt.Errorf("%w: Stage observation timestamps are invalid", ErrInvalid)
	}
	if s.ArchivedAt != nil && s.ArchivedAt.Before(s.LastObservedAt) {
		return fmt.Errorf("%w: Stage archive time precedes its last observation", ErrInvalid)
	}

	return nil
}

func (p StagePolicy) Validate() error {
	if p.StageID == uuid.Nil || p.UpdatedByPrincipalID == uuid.Nil {
		return fmt.Errorf("%w: policy Stage and principal IDs are required", ErrInvalid)
	}
	if !p.Mode.Valid() {
		return fmt.Errorf("%w: policy mode is invalid", ErrInvalid)
	}
	if p.Mode == PolicyModeAutomatic && p.ExpiresAt != nil {
		return fmt.Errorf("%w: automatic policy cannot expire", ErrInvalid)
	}
	if p.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: policy update time is required", ErrInvalid)
	}
	if p.ExpiresAt != nil && !p.ExpiresAt.After(p.UpdatedAt) {
		return fmt.Errorf("%w: policy expiry must follow its update", ErrInvalid)
	}
	if p.Reason != nil {
		if err := validateText("policy reason", *p.Reason, maximumMessageLength); err != nil {
			return err
		}
	}
	if p.UpdatedBySnapshot.PrincipalID != p.UpdatedByPrincipalID {
		return fmt.Errorf("%w: policy actor snapshot does not match its principal", ErrInvalid)
	}
	if err := p.UpdatedBySnapshot.Validate(); err != nil {
		return invalid(err)
	}

	return nil
}

func (s StageSnapshot) Validate() error {
	if s.SnapshotID == uuid.Nil {
		return fmt.Errorf("%w: Stage snapshot ID is required", ErrInvalid)
	}
	if err := kubevalidate.DNSLabel("Kargo integration name", s.Integration); err != nil {
		return invalid(err)
	}
	if err := kubevalidate.DNSLabel("cluster name", s.ClusterName); err != nil {
		return invalid(err)
	}
	if s.ObservedAt.IsZero() || s.AcceptedAt.IsZero() || s.AcceptedAt.Before(s.ObservedAt) {
		return fmt.Errorf("%w: Stage snapshot timestamps are invalid", ErrInvalid)
	}

	return nil
}

func (p Promotion) Validate() error {
	if p.ID == uuid.Nil || p.StageID == uuid.Nil || p.RepositoryID == uuid.Nil ||
		p.RequestedByPrincipalID == uuid.Nil {
		return fmt.Errorf("%w: Promotion, Stage, Repository, and principal IDs are required", ErrInvalid)
	}
	if err := kubevalidate.DNSLabel("Kargo integration name", p.Integration); err != nil {
		return invalid(err)
	}
	if !p.Source.Valid() {
		return fmt.Errorf("%w: Promotion source is invalid", ErrInvalid)
	}
	if err := validateText("external Promotion ID", p.ExternalPromotionID, maximumTextLength); err != nil {
		return err
	}
	if !validJSONObject(p.StageSnapshot) || !validJSONObject(p.RepositorySnapshot) ||
		!validJSONObject(p.PolicySnapshot) {
		return fmt.Errorf("%w: Promotion snapshots must be JSON objects", ErrInvalid)
	}
	hydrated := p.RequestDigest != (RequestDigest{}) || p.BaseRevision != "" ||
		p.CandidateRevision != "" || p.CandidateBranch != ""
	if hydrated {
		if p.RequestDigest == (RequestDigest{}) {
			return fmt.Errorf("%w: hydrated Promotion request digest is required", ErrInvalid)
		}
		if err := validateRevision("base revision", p.BaseRevision); err != nil {
			return err
		}
		if err := validateRevision("candidate revision", p.CandidateRevision); err != nil {
			return err
		}
		if err := validateText("candidate branch", p.CandidateBranch, maximumBranchLength); err != nil {
			return err
		}
		if !strings.HasPrefix(p.CandidateBranch, "kargo/promotion/") {
			return fmt.Errorf("%w: candidate branch must be under kargo/promotion/", ErrInvalid)
		}
	} else if p.Source != SourceSlipramp || (p.Status != StatusCreating && p.Status != StatusError) {
		return fmt.Errorf("%w: Promotion callback data is required", ErrInvalid)
	}
	if err := validateText("target branch", p.TargetBranch, maximumBranchLength); err != nil {
		return err
	}
	if !p.Status.Valid() {
		return fmt.Errorf("%w: Promotion status is invalid", ErrInvalid)
	}
	if !p.ReviewRequired && p.Status != StatusApproved && p.Status != StatusCreating && p.Status != StatusError {
		return fmt.Errorf("%w: automatic Promotion must be approved", ErrInvalid)
	}
	if !p.ReviewRequired && p.Status != StatusCreating && p.Status != StatusError &&
		(p.PreparationRepositoryID != nil || p.PreparationCatalogRevisionID != nil ||
			p.PreparationRunID != nil || p.PreparationCompletedAt != nil ||
			p.GitDiffArtifactID != nil || p.ArgoDiffArtifactID != nil || p.DecidedAt != nil) {
		return fmt.Errorf("%w: automatic Promotion cannot have preparation or decision state", ErrInvalid)
	}
	if p.Source == SourceKargo {
		if p.SourceStageID != nil || len(p.SelectedFreightSnapshot) != 0 || p.DispatchAttemptCount != 0 ||
			p.NextDispatchAt != nil || p.LastDispatchAt != nil || p.DispatchedAt != nil || p.KargoPromotionUID != nil {
			return fmt.Errorf("%w: Kargo-originated Promotion cannot have creation dispatch state", ErrInvalid)
		}
	} else {
		if p.SourceStageID == nil || !validJSONObject(p.SelectedFreightSnapshot) {
			return fmt.Errorf("%w: Slipramp-originated Promotion requires source Stage and Freight snapshots", ErrInvalid)
		}
		if p.Status == StatusCreating && hydrated {
			return fmt.Errorf("%w: creating Promotion cannot have callback data", ErrInvalid)
		}
		if p.Status == StatusCreating && p.NextDispatchAt == nil {
			return fmt.Errorf("%w: creating Promotion requires its next dispatch time", ErrInvalid)
		}
	}
	if p.DispatchAttemptCount < 0 {
		return fmt.Errorf("%w: Promotion dispatch attempt count cannot be negative", ErrInvalid)
	}
	if p.KargoPromotionUID != nil {
		if err := validateText("Kargo Promotion UID", *p.KargoPromotionUID, maximumTextLength); err != nil {
			return err
		}
		if p.DispatchedAt == nil {
			return fmt.Errorf("%w: Kargo Promotion UID requires successful dispatch time", ErrInvalid)
		}
	}
	if p.Status == StatusCreating && (p.PreparationRepositoryID != nil ||
		p.PreparationCatalogRevisionID != nil || p.PreparationRunID != nil ||
		p.PreparationCompletedAt != nil || p.GitDiffArtifactID != nil ||
		p.ArgoDiffArtifactID != nil || p.DecidedAt != nil) {
		return fmt.Errorf("%w: creating Promotion cannot have preparation or decision state", ErrInvalid)
	}
	if p.Status == StatusPreparing && (p.PreparationCompletedAt != nil || p.GitDiffArtifactID != nil ||
		p.ArgoDiffArtifactID != nil || p.DecidedAt != nil) {
		return fmt.Errorf("%w: preparing Promotion cannot have completed preparation state", ErrInvalid)
	}
	if (p.PreparationRepositoryID == nil) != (p.PreparationCatalogRevisionID == nil) {
		return fmt.Errorf("%w: preparation Repository and catalog revision must be reserved together", ErrInvalid)
	}
	if p.PreparationRunID != nil && p.PreparationCatalogRevisionID == nil {
		return fmt.Errorf("%w: preparation Run requires a reserved catalog revision", ErrInvalid)
	}
	if p.ReviewRequired &&
		(p.Status == StatusPending || p.Status == StatusApproved || p.Status == StatusRejected ||
			p.Status == StatusExpired) &&
		(p.PreparationRepositoryID == nil || p.PreparationCatalogRevisionID == nil ||
			p.PreparationRunID == nil || p.PreparationCompletedAt == nil ||
			p.GitDiffArtifactID == nil || p.ArgoDiffArtifactID == nil) {
		return fmt.Errorf("%w: prepared Promotion requires its Run and artifacts", ErrInvalid)
	}
	if p.ReviewRequired && (p.Status == StatusApproved || p.Status == StatusRejected) && p.DecidedAt == nil {
		return fmt.Errorf("%w: decided Promotion requires a decision time", ErrInvalid)
	}
	if p.Status != StatusApproved && p.Status != StatusRejected && p.DecidedAt != nil {
		return fmt.Errorf("%w: undecided Promotion cannot have a decision time", ErrInvalid)
	}
	if p.Message != nil {
		if err := validateText("Promotion message", *p.Message, maximumMessageLength); err != nil {
			return err
		}
	}
	if p.RequestedBySnapshot.PrincipalID != p.RequestedByPrincipalID {
		return fmt.Errorf("%w: requesting actor snapshot does not match its principal", ErrInvalid)
	}
	if err := p.RequestedBySnapshot.Validate(); err != nil {
		return invalid(err)
	}
	if p.CreatedAt.IsZero() || p.UpdatedAt.Before(p.CreatedAt) {
		return fmt.Errorf("%w: Promotion timestamps are invalid", ErrInvalid)
	}
	for label, value := range map[string]*time.Time{
		"next dispatch": p.NextDispatchAt,
		"last dispatch": p.LastDispatchAt,
		"dispatched":    p.DispatchedAt,
	} {
		if value != nil && value.Before(p.CreatedAt) {
			return fmt.Errorf("%w: Promotion %s time precedes creation", ErrInvalid, label)
		}
	}
	if p.ExpiresAt != nil && !p.ExpiresAt.After(p.CreatedAt) {
		return fmt.Errorf("%w: Promotion expiry must follow creation", ErrInvalid)
	}

	return nil
}

func (d Decision) Validate() error {
	if d.PromotionID == uuid.Nil || d.DecidedByPrincipalID == uuid.Nil {
		return fmt.Errorf("%w: decision Promotion and principal IDs are required", ErrInvalid)
	}
	if !d.Decision.Valid() {
		return fmt.Errorf("%w: decision value is invalid", ErrInvalid)
	}
	if d.Comment != nil {
		if err := validateText("decision comment", *d.Comment, maximumMessageLength); err != nil {
			return err
		}
	}
	if d.DecidedBySnapshot.PrincipalID != d.DecidedByPrincipalID {
		return fmt.Errorf("%w: deciding actor snapshot does not match its principal", ErrInvalid)
	}
	if err := d.DecidedBySnapshot.Validate(); err != nil {
		return invalid(err)
	}
	if d.DecidedAt.IsZero() {
		return fmt.Errorf("%w: decision time is required", ErrInvalid)
	}

	return nil
}

func invalid(err error) error {
	return fmt.Errorf("%w: %w", ErrInvalid, err)
}

func validateText(label, value string, maximumLength int) error {
	if strings.TrimSpace(value) == "" || value != strings.TrimSpace(value) {
		return fmt.Errorf("%w: %s must not be blank or have surrounding whitespace", ErrInvalid, label)
	}
	if utf8.RuneCountInString(value) > maximumLength {
		return fmt.Errorf("%w: %s must not exceed %d characters", ErrInvalid, label, maximumLength)
	}
	if strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return fmt.Errorf("%w: %s must not contain control characters", ErrInvalid, label)
	}

	return nil
}

func validateRevision(label, revision string) error {
	if len(revision) != 40 && len(revision) != 64 {
		return fmt.Errorf("%w: %s must be a full Git object ID", ErrInvalid, label)
	}
	for _, character := range revision {
		if character < '0' || character > '9' {
			if character < 'a' || character > 'f' {
				return fmt.Errorf("%w: %s must be a lowercase full Git object ID", ErrInvalid, label)
			}
		}
	}

	return nil
}

func validJSONObject(value json.RawMessage) bool {
	if len(value) == 0 || !json.Valid(value) {
		return false
	}

	var object map[string]json.RawMessage
	return json.Unmarshal(value, &object) == nil && object != nil
}
