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

type recordingPromotionService struct {
	integration string
	cluster     string
	replacement promotion.StageSnapshotReplacement
	request     promotion.KargoRequest
	principal   productidentity.Principal
	record      promotion.Promotion
	err         error
}

func (s *recordingPromotionService) SubmitKargoPromotion(
	_ context.Context,
	integration string,
	request promotion.KargoRequest,
	principal productidentity.Principal,
) (promotion.Promotion, error) {
	s.integration = integration
	s.request = request
	s.principal = principal

	return s.record, s.err
}

func (s *recordingPromotionService) ReplaceStageSnapshot(
	_ context.Context,
	integration string,
	cluster string,
	replacement promotion.StageSnapshotReplacement,
) error {
	s.integration = integration
	s.cluster = cluster
	s.replacement = replacement

	return s.err
}

func TestReplaceKargoStageSnapshotUsesAuthenticatedCluster(t *testing.T) {
	t.Parallel()

	promotions := &recordingPromotionService{}
	httpHandler := NewHTTP(
		Dependencies{
			ControllerIdentities: controllerIdentityFor("controller-token", "colo"),
			PromotionStages:      promotions,
		},
		HTTPOptions{},
	)
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPut,
		"/controller/v1/kargo/prod/stages",
		strings.NewReader(`{
          "snapshotId":"018f90ff-9fd8-7b5f-b7e1-f32ff54c1903",
          "observedAt":"2026-10-21T12:00:00Z",
          "stages":[{
            "project":"kargo-core",
            "name":"external-dns-prod",
            "kubernetesUid":"stage-uid",
            "resourceVersion":"42",
            "application":"external-dns",
            "environment":"prod",
            "repositoryUrl":"https://example.com/ops.git",
            "targetBranch":"stage/external-dns/prod",
            "sourcePaths":["k8s/external-dns/base"],
            "freight":{"items":[]}
          }]
        }`),
	)
	request.Header.Set("Authorization", "Bearer controller-token")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	httpHandler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusNoContent, response.Body)
	}
	if promotions.integration != "prod" || promotions.cluster != "colo" ||
		len(promotions.replacement.Stages) != 1 {
		t.Fatalf("submission = %#v", promotions)
	}
	stage := promotions.replacement.Stages[0]
	if stage.RepositoryURL != "https://example.com/ops.git" || string(stage.Freight) != `{"items":[]}` {
		t.Errorf("Stage observation = %#v", stage)
	}
}

func TestReplaceKargoStageSnapshotRejectsClusterMismatch(t *testing.T) {
	t.Parallel()

	promotions := &recordingPromotionService{err: promotion.ErrIntegrationCluster}
	httpHandler := NewHTTP(
		Dependencies{
			ControllerIdentities: controllerIdentityFor("controller-token", "edge"),
			PromotionStages:      promotions,
		},
		HTTPOptions{},
	)
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPut,
		"/controller/v1/kargo/prod/stages",
		strings.NewReader(`{
          "snapshotId":"018f90ff-9fd8-7b5f-b7e1-f32ff54c1903",
          "observedAt":"2026-10-21T12:00:00Z",
          "stages":[]
        }`),
	)
	request.Header.Set("Authorization", "Bearer controller-token")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	httpHandler.ServeHTTP(response, request)

	assertProblem(t, response, http.StatusForbidden)
}

func TestSubmitKargoPromotionReturnsDurableState(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 21, 12, 0, 0, 0, time.UTC)
	promotionID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1904")
	promotions := &recordingPromotionService{
		record: promotion.Promotion{
			ID:                  promotionID,
			ExternalPromotionID: "external-dns-01jbe4q8",
			Status:              promotion.StatusPreparing,
			ReviewRequired:      true,
			UpdatedAt:           now,
		},
	}
	authentication := productAuthentication(
		productidentity.RoleOperator,
		productidentity.AuthenticationMethodAPIKey,
		[]productidentity.Scope{productidentity.ScopePromotionsSubmit},
	)
	authentication.Principal.Kind = productidentity.KindServiceAccount
	httpHandler := NewHTTP(
		Dependencies{
			PromotionRequests: promotions,
			ProductAuthentication: &recordingProductAuthenticationService{
				authentication: authentication,
			},
		},
		HTTPOptions{},
	)
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		"/api/v1/kargo/prod/promotions",
		strings.NewReader(`{
          "externalPromotionId":"external-dns-01jbe4q8",
          "project":"kargo-core",
          "stage":"external-dns-prod",
          "baseRevision":"8b7f1d217a6b0000000000000000000000000000",
          "candidateRevision":"f4cb21a88e1d0000000000000000000000000000",
          "candidateBranch":"kargo/promotion/external-dns-01jbe4q8"
        }`),
	)
	request.Header.Set("Authorization", "Bearer kargo-key")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	httpHandler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body)
	}
	if promotions.integration != "prod" || promotions.request.Project != "kargo-core" ||
		promotions.principal.ID != authentication.Principal.ID {
		t.Errorf("submission = %#v", promotions)
	}
	for _, expected := range []string{promotionID.String(), `"status":"preparing"`, `"reviewRequired":true`} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Errorf("response %q does not contain %q", response.Body.String(), expected)
		}
	}
}

func TestSubmitKargoPromotionMapsIneligibleStageToConflict(t *testing.T) {
	t.Parallel()

	promotions := &recordingPromotionService{err: promotion.ErrStageStale}
	authentication := productAuthentication(
		productidentity.RoleOperator,
		productidentity.AuthenticationMethodAPIKey,
		[]productidentity.Scope{productidentity.ScopePromotionsSubmit},
	)
	authentication.Principal.Kind = productidentity.KindServiceAccount
	httpHandler := NewHTTP(
		Dependencies{
			PromotionRequests: promotions,
			ProductAuthentication: &recordingProductAuthenticationService{
				authentication: authentication,
			},
		},
		HTTPOptions{},
	)
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		"/api/v1/kargo/prod/promotions",
		strings.NewReader(`{
          "externalPromotionId":"external-dns-01jbe4q8",
          "project":"kargo-core",
          "stage":"external-dns-prod",
          "baseRevision":"8b7f1d217a6b0000000000000000000000000000",
          "candidateRevision":"f4cb21a88e1d0000000000000000000000000000",
          "candidateBranch":"kargo/promotion/external-dns-01jbe4q8"
        }`),
	)
	request.Header.Set("Authorization", "Bearer kargo-key")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	httpHandler.ServeHTTP(response, request)

	assertProblem(t, response, http.StatusConflict)
}
