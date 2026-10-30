package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/promotion"
)

type recordingPromotionPipelineService struct {
	filter promotion.FreightPipelineListFilter
	result promotion.FreightPipelineList
	err    error
}

func (s *recordingPromotionPipelineService) List(
	_ context.Context,
	filter promotion.FreightPipelineListFilter,
) (promotion.FreightPipelineList, error) {
	s.filter = filter

	return s.result, s.err
}

func TestListPromotionPipelinesReturnsOrderedFreightReadModel(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	stageID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1907")
	healthStatus := "Healthy"
	argoNamespace := "argocd"
	pipelines := &recordingPromotionPipelineService{
		result: promotion.FreightPipelineList{
			Items: []promotion.FreightPipeline{{
				Integration:      "prod",
				ClusterName:      "colo",
				Project:          "kargo-core",
				Repository:       "ops",
				Application:      "external-dns",
				EnvironmentOrder: []string{"homelab", "prod"},
				Stages: []promotion.FreightPipelineStage{{
					Stage: promotion.Stage{
						ID:                       stageID,
						Name:                     "external-dns-prod",
						Environment:              "prod",
						Application:              "external-dns",
						Freight:                  promotion.Freight{Items: []promotion.FreightItem{{Origin: "Warehouse/external-dns", Name: "freight-1", Artifacts: []promotion.FreightArtifact{{Kind: "image", Repository: new("ghcr.io/example/external-dns"), Version: "1.2.3"}}}}},
						HealthStatus:             &healthStatus,
						ArgoApplicationNamespace: &argoNamespace,
						LastObservedAt:           now,
					},
					PromotionSources: []promotion.FreightPromotionSource{{
						StageID:     uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1908"),
						StageName:   "external-dns-homelab",
						Environment: "homelab",
						Freight: []promotion.FreightItem{{
							Origin: "Warehouse/external-dns",
							Name:   "freight-1",
							Artifacts: []promotion.FreightArtifact{{
								Kind:    "image",
								Version: "1.2.3",
							}},
						}},
					}},
					AvailableFreight: []promotion.FreightItem{{
						Origin: "Warehouse/external-dns",
						Name:   "freight-2",
					}},
					PendingPromotionCount: 1,
					ObservationStale:      true,
				}},
			}},
			Total:  1,
			Limit:  10,
			Offset: 2,
		},
	}
	httpHandler := NewHTTP(
		Dependencies{
			PromotionPipelines: pipelines,
			KargoIntegrations: promotion.IntegrationSet{
				"prod": {
					UIURL:        "https://kargo.example.com",
					ArgoCDUIURLs: map[string]string{"prod": "https://argocd.example.com"},
				},
			},
			ProductAuthentication: &recordingProductAuthenticationService{
				authentication: productAuthentication(
					productidentity.RoleViewer,
					productidentity.AuthenticationMethodAPIKey,
					[]productidentity.Scope{productidentity.ScopeRead},
				),
			},
		},
		HTTPOptions{},
	)
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/api/v1/promotion-pipelines?integration=prod&cluster=colo&project=kargo-core&"+
			"repository=ops&application=external-dns&attention=any&limit=10&offset=2",
		nil,
	)
	request.Header.Set("Authorization", "Bearer viewer-key")
	response := httptest.NewRecorder()

	httpHandler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body)
	}
	if pipelines.filter.Integration != "prod" || pipelines.filter.Cluster != "colo" ||
		pipelines.filter.Project != "kargo-core" || pipelines.filter.Repository != "ops" ||
		pipelines.filter.Application != "external-dns" ||
		pipelines.filter.Attention != promotion.FreightPipelineAttentionAny || pipelines.filter.Limit != 10 ||
		pipelines.filter.Offset != 2 {
		t.Errorf("pipeline filter = %#v", pipelines.filter)
	}
	for _, expected := range []string{
		stageID.String(),
		`"environmentOrder":["homelab","prod"]`,
		`"version":"1.2.3"`,
		`"stageName":"external-dns-homelab"`,
		`"promotionSources":[`,
		`"availableFreight":{"items":[`,
		`"autoPromotionEnabled":false`,
		`"healthStatus":"Healthy"`,
		`"observationStale":true`,
		`"pendingPromotionCount":1`,
		`"kargoUrl":"https://kargo.example.com/project/kargo-core/stage/external-dns-prod"`,
		`"argoCdUrl":"https://argocd.example.com/applications/argocd/external-dns"`,
	} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Errorf("response %q does not contain %q", response.Body.String(), expected)
		}
	}
}
