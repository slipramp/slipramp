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
	"github.com/slipramp/slipramp/internal/notification"
	"github.com/slipramp/slipramp/internal/productidentity"
)

func TestNotificationDeliveryHistoryHTTPIsReadOnlyAndNonSensitive(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.October, 23, 18, 0, 0, 0, time.UTC)
	service := &recordingNotificationService{page: notification.DeliveryHistoryPage{
		Total: 4,
		Items: []notification.DeliveryHistoryItem{{
			ID:               uuid.MustParse("00000000-0000-0000-0000-000000000101"),
			EventID:          uuid.MustParse("00000000-0000-0000-0000-000000000102"),
			EventType:        notification.EventTypeReviewRequested,
			SubjectType:      notification.SubjectTypePromotion,
			SubjectID:        uuid.MustParse("00000000-0000-0000-0000-000000000103"),
			OccurredAt:       at.Add(-time.Minute),
			DestinationName:  "operations",
			DestinationType:  notification.DestinationTypeSlack,
			State:            notification.DeliveryStateFailed,
			AttemptCount:     5,
			LastError:        "provider temporarily unavailable",
			FirstAttemptedAt: new(at.Add(-50 * time.Second)),
			LastAttemptedAt:  new(at.Add(-10 * time.Second)),
			FailedAt:         new(at.Add(-10 * time.Second)),
			CreatedAt:        at.Add(-time.Minute),
		}},
	}}
	authentication := productAuthentication(
		productidentity.RoleViewer,
		productidentity.AuthenticationMethodAPIKey,
		[]productidentity.Scope{productidentity.ScopeRead},
	)
	httpHandler := NewHTTP(Dependencies{
		Notifications: service,
		ProductAuthentication: &recordingProductAuthenticationService{
			authentication: authentication,
		},
	}, HTTPOptions{})
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/api/v1/notification-deliveries?limit=1&offset=2",
		nil,
	)
	request.Header.Set("Authorization", "Bearer read-key")
	response := httptest.NewRecorder()

	httpHandler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if service.filter != (notification.DeliveryHistoryFilter{Limit: 1, Offset: 2}) {
		t.Fatalf("filter = %#v", service.filter)
	}
	var page api.NotificationDeliveryList
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 4 || page.Limit != 1 || page.Offset != 2 || len(page.Items) != 1 ||
		page.Items[0].DestinationName != "operations" || page.Items[0].AttemptCount != 5 {
		t.Fatalf("page = %#v", page)
	}
	for _, forbidden := range []string{"webhookUrl", "webhookUrlFile", "payload", "deduplication", "hooks.slack.com"} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatalf("response exposes %q: %s", forbidden, response.Body.String())
		}
	}
}

func TestNotificationDeliveryHistoryHTTPAuthorizationAndPagination(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		scope      productidentity.Scope
		query      string
		wantStatus int
	}{
		{name: "missing read scope", scope: productidentity.ScopeRunsWrite, wantStatus: http.StatusForbidden},
		{name: "invalid limit", scope: productidentity.ScopeRead, query: "?limit=201", wantStatus: http.StatusBadRequest},
		{name: "invalid offset", scope: productidentity.ScopeRead, query: "?offset=-1", wantStatus: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			service := &recordingNotificationService{}
			authentication := productAuthentication(
				productidentity.RoleViewer,
				productidentity.AuthenticationMethodAPIKey,
				[]productidentity.Scope{test.scope},
			)
			httpHandler := NewHTTP(Dependencies{
				Notifications: service,
				ProductAuthentication: &recordingProductAuthenticationService{
					authentication: authentication,
				},
			}, HTTPOptions{})
			request := httptest.NewRequestWithContext(
				t.Context(),
				http.MethodGet,
				"/api/v1/notification-deliveries"+test.query,
				nil,
			)
			request.Header.Set("Authorization", "Bearer key")
			response := httptest.NewRecorder()

			httpHandler.ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.wantStatus, response.Body.String())
			}
			if service.called {
				t.Fatal("unauthorized or invalid request reached the notification service")
			}
		})
	}
}

type recordingNotificationService struct {
	called bool
	filter notification.DeliveryHistoryFilter
	page   notification.DeliveryHistoryPage
}

func (s *recordingNotificationService) ListDeliveries(
	_ context.Context,
	filter notification.DeliveryHistoryFilter,
) (notification.DeliveryHistoryPage, error) {
	s.called = true
	s.filter = filter

	return s.page, nil
}
