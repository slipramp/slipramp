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

type recordingPromotionReviewService struct {
	stageFilter     promotion.StageListFilter
	promotionFilter promotion.PromotionListFilter
	policyStageID   uuid.UUID
	policyUpdate    promotion.PolicyUpdate
	decision        promotion.DecisionRequest
	stages          promotion.StageReviewList
	promotions      promotion.PromotionReviewList
	review          promotion.PromotionReview
	policy          promotion.PolicyView
	err             error
}

func (s *recordingPromotionReviewService) ExpirePending(context.Context) error {
	return s.err
}

func (s *recordingPromotionReviewService) ListStages(
	_ context.Context,
	filter promotion.StageListFilter,
) (promotion.StageReviewList, error) {
	s.stageFilter = filter

	return s.stages, s.err
}

func (s *recordingPromotionReviewService) UpdateStagePolicy(
	_ context.Context,
	stageID uuid.UUID,
	update promotion.PolicyUpdate,
	_ productidentity.Principal,
) (promotion.PolicyView, error) {
	s.policyStageID = stageID
	s.policyUpdate = update

	return s.policy, s.err
}

func (s *recordingPromotionReviewService) ListPromotions(
	_ context.Context,
	filter promotion.PromotionListFilter,
) (promotion.PromotionReviewList, error) {
	s.promotionFilter = filter

	return s.promotions, s.err
}

func (s *recordingPromotionReviewService) GetPromotion(
	context.Context,
	uuid.UUID,
) (promotion.PromotionReview, error) {
	return s.review, s.err
}

func (s *recordingPromotionReviewService) Decide(
	_ context.Context,
	request promotion.DecisionRequest,
) (promotion.PromotionReview, error) {
	s.decision = request

	return s.review, s.err
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

func TestListPromotionStagesUsesScopeFilters(t *testing.T) {
	t.Parallel()

	reviews := &recordingPromotionReviewService{
		stages: promotion.StageReviewList{Items: []promotion.StageReview{}, Total: 0, Limit: 10, Offset: 2},
	}
	httpHandler := NewHTTP(
		Dependencies{
			PromotionReviews: reviews,
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
		"/api/v1/promotion-stages?integration=prod&project=kargo-core&application=external-dns&cluster=colo&archived=false&limit=10&offset=2",
		nil,
	)
	request.Header.Set("Authorization", "Bearer viewer-key")
	response := httptest.NewRecorder()

	httpHandler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body)
	}
	if reviews.stageFilter.Integration != "prod" || reviews.stageFilter.Project != "kargo-core" ||
		reviews.stageFilter.Application != "external-dns" || reviews.stageFilter.Cluster != "colo" ||
		reviews.stageFilter.Archived == nil || *reviews.stageFilter.Archived ||
		reviews.stageFilter.Limit != 10 || reviews.stageFilter.Offset != 2 {
		t.Errorf("Stage filter = %#v", reviews.stageFilter)
	}
}

func TestListPromotionsUsesOperatorFilters(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 21, 12, 0, 0, 0, time.UTC)
	promotionID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1904")
	stageID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1905")
	reviews := &recordingPromotionReviewService{
		promotions: promotion.PromotionReviewList{
			Items: []promotion.PromotionReview{{Promotion: promotion.Promotion{
				ID:                     promotionID,
				Integration:            "prod",
				ExternalPromotionID:    "external-dns-01jbe4q8",
				StageID:                stageID,
				StageSnapshot:          []byte(`{"project":"kargo-core","name":"external-dns-prod","application":"external-dns"}`),
				RepositoryID:           uuid.New(),
				RepositorySnapshot:     []byte(`{"name":"ops"}`),
				BaseRevision:           "8b7f1d217a6b0000000000000000000000000000",
				CandidateRevision:      "f4cb21a88e1d0000000000000000000000000000",
				CandidateBranch:        "kargo/promotion/external-dns-01jbe4q8",
				TargetBranch:           "stage/external-dns/prod",
				PolicySnapshot:         []byte(`{"effectiveMode":"review-required"}`),
				ReviewRequired:         true,
				Status:                 promotion.StatusPending,
				RequestedBySnapshot:    productidentity.ActorSnapshot{PrincipalID: uuid.New(), Kind: productidentity.KindServiceAccount, DisplayName: "Kargo"},
				RequestedByPrincipalID: uuid.New(),
				CreatedAt:              now,
				UpdatedAt:              now,
			}}},
			Total:  1,
			Limit:  10,
			Offset: 2,
		},
	}
	// The response snapshot is immutable and independently validated by the
	// domain store. Keep the actor IDs aligned for the API conversion.
	record := &reviews.promotions.Items[0].Promotion
	record.RequestedByPrincipalID = record.RequestedBySnapshot.PrincipalID
	httpHandler := NewHTTP(
		Dependencies{
			PromotionReviews: reviews,
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
		"/api/v1/promotions?status=pending&integration=prod&cluster=colo&project=kargo-core&application=external-dns&stageId="+
			stageID.String()+"&limit=10&offset=2",
		nil,
	)
	request.Header.Set("Authorization", "Bearer viewer-key")
	response := httptest.NewRecorder()

	httpHandler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body)
	}
	if reviews.promotionFilter.Status != promotion.StatusPending ||
		reviews.promotionFilter.Integration != "prod" || reviews.promotionFilter.Cluster != "colo" ||
		reviews.promotionFilter.Project != "kargo-core" ||
		reviews.promotionFilter.Application != "external-dns" || reviews.promotionFilter.StageID == nil ||
		*reviews.promotionFilter.StageID != stageID || reviews.promotionFilter.Limit != 10 ||
		reviews.promotionFilter.Offset != 2 {
		t.Errorf("Promotion filter = %#v", reviews.promotionFilter)
	}
	for _, expected := range []string{
		promotionID.String(),
		`"total":1`,
		`"status":"pending"`,
		`"kargoUrl":"https://kargo.example.com/project/kargo-core/promotion/external-dns-01jbe4q8"`,
	} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Errorf("response %q does not contain %q", response.Body.String(), expected)
		}
	}
}

func TestDecidePromotionUsesAuthenticatedActor(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 21, 12, 0, 0, 0, time.UTC)
	promotionID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1904")
	actor := productidentity.ActorSnapshot{
		PrincipalID: uuid.New(),
		Kind:        productidentity.KindUser,
		DisplayName: "Review operator",
	}
	comment := "Validated the RBAC migration"
	reviews := &recordingPromotionReviewService{
		review: promotion.PromotionReview{
			Promotion: promotion.Promotion{
				ID:                     promotionID,
				Integration:            "prod",
				ExternalPromotionID:    "external-dns-01jbe4q8",
				StageID:                uuid.New(),
				StageSnapshot:          []byte(`{"name":"external-dns-prod"}`),
				RepositoryID:           uuid.New(),
				RepositorySnapshot:     []byte(`{"name":"ops"}`),
				BaseRevision:           "8b7f1d217a6b0000000000000000000000000000",
				CandidateRevision:      "f4cb21a88e1d0000000000000000000000000000",
				CandidateBranch:        "kargo/promotion/external-dns-01jbe4q8",
				TargetBranch:           "stage/external-dns/prod",
				PolicySnapshot:         []byte(`{"effectiveMode":"review-required"}`),
				ReviewRequired:         true,
				Status:                 promotion.StatusApproved,
				RequestedByPrincipalID: uuid.New(),
				RequestedBySnapshot:    productidentity.ActorSnapshot{PrincipalID: uuid.New(), Kind: productidentity.KindServiceAccount, DisplayName: "Kargo"},
				CreatedAt:              now,
				UpdatedAt:              now,
			},
			Decision: &promotion.Decision{
				PromotionID:          promotionID,
				Decision:             promotion.DecisionApproved,
				Comment:              &comment,
				DecidedByPrincipalID: actor.PrincipalID,
				DecidedBySnapshot:    actor,
				DecidedAt:            now,
			},
		},
	}
	reviews.review.Promotion.RequestedByPrincipalID = reviews.review.Promotion.RequestedBySnapshot.PrincipalID
	authentication := productAuthentication(
		productidentity.RoleOperator,
		productidentity.AuthenticationMethodAPIKey,
		[]productidentity.Scope{productidentity.ScopePromotionsWrite},
	)
	httpHandler := NewHTTP(
		Dependencies{
			PromotionReviews: reviews,
			ProductAuthentication: &recordingProductAuthenticationService{
				authentication: authentication,
			},
		},
		HTTPOptions{},
	)
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPut,
		"/api/v1/promotions/"+promotionID.String()+"/decision",
		strings.NewReader(`{"decision":"approved","comment":"Validated the RBAC migration"}`),
	)
	request.Header.Set("Authorization", "Bearer operator-key")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	httpHandler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body)
	}
	if reviews.decision.Principal.ID != authentication.Principal.ID ||
		reviews.decision.Decision != promotion.DecisionApproved || reviews.decision.Comment == nil ||
		*reviews.decision.Comment != comment {
		t.Errorf("decision request = %#v", reviews.decision)
	}
}
