package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/gittrigger"
	"github.com/slipramp/slipramp/internal/productidentity"
)

func TestFailedGitDeliveryHTTPPaginationAndAuthorization(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		scope      productidentity.Scope
		query      string
		wantStatus int
	}{
		{name: "read", scope: productidentity.ScopeRead, query: "?limit=1&offset=2", wantStatus: 200},
		{name: "missing read scope", scope: productidentity.ScopeRunsWrite, wantStatus: 403},
		{name: "limit too large", scope: productidentity.ScopeRead, query: "?limit=201", wantStatus: 400},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &recordingGitFailureService{}
			authentication := productAuthentication(productidentity.RoleViewer,
				productidentity.AuthenticationMethodAPIKey, []productidentity.Scope{test.scope})
			auth := &recordingProductAuthenticationService{authentication: authentication}
			httpHandler := NewHTTP(Dependencies{ProductAuthentication: auth, GitTriggerDeliveries: service}, HTTPOptions{})
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/git-trigger-deliveries/failed"+test.query, nil)
			request.Header.Set("Authorization", "Bearer test-read-key")
			response := httptest.NewRecorder()
			httpHandler.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status=%d body=%s", response.Code, response.Body)
			}
			if test.wantStatus != 200 {
				if service.called {
					t.Fatal("unauthorized or invalid request reached store")
				}
				return
			}
			limit, offset := 1, 2
			if service.limit != limit || service.offset != offset {
				t.Fatalf("store window=%d,%d", service.limit, service.offset)
			}
			var page api.FailedGitTriggerDeliveryList
			if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			if page.Limit != limit || page.Offset != offset || page.Total != 4 || len(page.Items) != 1 {
				t.Fatalf("page=%#v", page)
			}
			if page.Items[0].Target != "network" || page.Items[0].FailureCount != 8 {
				t.Fatalf("item=%#v", page.Items[0])
			}
			if strings.Contains(response.Body.String(), "secret") || strings.Contains(response.Body.String(), "parameters") {
				t.Fatal("unsafe details in response")
			}
		})
	}
}

type recordingGitFailureService struct {
	called bool
	limit  int
	offset int
}

func (s *recordingGitFailureService) ListFailedGitTriggerDeliveries(
	_ context.Context, limit int, offset int,
) (gittrigger.FailedDeliveryPage, error) {
	s.called = true
	s.limit, s.offset = limit, offset
	now := time.Now().UTC()
	return gittrigger.FailedDeliveryPage{Total: 4, Items: []gittrigger.FailedDelivery{{
		ID: uuid.New(), RepositoryID: uuid.New(), RepositoryName: "infrastructure", CatalogRevisionID: uuid.New(),
		CommitSHA: strings.Repeat("a", 40), TargetName: "network", ActionName: "sync", TriggerName: "on-change",
		FailureCount: 8, FailedAt: now,
		Diagnostic: gittrigger.FailureDiagnostic(errors.New("postgres://admin:secret@db")),
	}}}, nil
}
