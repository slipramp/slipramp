package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/gate"
	"github.com/slipramp/slipramp/internal/productidentity"
)

type recordingGateOverrideStore struct{ request gate.OverrideRequest }

func (s *recordingGateOverrideStore) RecordGateOverride(_ context.Context, request gate.OverrideRequest) error {
	s.request = request
	return request.Validate()
}

func TestGateOverrideEndpoints(t *testing.T) {
	for _, tc := range []struct {
		path    string
		subject gate.SubjectType
		scope   productidentity.Scope
	}{
		{"runs", gate.RunSubject, productidentity.ScopeRunsOverrideGates},
		{"promotions", gate.PromotionSubject, productidentity.ScopePromotionsOverrideGates},
	} {
		t.Run(tc.path, func(t *testing.T) {
			id, process := uuid.New(), uuid.New()
			auth := productAuthentication(productidentity.RoleOperator, productidentity.AuthenticationMethodAPIKey, []productidentity.Scope{tc.scope})
			store := &recordingGateOverrideStore{}
			handler := NewHTTP(Dependencies{
				ProductAuthentication: &recordingProductAuthenticationService{authentication: auth},
				GateOverrides:         store, GateEvaluatorProcessID: process,
			}, HTTPOptions{})
			digest := strings.Repeat("a", 64)
			path := "/api/v1/" + tc.path + "/" + id.String() + "/gates/network-idle/override"
			body := `{"reason":"incident recovery","definitionDigest":"` + digest + `"}`
			request := httptest.NewRequestWithContext(t.Context(), http.MethodPut, path, strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer test-api-key")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusNoContent {
				t.Fatalf("override endpoint returned %d: %s", response.Code, response.Body)
			}
			got := store.request
			if got.Subject != tc.subject || got.SubjectID != id || got.GateName != "network-idle" ||
				got.DefinitionDigest != digest || got.Actor.PrincipalID != auth.Principal.ID ||
				got.EvaluatorProcessID != process || got.Reason != "incident recovery" || got.RecordedAt.IsZero() {
				t.Fatalf("override request = %#v", got)
			}
		})
	}
}
