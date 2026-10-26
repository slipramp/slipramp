package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/inbox"
	"github.com/slipramp/slipramp/internal/productidentity"
)

func TestGetInboxMapsSnapshotAndPermissions(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.October, 23, 18, 0, 0, 0, time.UTC)
	problemID := strings.Repeat("a", 64)
	service := &recordingInboxService{snapshot: inbox.Snapshot{
		GeneratedAt: at,
		Decisions: []inbox.Item{{
			ID:          "promotion-review:00000000-0000-0000-0000-000000000010",
			Kind:        inbox.KindPromotionReview,
			Title:       "Promotion awaits review",
			Description: "api · candidate → main",
			OccurredAt:  at.Add(-time.Minute),
			ActionLabel: "Review",
			Destination: inbox.Destination{
				Type: inbox.DestinationPromotion,
				ID:   "00000000-0000-0000-0000-000000000010",
			},
		}},
		Problems: []inbox.Item{{
			ID:          problemID,
			Kind:        inbox.KindRepositoryRefresh,
			Title:       "infrastructure could not refresh",
			Description: "Target definition is invalid.",
			OccurredAt:  at.Add(-2 * time.Minute),
			ActionLabel: "View repository",
			Destination: inbox.Destination{
				Type: inbox.DestinationRepository,
				ID:   "00000000-0000-0000-0000-000000000020",
			},
		}},
		Activity: inbox.Activity{Queued: 1, Starting: 2, Running: 3},
	}}
	authentication := inboxHandlerAuthentication(
		productidentity.RoleAdmin,
		[]productidentity.Scope{productidentity.ScopeAdmin},
	)
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/inbox", nil)
	request.Header.Set("Authorization", "Bearer inbox-key")
	response := httptest.NewRecorder()

	inboxHTTPHandler(service, authentication).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	var body api.Inbox
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if service.query.PrincipalID != authentication.Principal.ID ||
		!service.query.IncludePromotionDecisions || !service.query.IncludeRunDecisions {
		t.Errorf("query = %#v", service.query)
	}
	if !body.GeneratedAt.Equal(at) || !body.Capabilities.AcknowledgeProblems ||
		body.Activity != (api.InboxActivity{Queued: 1, Starting: 2, Running: 3}) {
		t.Errorf("Inbox response = %#v", body)
	}
	if len(body.Decisions) != 1 || body.Decisions[0].Kind != api.InboxItemKindPromotionReview ||
		len(body.Problems) != 1 || body.Problems[0].ID != problemID {
		t.Errorf("Inbox items = decisions %#v, problems %#v", body.Decisions, body.Problems)
	}
}

func TestGetInboxDecisionVisibilityMatchesPermissions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		scopes           []productidentity.Scope
		wantPromotions   bool
		wantRunDecisions bool
	}{
		{
			name:   "read only",
			scopes: []productidentity.Scope{productidentity.ScopeRead},
		},
		{
			name: "Run write does not imply Run decisions",
			scopes: []productidentity.Scope{
				productidentity.ScopeRead,
				productidentity.ScopeRunsWrite,
			},
		},
		{
			name: "Run decisions",
			scopes: []productidentity.Scope{
				productidentity.ScopeRead,
				productidentity.ScopeRunsReview,
			},
			wantRunDecisions: true,
		},
		{
			name: "Promotion decisions",
			scopes: []productidentity.Scope{
				productidentity.ScopeRead,
				productidentity.ScopePromotionsWrite,
			},
			wantPromotions: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			service := &recordingInboxService{snapshot: inbox.Snapshot{}}
			authentication := inboxHandlerAuthentication(productidentity.RoleOperator, test.scopes)
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/inbox", nil)
			request.Header.Set("Authorization", "Bearer inbox-key")
			response := httptest.NewRecorder()

			inboxHTTPHandler(service, authentication).ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
			}
			if service.query.IncludePromotionDecisions != test.wantPromotions ||
				service.query.IncludeRunDecisions != test.wantRunDecisions {
				t.Errorf("query = %#v", service.query)
			}
		})
	}
}

func TestAcknowledgeInboxProblemsPassesPrincipalAndBatch(t *testing.T) {
	t.Parallel()

	problemIDs := []string{strings.Repeat("a", 64), strings.Repeat("b", 64)}
	service := &recordingInboxService{}
	authentication := inboxHandlerAuthentication(
		productidentity.RoleOperator,
		[]productidentity.Scope{productidentity.ScopeInboxWrite},
	)
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		"/api/v1/inbox/problem-acknowledgements",
		strings.NewReader(`{"problemIds":["`+problemIDs[0]+`","`+problemIDs[1]+`"]}`),
	)
	request.Header.Set("Authorization", "Bearer inbox-key")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	inboxHTTPHandler(service, authentication).ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusNoContent, response.Body.String())
	}
	if service.principalID != authentication.Principal.ID ||
		len(service.problemIDs) != 2 || service.problemIDs[0] != problemIDs[0] || service.problemIDs[1] != problemIDs[1] {
		t.Errorf("principal = %s, IDs = %v", service.principalID, service.problemIDs)
	}
}

func TestAcknowledgeInboxProblemsRejectsInvalidRequests(t *testing.T) {
	t.Parallel()

	authentication := inboxHandlerAuthentication(
		productidentity.RoleOperator,
		[]productidentity.Scope{productidentity.ScopeInboxWrite},
	)
	tests := []struct {
		name    string
		body    string
		service *recordingInboxService
	}{
		{name: "missing body", service: &recordingInboxService{}},
		{
			name:    "invalid batch",
			body:    `{"problemIds":["` + strings.Repeat("a", 64) + `"]}`,
			service: &recordingInboxService{acknowledgeErr: inbox.ErrInvalidAcknowledgement},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequestWithContext(
				t.Context(),
				http.MethodPost,
				"/api/v1/inbox/problem-acknowledgements",
				strings.NewReader(test.body),
			)
			request.Header.Set("Authorization", "Bearer inbox-key")
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()

			inboxHTTPHandler(test.service, authentication).ServeHTTP(response, request)

			assertProblem(t, response, http.StatusBadRequest)
		})
	}
}

type recordingInboxService struct {
	snapshot       inbox.Snapshot
	loadErr        error
	acknowledgeErr error
	query          inbox.Query
	principalID    uuid.UUID
	problemIDs     []string
}

func (s *recordingInboxService) Load(_ context.Context, query inbox.Query) (inbox.Snapshot, error) {
	s.query = query
	return s.snapshot, s.loadErr
}

func (s *recordingInboxService) AcknowledgeProblems(
	_ context.Context,
	principalID uuid.UUID,
	problemIDs []string,
) error {
	s.principalID = principalID
	s.problemIDs = append([]string(nil), problemIDs...)
	return s.acknowledgeErr
}

func inboxHTTPHandler(service InboxService, authentication productidentity.Authentication) http.Handler {
	return NewHTTP(
		Dependencies{
			Inbox: service,
			ProductAuthentication: &recordingProductAuthenticationService{
				authentication: authentication,
			},
		},
		HTTPOptions{},
	)
}

func inboxHandlerAuthentication(
	role productidentity.Role,
	scopes []productidentity.Scope,
) productidentity.Authentication {
	at := time.Date(2026, time.October, 23, 17, 0, 0, 0, time.UTC)
	return productidentity.Authentication{
		Principal: productidentity.Principal{
			ID:          uuid.MustParse("00000000-0000-0000-0000-000000000901"),
			Kind:        productidentity.KindUser,
			DisplayName: "Inbox test operator",
			Role:        role,
			CreatedAt:   at,
			UpdatedAt:   at,
		},
		Method:          productidentity.AuthenticationMethodAPIKey,
		CredentialID:    uuid.MustParse("00000000-0000-0000-0000-000000000902"),
		CredentialLabel: "Inbox test key",
		Scopes:          scopes,
	}
}
