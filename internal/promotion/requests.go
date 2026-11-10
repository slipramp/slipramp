package promotion

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/kubevalidate"
	"github.com/slipramp/slipramp/internal/productidentity"
)

const (
	PreparationParameterPromotionID         = "promotion-id"
	PreparationParameterExternalPromotionID = "external-promotion-id"
	PreparationParameterProject             = "project"
	PreparationParameterStage               = "stage"
	PreparationParameterApplication         = "application"
	PreparationParameterEnvironment         = "environment"
	PreparationParameterRepositoryName      = "repository-name"
	PreparationParameterRepositoryURL       = "repository-url"
	PreparationParameterBaseRevision        = "base-revision"
	PreparationParameterCandidateRevision   = "candidate-revision"
	PreparationParameterCandidateBranch     = "candidate-branch"
	PreparationParameterTargetBranch        = "target-branch"
	PreparationParameterSourcePaths         = "source-paths"
)

type KargoRequest struct {
	ExternalPromotionID string `json:"externalPromotionId"`
	Project             string `json:"project"`
	Stage               string `json:"stage"`
	BaseRevision        string `json:"baseRevision,omitempty"`
	CandidateRevision   string `json:"candidateRevision,omitempty"`
	CandidateBranch     string `json:"candidateBranch,omitempty"`
}

type PromotionAcceptance struct {
	ID                         uuid.UUID
	Integration                string
	Request                    KargoRequest
	RequestDigest              RequestDigest
	AllowedRepositories        []string
	RequestedByPrincipalID     uuid.UUID
	RequestedBySnapshot        productidentity.ActorSnapshot
	AcceptedAt                 time.Time
	MaximumStageObservationAge time.Duration
	PendingReviewRetention     time.Duration
}

type PromotionRequestStore interface {
	AcceptPromotionRequest(context.Context, PromotionAcceptance) (Promotion, error)
}

type RequestService struct {
	integrations IntegrationSet
	requests     PromotionRequestStore
	newID        func() uuid.UUID
	now          func() time.Time
}

func NewRequestService(integrations IntegrationSet, requests PromotionRequestStore) *RequestService {
	return &RequestService{
		integrations: integrations,
		requests:     requests,
		newID:        uuid.New,
		now:          time.Now,
	}
}

func (r KargoRequest) Validate() error {
	if err := validateText("external Promotion ID", r.ExternalPromotionID, maximumKargoPromotionNameLength); err != nil {
		return err
	}
	if err := kubevalidate.DNSLabel("Kargo project", r.Project); err != nil {
		return invalid(err)
	}
	if err := kubevalidate.DNSSubdomain("Kargo Stage name", r.Stage); err != nil {
		return invalid(err)
	}
	_, err := r.GitContext()

	return err
}

func (r KargoRequest) GitContext() (*PromotionGitContext, error) {
	git := PromotionGitContext{
		BaseRevision:      r.BaseRevision,
		CandidateRevision: r.CandidateRevision,
		CandidateBranch:   r.CandidateBranch,
	}
	if git == (PromotionGitContext{}) {
		return nil, nil
	}
	if err := git.Validate(); err != nil {
		return nil, err
	}

	return &git, nil
}

func (r KargoRequest) Digest() (RequestDigest, error) {
	if err := r.Validate(); err != nil {
		return RequestDigest{}, err
	}

	encoded, err := json.Marshal(r)
	if err != nil {
		return RequestDigest{}, fmt.Errorf("encode canonical Kargo Promotion request: %w", err)
	}

	return sha256.Sum256(encoded), nil
}

func (a PromotionAcceptance) Validate() error {
	if a.ID == uuid.Nil || a.RequestedByPrincipalID == uuid.Nil {
		return fmt.Errorf("%w: Promotion and requesting principal IDs are required", ErrInvalid)
	}
	if err := kubevalidate.DNSLabel("Kargo integration name", a.Integration); err != nil {
		return invalid(err)
	}
	digest, err := a.Request.Digest()
	if err != nil {
		return err
	}
	if digest != a.RequestDigest {
		return fmt.Errorf("%w: canonical request digest does not match the request", ErrInvalid)
	}
	for _, name := range a.AllowedRepositories {
		if err := kubevalidate.DNSLabel("Kargo repository name", name); err != nil {
			return invalid(err)
		}
	}
	if a.RequestedBySnapshot.PrincipalID != a.RequestedByPrincipalID {
		return fmt.Errorf("%w: requesting actor snapshot does not match its principal", ErrInvalid)
	}
	if err := a.RequestedBySnapshot.Validate(); err != nil {
		return invalid(err)
	}
	if a.RequestedBySnapshot.Kind != productidentity.KindServiceAccount {
		return fmt.Errorf("%w: requesting principal must be a service account", ErrInvalid)
	}
	if a.AcceptedAt.IsZero() || a.MaximumStageObservationAge <= 0 || a.PendingReviewRetention < 0 {
		return fmt.Errorf("%w: request time and valid integration durations are required", ErrInvalid)
	}

	return nil
}

func (p Promotion) PreparationParameters() (map[string]any, error) {
	stage := p.StageSnapshot
	if stage.ID == uuid.Nil || stage.Name == "" || stage.Project == "" || !stage.Enrichment.ReviewReady() ||
		p.ReviewContext == nil || p.ReviewContext.Git == nil {
		return nil, fmt.Errorf("%w: Promotion Stage snapshot is incomplete", ErrInvalid)
	}
	enrichment := stage.Enrichment
	review := p.ReviewContext
	git := review.Git

	paths, err := json.Marshal(enrichment.SourcePaths)
	if err != nil {
		return nil, fmt.Errorf("encode Promotion source paths: %w", err)
	}

	return map[string]any{
		PreparationParameterPromotionID:         p.ID.String(),
		PreparationParameterExternalPromotionID: p.ExternalPromotionID,
		PreparationParameterProject:             stage.Project,
		PreparationParameterStage:               stage.Name,
		PreparationParameterApplication:         enrichment.Application,
		PreparationParameterEnvironment:         enrichment.Environment,
		PreparationParameterRepositoryName:      enrichment.RepositoryName,
		PreparationParameterRepositoryURL:       enrichment.RepositoryURL,
		PreparationParameterBaseRevision:        git.BaseRevision,
		PreparationParameterCandidateRevision:   git.CandidateRevision,
		PreparationParameterCandidateBranch:     git.CandidateBranch,
		PreparationParameterTargetBranch:        review.TargetBranch,
		PreparationParameterSourcePaths:         string(paths),
	}, nil
}

func (s *RequestService) SubmitKargoPromotion(
	ctx context.Context,
	integrationName string,
	request KargoRequest,
	principal productidentity.Principal,
) (Promotion, error) {
	integration, ok := s.integrations.Lookup(integrationName)
	if !ok {
		return Promotion{}, ErrIntegrationNotFound
	}
	digest, err := request.Digest()
	if err != nil {
		return Promotion{}, err
	}
	actor, err := principal.ActorSnapshot()
	if err != nil {
		return Promotion{}, invalid(err)
	}
	if principal.Kind != productidentity.KindServiceAccount {
		return Promotion{}, fmt.Errorf("%w: requesting principal must be a service account", ErrInvalid)
	}

	acceptedAt := s.now().UTC().Truncate(time.Microsecond)
	acceptance := PromotionAcceptance{
		ID:                         s.newID(),
		Integration:                integrationName,
		Request:                    request,
		RequestDigest:              digest,
		AllowedRepositories:        slices.Clone(integration.Repositories),
		RequestedByPrincipalID:     principal.ID,
		RequestedBySnapshot:        actor,
		AcceptedAt:                 acceptedAt,
		MaximumStageObservationAge: integration.MaximumStageObservationAge,
		PendingReviewRetention:     integration.PendingReviewRetention,
	}

	return s.requests.AcceptPromotionRequest(ctx, acceptance)
}
