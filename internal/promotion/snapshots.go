package promotion

import (
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/productidentity"
)

type PromotionStageSnapshot struct {
	ID                              uuid.UUID                        `json:"id"`
	Integration                     string                           `json:"integration"`
	ClusterName                     string                           `json:"cluster"`
	Project                         string                           `json:"project"`
	Name                            string                           `json:"name"`
	KubernetesUID                   string                           `json:"kubernetesUid"`
	ResourceVersion                 string                           `json:"resourceVersion"`
	Enrichment                      *PromotionStageEnrichment        `json:"enrichment,omitempty"`
	RequestedFreight                []FreightRequest                 `json:"requestedFreight"`
	Freight                         Freight                          `json:"freight"`
	ArgoApplicationNamespace        *string                          `json:"argoApplicationNamespace,omitempty"`
	SuccessfulRenderAcknowledgement *SuccessfulRenderAcknowledgement `json:"successfulRenderAcknowledgement,omitempty"`
	LastObservedAt                  time.Time                        `json:"lastObservedAt"`
}

type PromotionStageEnrichment struct {
	Application    string   `json:"application,omitempty"`
	Environment    string   `json:"environment,omitempty"`
	RepositoryName string   `json:"repository,omitempty"`
	RepositoryURL  string   `json:"repositoryUrl,omitempty"`
	TargetBranch   string   `json:"targetBranch,omitempty"`
	SourcePaths    []string `json:"sourcePaths,omitempty"`
}

type PromotionRepositorySnapshot struct {
	Name                   string  `json:"name"`
	CloneURL               string  `json:"cloneUrl"`
	Branch                 string  `json:"branch"`
	CheckoutCredentialName *string `json:"checkoutCredentialName,omitempty"`
}

type PromotionGitContext struct {
	BaseRevision      string
	CandidateRevision string
	CandidateBranch   string
}

type PromotionReviewContext struct {
	RepositoryID       uuid.UUID
	RepositorySnapshot PromotionRepositorySnapshot
	TargetBranch       string
	Git                *PromotionGitContext
}

type PromotionPolicySnapshot struct {
	ConfiguredMode PolicyMode                     `json:"configuredMode"`
	EffectiveMode  PolicyMode                     `json:"effectiveMode"`
	ExpiresAt      *time.Time                     `json:"expiresAt,omitempty"`
	Reason         *string                        `json:"reason,omitempty"`
	UpdatedBy      *productidentity.ActorSnapshot `json:"updatedBy,omitempty"`
	UpdatedAt      *time.Time                     `json:"updatedAt,omitempty"`
}

type SuccessfulRenderAcknowledgement struct {
	SourceRevision string `json:"sourceRevision,omitempty"`
	TargetRevision string `json:"targetRevision,omitempty"`
	ResultRevision string `json:"resultRevision,omitempty"`
}

func (s Stage) PromotionSnapshot() PromotionStageSnapshot {
	var enrichment *PromotionStageEnrichment
	if s.Application != "" || s.Environment != "" || s.RepositoryName != "" || s.RepositoryURL != "" ||
		s.TargetBranch != "" || len(s.SourcePaths) > 0 {
		enrichment = &PromotionStageEnrichment{
			Application: s.Application, Environment: s.Environment, RepositoryName: s.RepositoryName,
			RepositoryURL: s.RepositoryURL, TargetBranch: s.TargetBranch, SourcePaths: slices.Clone(s.SourcePaths),
		}
	}
	return PromotionStageSnapshot{
		ID:                              s.ID,
		Integration:                     s.Integration,
		ClusterName:                     s.ClusterName,
		Project:                         s.Project,
		Name:                            s.Name,
		KubernetesUID:                   s.KubernetesUID,
		ResourceVersion:                 s.ResourceVersion,
		Enrichment:                      enrichment,
		RequestedFreight:                cloneFreightRequests(s.RequestedFreight),
		Freight:                         cloneFreight(s.Freight),
		ArgoApplicationNamespace:        cloneOptionalString(s.ArgoApplicationNamespace),
		SuccessfulRenderAcknowledgement: cloneRenderAcknowledgement(s.SuccessfulRenderAcknowledgement),
		LastObservedAt:                  s.LastObservedAt,
	}
}

func (s Stage) ReviewReady() bool {
	return s.Application != "" && s.Environment != "" && s.RepositoryName != "" && s.RepositoryURL != "" &&
		s.TargetBranch != "" && len(s.SourcePaths) > 0
}

func (e PromotionStageEnrichment) ReviewReady() bool {
	return e.Application != "" && e.Environment != "" && e.RepositoryName != "" && e.RepositoryURL != "" &&
		e.TargetBranch != "" && len(e.SourcePaths) > 0
}

func cloneRenderAcknowledgement(value *SuccessfulRenderAcknowledgement) *SuccessfulRenderAcknowledgement {
	if value == nil {
		return nil
	}
	cloned := *value

	return &cloned
}

func cloneOptionalString(value *string) *string {
	if value == nil {
		return nil
	}

	return new(*value)
}
