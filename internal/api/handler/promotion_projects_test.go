package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/promotion"
)

type recordingPromotionProjectService struct {
	filter promotion.ProjectListFilter
	result promotion.ProjectList
	err    error
}

func (s *recordingPromotionProjectService) List(
	_ context.Context,
	filter promotion.ProjectListFilter,
) (promotion.ProjectList, error) {
	s.filter = filter

	return s.result, s.err
}

func TestListPromotionProjectsReturnsObservedProjectSummaries(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	projects := &recordingPromotionProjectService{
		result: promotion.ProjectList{
			Items: []promotion.ProjectSummary{{
				Integration:      "prod",
				ClusterName:      "colo",
				Project:          "kargo-core",
				StageCount:       4,
				ApplicationCount: 2,
				LastObservedAt:   now,
			}},
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
		"/api/v1/promotion-projects?integration=prod&cluster=colo&project=kargo-core&limit=10&offset=2",
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
		projects.filter.Offset != 2 {
		t.Errorf("project filter = %#v", projects.filter)
	}
	for _, expected := range []string{
		`"integration":"prod"`,
		`"cluster":"colo"`,
		`"project":"kargo-core"`,
		`"stageCount":4`,
		`"applicationCount":2`,
		`"lastObservedAt":"2026-10-22T12:00:00Z"`,
		`"kargoUrl":"https://kargo.example.com/project/kargo-core"`,
	} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Errorf("response %q does not contain %q", response.Body.String(), expected)
		}
	}
}
