// Package promotion owns Kargo promotion-review state and validation without
// depending on HTTP or Kubernetes representations.
package promotion

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/kubevalidate"
	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/repository"
)

const (
	PolicyModeAutomatic      PolicyMode = "automatic"
	PolicyModeReviewRequired PolicyMode = "review-required"

	SourceKargo    PromotionSource = "kargo"
	SourceSlipramp PromotionSource = "slipramp"

	DecisionApproved DecisionValue = "approved"
	DecisionRejected DecisionValue = "rejected"

	KargoPhaseSucceeded KargoPhase = "Succeeded"
	KargoPhaseFailed    KargoPhase = "Failed"
	KargoPhaseErrored   KargoPhase = "Errored"
	KargoPhaseAborted   KargoPhase = "Aborted"

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
	ErrProjectNotFound            = errors.New("kargo project not found")
	ErrPolicyNotFound             = errors.New("promotion policy not found")
	ErrIntegrationNotFound        = errors.New("kargo integration not found")
	ErrIntegrationCluster         = errors.New("kargo integration belongs to another cluster")
	ErrStageArchived              = errors.New("promotion Stage is archived")
	ErrStageStale                 = errors.New("promotion Stage observation is stale")
	ErrRepositoryUnavailable      = errors.New("promotion repository is unavailable")
	ErrReviewContextUnavailable   = errors.New("review context is unavailable for this Stage")
	ErrStageControlFlow           = errors.New("control-flow Stages cannot materialize Promotions")
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
	ErrFreightUnavailable         = errors.New("promotion Freight is not observed as available to the target Stage")
	ErrStageScope                 = errors.New("promotion source and target Stages must share integration, cluster, and project")
	ErrNoCreationDispatch         = errors.New("no Promotion creation dispatch is available")
	ErrCreationDispatchConflict   = errors.New("promotion creation dispatch conflicts with current state")
	ErrNoReconciliation           = errors.New("no Promotion reconciliation is available")
	ErrReconciliationConflict     = errors.New(
		"promotion reconciliation conflicts with current state",
	)
)

type PolicyMode string

type PromotionSource string

type DecisionValue string

type KargoPhase string

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
	Freight                         Freight
	CandidateFreight                Freight
	AutoPromotionEnabled            bool
	ControlFlow                     bool
	Conditions                      []StageCondition
	CurrentPromotion                *StagePromotionReference
	LastPromotion                   *StagePromotionReference
	HealthStatus                    *string
	ArgoApplicationNamespace        *string
	SuccessfulRenderAcknowledgement *SuccessfulRenderAcknowledgement
	FirstObservedAt                 time.Time
	LastObservedAt                  time.Time
	ArchivedAt                      *time.Time
}

type StageCondition struct {
	Type               string     `json:"type"`
	Status             string     `json:"status"`
	Reason             string     `json:"reason,omitempty"`
	Message            string     `json:"message,omitempty"`
	LastTransitionTime *time.Time `json:"lastTransitionTime,omitempty"`
}

type StagePromotionReference struct {
	Name       string     `json:"name"`
	Phase      string     `json:"phase,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
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
	ID                      uuid.UUID
	Source                  PromotionSource
	Integration             string
	ExternalPromotionID     string
	RequestDigest           RequestDigest
	StageID                 uuid.UUID
	SourceStageID           *uuid.UUID
	StageSnapshot           PromotionStageSnapshot
	ReviewContext           *PromotionReviewContext
	PolicySnapshot          PromotionPolicySnapshot
	SelectedFreightSnapshot *FreightItem
	ReviewRequired          bool
	Lifecycle               PromotionLifecycle
	Message                 *string
	CreationDispatch        CreationDispatchState
	Preparation             PreparationState
	KargoExecution          KargoExecutionState
	RequestedByPrincipalID  uuid.UUID
	RequestedBySnapshot     productidentity.ActorSnapshot
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

type KargoOutcome struct {
	Phase      KargoPhase
	ObservedAt time.Time
	Diagnostic *string
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

func (d DecisionValue) Valid() bool {
	return d == DecisionApproved || d == DecisionRejected
}

func (p KargoPhase) Terminal() bool {
	switch p {
	case KargoPhaseSucceeded, KargoPhaseFailed, KargoPhaseErrored, KargoPhaseAborted:
		return true
	default:
		return false
	}
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
	} {
		if err := validateText(label, value, maximumTextLength); err != nil {
			return err
		}
	}
	for label, value := range map[string]string{
		"application":    s.Application,
		"environment":    s.Environment,
		"repository URL": s.RepositoryURL,
		"target branch":  s.TargetBranch,
	} {
		if value == "" {
			continue
		}
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
	if s.RepositoryName != "" {
		if err := kubevalidate.DNSLabel("repository name", s.RepositoryName); err != nil {
			return invalid(err)
		}
	}
	if s.HealthStatus != nil {
		if err := validateText("Kargo Stage health status", *s.HealthStatus, maximumTextLength); err != nil {
			return err
		}
	}
	if s.ArgoApplicationNamespace != nil {
		if err := kubevalidate.DNSLabel("Argo CD Application namespace", *s.ArgoApplicationNamespace); err != nil {
			return invalid(err)
		}
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
		if !request.AvailabilityStrategy.Valid() {
			return fmt.Errorf("%w: requested Freight availability strategy is invalid", ErrInvalid)
		}
		if request.RequiredSoakTime < 0 {
			return fmt.Errorf("%w: requested Freight soak time must not be negative", ErrInvalid)
		}
		if !request.Direct && len(request.SourceStages) == 0 {
			return fmt.Errorf("%w: requested Freight must have a direct or Stage source", ErrInvalid)
		}
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
	if p.ID == uuid.Nil || p.StageID == uuid.Nil || p.RequestedByPrincipalID == uuid.Nil {
		return fmt.Errorf("%w: Promotion, Stage, and principal IDs are required", ErrInvalid)
	}
	if err := kubevalidate.DNSLabel("Kargo integration name", p.Integration); err != nil {
		return invalid(err)
	}
	if !p.Source.Valid() {
		return fmt.Errorf("%w: Promotion source is invalid", ErrInvalid)
	}
	if err := validateText("external Promotion ID", p.ExternalPromotionID, maximumKargoPromotionNameLength); err != nil {
		return err
	}
	if p.StageSnapshot.ID != p.StageID || p.StageSnapshot.Integration != p.Integration {
		return fmt.Errorf("%w: Promotion Stage snapshot identity does not match", ErrInvalid)
	}
	if !p.PolicySnapshot.ConfiguredMode.Valid() || !p.PolicySnapshot.EffectiveMode.Valid() {
		return fmt.Errorf("%w: Promotion policy snapshot modes are invalid", ErrInvalid)
	}
	hydrated := p.RequestDigest != (RequestDigest{})
	if !hydrated && p.Source != SourceSlipramp {
		return fmt.Errorf("%w: Promotion callback data is required", ErrInvalid)
	}
	if p.ReviewContext != nil {
		if err := p.ReviewContext.Validate(p.StageSnapshot); err != nil {
			return err
		}
	} else if p.ReviewRequired {
		return fmt.Errorf("%w: review-required Promotion requires review context", ErrInvalid)
	}
	if p.ReviewRequired && p.Lifecycle.State != LifecycleCreating && p.Lifecycle.State != LifecycleError &&
		(p.ReviewContext == nil || p.ReviewContext.Git == nil) {
		return fmt.Errorf("%w: Promotion review preparation requires Git context", ErrInvalid)
	}
	if p.Source == SourceKargo {
		if p.SourceStageID != nil || p.SelectedFreightSnapshot != nil {
			return fmt.Errorf("%w: Kargo-originated Promotion cannot have selected Freight", ErrInvalid)
		}
	} else {
		if p.SelectedFreightSnapshot == nil {
			return fmt.Errorf("%w: Slipramp-originated Promotion requires a Freight snapshot", ErrInvalid)
		}
		if p.Lifecycle.State == LifecycleCreating && hydrated {
			return fmt.Errorf("%w: creating Promotion cannot have callback data", ErrInvalid)
		}
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
	if err := p.Lifecycle.Validate(p.ReviewRequired, p.CreatedAt); err != nil {
		return err
	}
	if err := p.CreationDispatch.Validate(p.Source, p.Lifecycle.State, p.CreatedAt); err != nil {
		return err
	}
	if err := p.Preparation.Validate(p.ReviewRequired, p.Lifecycle.State, p.CreatedAt); err != nil {
		return err
	}
	if err := p.KargoExecution.Validate(p.Source, p.Lifecycle.State, p.CreationDispatch, p.CreatedAt); err != nil {
		return err
	}

	return nil
}

func (c PromotionReviewContext) Validate(stage PromotionStageSnapshot) error {
	if c.RepositoryID == uuid.Nil {
		return fmt.Errorf("%w: review Repository ID is required", ErrInvalid)
	}
	if err := c.RepositorySnapshot.Validate(); err != nil {
		return err
	}
	if err := validateText("target branch", c.TargetBranch, maximumBranchLength); err != nil {
		return err
	}
	if !stage.Enrichment.ReviewReady() {
		return fmt.Errorf("%w: Promotion Stage snapshot lacks complete review enrichment", ErrInvalid)
	}
	if stage.Enrichment.RepositoryName != c.RepositorySnapshot.Name ||
		stage.Enrichment.RepositoryURL != c.RepositorySnapshot.CloneURL ||
		stage.Enrichment.TargetBranch != c.TargetBranch {
		return fmt.Errorf("%w: Promotion review context does not match its Stage snapshot", ErrInvalid)
	}
	if c.Git != nil {
		return c.Git.Validate()
	}

	return nil
}

func (c PromotionGitContext) Validate() error {
	if c.BaseRevision == "" || c.CandidateRevision == "" || c.CandidateBranch == "" {
		return fmt.Errorf("%w: base revision, candidate revision, and candidate branch must be supplied together", ErrInvalid)
	}
	if err := validateRevision("base revision", c.BaseRevision); err != nil {
		return err
	}
	if err := validateRevision("candidate revision", c.CandidateRevision); err != nil {
		return err
	}
	if err := validateText("candidate branch", c.CandidateBranch, maximumBranchLength); err != nil {
		return err
	}
	if branch, ok := strings.CutPrefix(c.CandidateBranch, "kargo/promotion/"); !ok || branch == "" {
		return fmt.Errorf("%w: candidate branch must be under kargo/promotion/", ErrInvalid)
	}

	return nil
}

func (s PromotionRepositorySnapshot) Validate() error {
	if err := kubevalidate.DNSLabel("repository name", s.Name); err != nil {
		return invalid(err)
	}
	if err := validateText("repository clone URL", s.CloneURL, maximumURLLength); err != nil {
		return err
	}
	if err := validateText("repository branch", s.Branch, maximumBranchLength); err != nil {
		return err
	}

	return nil
}

func (o KargoOutcome) Validate(createdAt time.Time) error {
	if !o.Phase.Terminal() {
		return fmt.Errorf("%w: Kargo outcome phase is not terminal", ErrInvalid)
	}
	if o.ObservedAt.IsZero() || o.ObservedAt.Before(createdAt) {
		return fmt.Errorf("%w: Kargo outcome observation time is invalid", ErrInvalid)
	}
	if o.Diagnostic != nil {
		if err := validateText("Kargo outcome diagnostic", *o.Diagnostic, maximumMessageLength); err != nil {
			return err
		}
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
	if !repository.ValidCommitSHA(revision) {
		return fmt.Errorf("%w: %s must be a lowercase full Git object ID", ErrInvalid, label)
	}

	return nil
}
