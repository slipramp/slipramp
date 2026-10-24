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
	"github.com/slipramp/slipramp/internal/repository"
)

type recordingPromotionBranchDriftService struct {
	filter promotion.BranchDriftListFilter
	result promotion.BranchDriftList
	err    error
}

func (s *recordingPromotionBranchDriftService) List(
	_ context.Context,
	filter promotion.BranchDriftListFilter,
) (promotion.BranchDriftList, error) {
	s.filter = filter

	return s.result, s.err
}

func TestListPromotionBranchDriftReturnsRepositoryGroupedCache(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	lastRenderedAt := now.Add(-2 * time.Hour)
	stageID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1910")
	drift := &recordingPromotionBranchDriftService{
		result: promotion.BranchDriftList{
			Items: []promotion.BranchDriftRepository{{
				Repository: repository.Repository{Name: "ops", Branch: "main"},
				ScannedAt:  now,
				Stages: []promotion.BranchDriftStage{{
					Stage: promotion.Stage{
						ID:             stageID,
						Integration:    "prod",
						ClusterName:    "colo",
						Project:        "kargo-core",
						Name:           "external-dns-prod",
						Application:    "external-dns",
						Environment:    "prod",
						TargetBranch:   "stage/external-dns/prod",
						SourcePaths:    []string{"apps/external-dns"},
						Freight:        promotion.Freight{Items: []promotion.FreightItem{{Origin: "Warehouse/external-dns", Name: "freight-1", Artifacts: []promotion.FreightArtifact{}}}},
						LastObservedAt: now.Add(-time.Minute),
					},
					Status:                         promotion.BranchDriftStatusCurrent,
					AcknowledgedSourceRevision:     strings.Repeat("a", 40),
					AcknowledgedRenderBaseRevision: strings.Repeat("b", 40),
					AcknowledgedRenderRevision:     strings.Repeat("b", 40),
					AcknowledgedRenderAuthoredAt:   &lastRenderedAt,
					SourceBranchRevision:           strings.Repeat("a", 40),
					SourceBranchAuthoredAt:         &now,
					TargetBranchRevision:           strings.Repeat("b", 40),
					NoOpRender:                     true,
				}},
			}},
			Total:  42,
			Limit:  10,
			Offset: 2,
		},
	}
	httpHandler := NewHTTP(
		Dependencies{
			PromotionBranchDrift: drift,
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
		"/api/v1/promotion-branch-drift?status=current&integration=prod&project=kargo-core&application=external-dns&repository=ops&limit=10&offset=2",
		nil,
	)
	request.Header.Set("Authorization", "Bearer viewer-key")
	response := httptest.NewRecorder()

	httpHandler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body)
	}
	if drift.filter.Status != promotion.BranchDriftStatusCurrent ||
		drift.filter.Integration != "prod" || drift.filter.Project != "kargo-core" ||
		drift.filter.Application != "external-dns" ||
		drift.filter.Repository != "ops" ||
		drift.filter.Limit != 10 || drift.filter.Offset != 2 {
		t.Errorf("branch drift filter = %#v", drift.filter)
	}
	for _, expected := range []string{
		stageID.String(),
		`"repository":"ops"`,
		`"total":42`,
		`"limit":10`,
		`"offset":2`,
		`"status":"current"`,
		`"name":"freight-1"`,
		`"noOpRender":true`,
		`"sourceDrift":false`,
		`"targetDrift":false`,
		`"authoredAt":"2026-10-22T10:00:00Z"`,
		`"kargoUrl":"https://kargo.example.com/project/kargo-core/stage/external-dns-prod"`,
	} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Errorf("response %q does not contain %q", response.Body.String(), expected)
		}
	}
}
