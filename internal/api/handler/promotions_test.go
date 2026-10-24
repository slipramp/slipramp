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

type recordingPromotionCreationService struct {
	request promotion.OperatorPromotionRequest
	record  promotion.Promotion
	err     error
}

func (s *recordingPromotionCreationService) Create(
	_ context.Context,
	request promotion.OperatorPromotionRequest,
) (promotion.Promotion, error) {
	s.request = request

	return s.record, s.err
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
            "requestedFreight":[],
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

func TestCreatePromotionReturnsDurableCreatingState(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 23, 12, 0, 0, 0, time.UTC)
	promotionID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1930")
	targetStageID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1931")
	sourceStageID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1932")
	authentication := productAuthentication(
		productidentity.RoleOperator,
		productidentity.AuthenticationMethodWebSession,
		nil,
	)
	actor, err := authentication.Principal.ActorSnapshot()
	if err != nil {
		t.Fatalf("ActorSnapshot() error = %v", err)
	}
	nextDispatchAt := now
	promotions := &recordingPromotionCreationService{
		record: promotion.Promotion{
			ID:                      promotionID,
			Source:                  promotion.SourceSlipramp,
			Integration:             "prod",
			ExternalPromotionID:     "external-dns-prod-slipramp-" + promotionID.String(),
			StageID:                 targetStageID,
			SourceStageID:           &sourceStageID,
			StageSnapshot:           []byte(`{"project":"kargo-core","name":"external-dns-prod"}`),
			RepositoryID:            uuid.New(),
			RepositorySnapshot:      []byte(`{"name":"ops"}`),
			TargetBranch:            "stage/external-dns/prod",
			PolicySnapshot:          []byte(`{"effectiveMode":"automatic"}`),
			SelectedFreightSnapshot: []byte(`{"origin":"Warehouse/external-dns","name":"freight-42","artifacts":[]}`),
			Status:                  promotion.StatusCreating,
			NextDispatchAt:          &nextDispatchAt,
			RequestedByPrincipalID:  authentication.Principal.ID,
			RequestedBySnapshot:     actor,
			CreatedAt:               now,
			UpdatedAt:               now,
		},
	}
	httpHandler := NewHTTP(
		Dependencies{
			PromotionCreations: promotions,
			ProductAuthentication: &recordingProductAuthenticationService{
				authentication: authentication,
			},
		},
		HTTPOptions{},
	)
	request := authenticatedProductRequest(
		http.MethodPost,
		"/api/v1/promotion-stages/"+targetStageID.String()+"/promotions",
		`{"sourceStageId":"`+sourceStageID.String()+`","freightName":"freight-42"}`,
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	httpHandler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusCreated, response.Body)
	}
	if response.Header().Get("Location") != "/api/v1/promotions/"+promotionID.String() {
		t.Errorf("Location = %q", response.Header().Get("Location"))
	}
	if promotions.request.TargetStageID != targetStageID || promotions.request.SourceStageID != sourceStageID ||
		promotions.request.FreightName != "freight-42" ||
		promotions.request.Principal.ID != authentication.Principal.ID {
		t.Errorf("creation request = %#v", promotions.request)
	}
	for _, expected := range []string{promotionID.String(), `"source":"slipramp"`, `"status":"creating"`} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Errorf("response %q does not contain %q", response.Body.String(), expected)
		}
	}
	if strings.Contains(response.Body.String(), `"kargoUrl"`) {
		t.Errorf("creating Promotion response enabled Kargo link: %s", response.Body)
	}
}

func TestOperatorPromotionKargoLinkRequiresSuccessfulDispatch(t *testing.T) {
	t.Parallel()

	record := promotion.Promotion{
		Source:              promotion.SourceSlipramp,
		Integration:         "prod",
		ExternalPromotionID: "external-dns-prod-slipramp-018f90ff-9fd8-7b5f-b7e1-f32ff54c1930",
		StageSnapshot:       []byte(`{"project":"kargo-core","name":"external-dns-prod"}`),
		RepositorySnapshot:  []byte(`{"name":"ops"}`),
		PolicySnapshot:      []byte(`{"effectiveMode":"review-required"}`),
		Status:              promotion.StatusPreparing,
		RequestedBySnapshot: productidentity.ActorSnapshot{
			PrincipalID: uuid.New(),
			Kind:        productidentity.KindUser,
			DisplayName: "Operator",
		},
	}
	record.RequestedByPrincipalID = record.RequestedBySnapshot.PrincipalID
	integrations := promotion.IntegrationSet{"prod": {UIURL: "https://kargo.example.com"}}

	body, err := promotionResponse(promotion.PromotionReview{Promotion: record}, nil, integrations)
	if err != nil {
		t.Fatalf("promotionResponse() error = %v", err)
	}
	if body.KargoURL != nil {
		t.Errorf("undispatched operator Promotion KargoURL = %q", *body.KargoURL)
	}

	uid := "promotion-uid"
	record.KargoPromotionUID = &uid
	body, err = promotionResponse(promotion.PromotionReview{Promotion: record}, nil, integrations)
	if err != nil {
		t.Fatalf("dispatched promotionResponse() error = %v", err)
	}
	if body.KargoURL == nil || *body.KargoURL !=
		"https://kargo.example.com/project/kargo-core/promotion/"+record.ExternalPromotionID {
		t.Errorf("dispatched operator Promotion KargoURL = %v", body.KargoURL)
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
				Source:                 promotion.SourceKargo,
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
