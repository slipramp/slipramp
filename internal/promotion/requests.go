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
	BaseRevision        string `json:"baseRevision"`
	CandidateRevision   string `json:"candidateRevision"`
	CandidateBranch     string `json:"candidateBranch"`
}

type PromotionStageSnapshot struct {
	ID                              uuid.UUID        `json:"id"`
	Integration                     string           `json:"integration"`
	ClusterName                     string           `json:"cluster"`
	Project                         string           `json:"project"`
	Name                            string           `json:"name"`
	KubernetesUID                   string           `json:"kubernetesUid"`
	ResourceVersion                 string           `json:"resourceVersion"`
	Application                     string           `json:"application"`
	Environment                     string           `json:"environment"`
	RepositoryName                  string           `json:"repository"`
	RepositoryURL                   string           `json:"repositoryUrl"`
	TargetBranch                    string           `json:"targetBranch"`
	SourcePaths                     []string         `json:"sourcePaths"`
	RequestedFreight                []FreightRequest `json:"requestedFreight"`
	Freight                         json.RawMessage  `json:"freight"`
	SuccessfulRenderAcknowledgement json.RawMessage  `json:"successfulRenderAcknowledgement,omitempty"`
	LastObservedAt                  time.Time        `json:"lastObservedAt"`
}

type PromotionRepositorySnapshot struct {
	Name                   string  `json:"name"`
	CloneURL               string  `json:"cloneUrl"`
	Branch                 string  `json:"branch"`
	CheckoutCredentialName *string `json:"checkoutCredentialName,omitempty"`
}

type PromotionPolicySnapshot struct {
	ConfiguredMode PolicyMode                     `json:"configuredMode"`
	EffectiveMode  PolicyMode                     `json:"effectiveMode"`
	ExpiresAt      *time.Time                     `json:"expiresAt,omitempty"`
	Reason         *string                        `json:"reason,omitempty"`
	UpdatedBy      *productidentity.ActorSnapshot `json:"updatedBy,omitempty"`
	UpdatedAt      *time.Time                     `json:"updatedAt,omitempty"`
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
	if err := validateRevision("base revision", r.BaseRevision); err != nil {
		return err
	}
	if err := validateRevision("candidate revision", r.CandidateRevision); err != nil {
		return err
	}
	if err := validateText("candidate branch", r.CandidateBranch, maximumBranchLength); err != nil {
		return err
	}
	if len(r.CandidateBranch) <= len("kargo/promotion/") ||
		r.CandidateBranch[:len("kargo/promotion/")] != "kargo/promotion/" {
		return fmt.Errorf("%w: candidate branch must be under kargo/promotion/", ErrInvalid)
	}

	return nil
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
	if err := a.Request.Validate(); err != nil {
		return err
	}
	digest, err := a.Request.Digest()
	if err != nil {
		return err
	}
	if digest != a.RequestDigest {
		return fmt.Errorf("%w: canonical request digest does not match the request", ErrInvalid)
	}
	if len(a.AllowedRepositories) == 0 {
		return fmt.Errorf("%w: at least one allowed repository is required", ErrInvalid)
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
	if a.AcceptedAt.IsZero() || a.MaximumStageObservationAge <= 0 || a.PendingReviewRetention <= 0 {
		return fmt.Errorf("%w: request time and positive integration durations are required", ErrInvalid)
	}

	return nil
}

func (p Promotion) PreparationParameters() (map[string]any, error) {
	var stage PromotionStageSnapshot
	if err := json.Unmarshal(p.StageSnapshot, &stage); err != nil {
		return nil, fmt.Errorf("decode Promotion Stage snapshot: %w", err)
	}
	if stage.ID == uuid.Nil || stage.Name == "" || stage.Project == "" || stage.Application == "" ||
		stage.Environment == "" || stage.RepositoryName == "" || stage.RepositoryURL == "" ||
		len(stage.SourcePaths) == 0 {
		return nil, fmt.Errorf("%w: Promotion Stage snapshot is incomplete", ErrInvalid)
	}

	paths, err := json.Marshal(stage.SourcePaths)
	if err != nil {
		return nil, fmt.Errorf("encode Promotion source paths: %w", err)
	}

	return map[string]any{
		PreparationParameterPromotionID:         p.ID.String(),
		PreparationParameterExternalPromotionID: p.ExternalPromotionID,
		PreparationParameterProject:             stage.Project,
		PreparationParameterStage:               stage.Name,
		PreparationParameterApplication:         stage.Application,
		PreparationParameterEnvironment:         stage.Environment,
		PreparationParameterRepositoryName:      stage.RepositoryName,
		PreparationParameterRepositoryURL:       stage.RepositoryURL,
		PreparationParameterBaseRevision:        p.BaseRevision,
		PreparationParameterCandidateRevision:   p.CandidateRevision,
		PreparationParameterCandidateBranch:     p.CandidateBranch,
		PreparationParameterTargetBranch:        p.TargetBranch,
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
