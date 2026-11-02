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
	Application                     string                           `json:"application"`
	Environment                     string                           `json:"environment"`
	RepositoryName                  string                           `json:"repository"`
	RepositoryURL                   string                           `json:"repositoryUrl"`
	TargetBranch                    string                           `json:"targetBranch"`
	SourcePaths                     []string                         `json:"sourcePaths"`
	RequestedFreight                []FreightRequest                 `json:"requestedFreight"`
	Freight                         Freight                          `json:"freight"`
	ArgoApplicationNamespace        *string                          `json:"argoApplicationNamespace,omitempty"`
	SuccessfulRenderAcknowledgement *SuccessfulRenderAcknowledgement `json:"successfulRenderAcknowledgement,omitempty"`
	LastObservedAt                  time.Time                        `json:"lastObservedAt"`
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

type SuccessfulRenderAcknowledgement struct {
	SourceRevision string `json:"sourceRevision,omitempty"`
	TargetRevision string `json:"targetRevision,omitempty"`
	ResultRevision string `json:"resultRevision,omitempty"`
}

func (s Stage) PromotionSnapshot() PromotionStageSnapshot {
	return PromotionStageSnapshot{
		ID:                              s.ID,
		Integration:                     s.Integration,
		ClusterName:                     s.ClusterName,
		Project:                         s.Project,
		Name:                            s.Name,
		KubernetesUID:                   s.KubernetesUID,
		ResourceVersion:                 s.ResourceVersion,
		Application:                     s.Application,
		Environment:                     s.Environment,
		RepositoryName:                  s.RepositoryName,
		RepositoryURL:                   s.RepositoryURL,
		TargetBranch:                    s.TargetBranch,
		SourcePaths:                     slices.Clone(s.SourcePaths),
		RequestedFreight:                cloneFreightRequests(s.RequestedFreight),
		Freight:                         cloneFreight(s.Freight),
		ArgoApplicationNamespace:        cloneOptionalString(s.ArgoApplicationNamespace),
		SuccessfulRenderAcknowledgement: cloneRenderAcknowledgement(s.SuccessfulRenderAcknowledgement),
		LastObservedAt:                  s.LastObservedAt,
	}
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
