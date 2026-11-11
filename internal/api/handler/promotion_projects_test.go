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
	"github.com/slipramp/slipramp/internal/promotion/projection"
)

type recordingPromotionProjectService struct {
	filter projection.ProjectListFilter
	result projection.ProjectList
	key    projection.ProjectKey
	detail projection.ProjectDetail
	err    error
}

func (s *recordingPromotionProjectService) List(
	_ context.Context,
	filter projection.ProjectListFilter,
) (projection.ProjectList, error) {
	s.filter = filter

	return s.result, s.err
}

func (s *recordingPromotionProjectService) Get(
	_ context.Context,
	key projection.ProjectKey,
) (projection.ProjectDetail, error) {
	s.key = key
	if s.err != nil {
		return projection.ProjectDetail{}, s.err
	}
	if s.detail.Project != "" {
		return s.detail, nil
	}

	return projection.ProjectDetail{}, promotion.ErrProjectNotFound
}

func TestListPromotionProjectsReturnsObservedProjectSummaries(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	projects := &recordingPromotionProjectService{
		result: projection.ProjectList{
			Items: []projection.ProjectSummary{{
				Integration:                   "prod",
				ClusterName:                   "colo",
				Project:                       "kargo-core",
				StageCount:                    4,
				UnhealthyStageCount:           1,
				StaleStageCount:               2,
				PendingReviewCount:            3,
				FreightDifferenceCount:        4,
				UnknownFreightComparisonCount: 5,
				LatestObservedAt:              now,
			}},
			Summary: projection.ProjectListSummary{
				StageCount: 4, UnhealthyStageCount: 1, StaleStageCount: 2, PendingReviewCount: 3,
				FreightDifferenceCount: 4, UnknownFreightComparisonCount: 5,
			},
			Total:  1,
			Limit:  10,
			Offset: 2,
		},
	}
	httpHandler := NewHTTP(
		Dependencies{
			PromotionProjects: projects,
			KargoIntegrations: promotion.IntegrationSet{
				"prod": {UIURL: "https://kargo.example.com"},
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
		"/api/v1/promotion-projects?integration=prod&cluster=colo&project=kargo-core&"+
			"freightAlignment=different&unhealthy=true&stale=true&pendingReview=true&limit=10&offset=2",
		nil,
	)
	request.Header.Set("Authorization", "Bearer viewer-key")
	response := httptest.NewRecorder()

	httpHandler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body)
	}
	if projects.filter.Integration != "prod" || projects.filter.Cluster != "colo" ||
		projects.filter.Project != "kargo-core" || projects.filter.Limit != 10 ||
		projects.filter.Offset != 2 || projects.filter.FreightAlignment != projection.FreightAlignmentDifferent ||
		!projects.filter.Unhealthy || !projects.filter.Stale || !projects.filter.PendingReview {
		t.Errorf("project filter = %#v", projects.filter)
	}
	for _, expected := range []string{
		`"integration":"prod"`,
		`"cluster":"colo"`,
		`"project":"kargo-core"`,
		`"stageCount":4`,
		`"unhealthyStageCount":1`,
		`"staleStageCount":2`,
		`"pendingReviewCount":3`,
		`"freightDifferenceCount":4`,
		`"unknownFreightComparisonCount":5`,
		`"summary":{"freightDifferenceCount":4,"pendingReviewCount":3,"stageCount":4,"staleStageCount":2,"unhealthyStageCount":1,"unknownFreightComparisonCount":5}`,
		`"latestObservedAt":"2026-10-22T12:00:00Z"`,
		`"kargoUrl":"https://kargo.example.com/project/kargo-core"`,
	} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Errorf("response %q does not contain %q", response.Body.String(), expected)
		}
	}
}

func TestGetPromotionProjectReturnsCanonicalStageTopology(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 8, 12, 0, 0, 0, time.UTC)
	stageID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1931")
	sourceStageID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1932")
	projects := &recordingPromotionProjectService{
		detail: projection.ProjectDetail{
			Integration: "prod",
			ClusterName: "colo",
			Project:     "kargo-core",
			Stages: []projection.ProjectStage{{
				Stage: promotion.Stage{
					ID: stageID, Integration: "prod", ClusterName: "colo", Project: "kargo-core", Name: "prod",
					KubernetesUID: "stage-uid", ResourceVersion: "42", ControlFlow: true,
					Conditions:      []promotion.StageCondition{{Type: "Ready", Status: "False"}},
					Freight:         promotion.Freight{Items: []promotion.FreightItem{}},
					FirstObservedAt: now.Add(-time.Hour), LastObservedAt: now,
				},
				Policy: promotion.PromotionPolicySnapshot{
					ConfiguredMode: promotion.PolicyModeAutomatic,
					EffectiveMode:  promotion.PolicyModeAutomatic,
				},
				ObservationStale: true,
				RequestedFreight: []projection.ProjectFreightRequest{{
					Request: promotion.FreightRequest{
						Origin: "Warehouse/app", Direct: true,
						AvailabilityStrategy: promotion.FreightAvailabilityOneOf,
					},
					Sources: []projection.ProjectStageSource{},
					Candidates: []projection.FreightCandidate{{
						Freight: promotion.FreightItem{Origin: "Warehouse/app", Name: "freight-1"},
						Evidence: promotion.FreightReadinessEvidence{
							Readiness: promotion.FreightReadinessReady,
							Facts:     []string{"The Stage accepts Freight directly from its origin."},
						},
					}},
				}},
				FreightAlignments: []projection.FreightAlignment{{
					Origin: "Warehouse/app", SourceStageName: "qa", SourceStageID: &sourceStageID,
					TargetStageID: stageID, Status: projection.FreightAlignmentDifferent,
					SourceCurrentFreight: &promotion.FreightItem{
						Origin: "Warehouse/app", Name: "freight-2",
						Artifacts: []promotion.FreightArtifact{{Kind: "image", Version: "2.0.0"}},
					},
					TargetCurrentFreight: &promotion.FreightItem{
						Origin: "Warehouse/app", Name: "freight-1",
						Artifacts: []promotion.FreightArtifact{{Kind: "image", Version: "1.0.0"}},
					},
					Reason: "The source and target currently use different Freight for this origin.",
				}},
				Capabilities: projection.StageCapabilities{
					Promotion: projection.StageCapability{
						Status: projection.StageCapabilityNotApplicable,
						Reason: "Control-flow Stages do not materialize Promotions.",
					},
					Review: projection.StageCapability{
						Status: projection.StageCapabilityUnavailable,
						Reason: "Slipramp review context is unavailable.",
					},
					BranchDrift: projection.StageCapability{
						Status: projection.StageCapabilityUnavailable,
						Reason: "Branch drift mapping is unavailable.",
					},
				},
			}},
		},
	}
	httpHandler := NewHTTP(
		Dependencies{
			PromotionProjects: projects,
			KargoIntegrations: promotion.IntegrationSet{
				"prod": {UIURL: "https://kargo.example.com"},
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
		"/api/v1/promotion-projects/prod/colo/kargo-core",
		nil,
	)
	request.Header.Set("Authorization", "Bearer viewer-key")
	response := httptest.NewRecorder()

	httpHandler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body)
	}
	if projects.key != (projection.ProjectKey{Integration: "prod", Cluster: "colo", Project: "kargo-core"}) {
		t.Errorf("project key = %#v", projects.key)
	}
	for _, expected := range []string{
		"\"id\":\"018f90ff-9fd8-7b5f-b7e1-f32ff54c1931\"",
		"\"controlFlow\":true",
		"\"observationStale\":true",
		"\"availabilityStrategy\":\"OneOf\"",
		"\"readiness\":\"ready\"",
		"\"sourceStageName\":\"qa\"",
		"\"status\":\"different\"",
		"\"sourceCurrentFreight\":{\"artifacts\":[{\"kind\":\"image\",\"version\":\"2.0.0\"}],\"name\":\"freight-2\",\"origin\":\"Warehouse/app\"}",
		"\"conditions\":[{\"status\":\"False\",\"type\":\"Ready\"}]",
		"\"promotion\":{\"reason\":\"Control-flow Stages do not materialize Promotions.\",\"status\":\"not-applicable\"}",
		"\"kargoUrl\":\"https://kargo.example.com/project/kargo-core/stage/prod\"",
	} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Errorf("response %q does not contain %q", response.Body.String(), expected)
		}
	}
}
