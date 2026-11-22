package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/gate"
	"github.com/slipramp/slipramp/internal/productidentity"
)

type recordingGateOverrideStore struct {
	request gate.OverrideRequest
	repoID  uuid.UUID
	list    gate.OverrideCollection
	listErr error
}

func (s *recordingGateOverrideStore) ListGateOverrides(
	_ context.Context, kind gate.SubjectType, id, process uuid.UUID,
) (gate.OverrideCollection, error) {
	s.request = gate.OverrideRequest{Subject: kind, SubjectID: id, EvaluatorProcessID: process}
	return s.list, s.listErr
}

func (s *recordingGateOverrideStore) RecordGateOverride(_ context.Context, request gate.OverrideRequest) (gate.Requirement, error) {
	s.request = request
	if err := request.Validate(); err != nil {
		return gate.Requirement{}, err
	}
	return gate.Requirement{
		Reference: request.Reference, Origin: gate.RepositoryOrigin, Scope: gate.Scope{Kind: gate.RepositoryScope, ID: s.repoID},
		Name: "network-idle", Type: gate.PrometheusType, State: gate.RequirementBlocking, Evidence: gate.PrometheusSample(2),
		Overrides: []gate.Override{{
			Reference: request.Reference, Reason: request.Reason, Actor: request.Actor,
			RecordedAt: request.RecordedAt, ObservedState: gate.RequirementBlocking,
		}},
	}, nil
}

func serveGateRequest(t *testing.T, handler http.Handler, method, path, body string, status int) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")
	out := httptest.NewRecorder()
	handler.ServeHTTP(out, req)
	if out.Code != status {
		t.Fatalf("%s %s returned %d, want %d: %s", method, path, out.Code, status, out.Body)
	}
	return out
}

func decodeGateResponse[T any](t *testing.T, out *httptest.ResponseRecorder) T {
	t.Helper()
	var result T
	if err := json.Unmarshal(out.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestGateOverrideEndpoints(t *testing.T) {
	for _, tc := range []struct {
		subject gate.SubjectType
		scope   productidentity.Scope
	}{
		{gate.RunSubject, productidentity.ScopeRunsOverrideGates},
		{gate.PromotionSubject, productidentity.ScopePromotionsOverrideGates},
	} {
		t.Run(string(tc.subject), func(t *testing.T) {
			id, process := uuid.New(), uuid.New()
			auth := productAuthentication(productidentity.RoleOperator, productidentity.AuthenticationMethodAPIKey, []productidentity.Scope{tc.scope})
			authService := &recordingProductAuthenticationService{authentication: auth}
			store := &recordingGateOverrideStore{repoID: uuid.New()}
			handler := NewHTTP(Dependencies{
				ProductAuthentication: authService, GateOverrides: store, GateEvaluatorProcessID: process,
			}, HTTPOptions{})
			path := "/api/v1/" + string(tc.subject) + "s/" + id.String() + "/gate-overrides"
			digest := strings.Repeat("a", 64)
			body := `{"reason":"incident recovery","key":"repository/network-idle","version":"` + digest + `"}`
			out := serveGateRequest(t, handler, http.MethodPost, path, body, http.StatusOK)
			result := decodeGateResponse[api.GateRequirement](t, out)
			if result.Key != "repository/network-idle" || result.Version == nil || *result.Version != digest ||
				result.Type == nil || *result.Type != gate.PrometheusType || result.Scope.ID != store.repoID ||
				string(result.Origin) != "repository" || string(result.Scope.Kind) != "repository" ||
				result.Overrides == nil || len(*result.Overrides) != 1 || (*result.Overrides)[0].Key != result.Key ||
				(*result.Overrides)[0].Version != digest || result.Evidence.PrometheusValue() == nil {
				t.Fatalf("override projection = %#v", result)
			}
			got := store.request
			if got.Subject != tc.subject || got.SubjectID != id || got.Key != "repository/network-idle" ||
				got.Version != gate.Version(digest) || got.Actor.PrincipalID != auth.Principal.ID ||
				got.EvaluatorProcessID != process || got.Reason != "incident recovery" || got.RecordedAt.IsZero() {
				t.Fatalf("override request = %#v", got)
			}
			if tc.subject == gate.RunSubject {
				for _, invalid := range []string{
					`{"key":"lock/no-such-id","version":"1","reason":"invalid scope"}`,
					`{"key":"repository/network-idle","reason":"missing version"}`,
					`{"key":"repository/network-idle","version":"v1","reason":" "}`,
				} {
					serveGateRequest(t, handler, http.MethodPost, path, invalid, http.StatusBadRequest)
				}
			}

			actor, err := auth.Principal.ActorSnapshot()
			if err != nil {
				t.Fatal(err)
			}
			scopeID := uuid.New()
			lock := &gate.Evidence{Payload: &gate.ScopeLock{Actor: actor, Reason: "maintenance", CreatedAt: got.RecordedAt}}
			check := gate.Requirement{Key: gate.LockKey(scopeID), Version: "3", Origin: gate.ServerOrigin,
				Type: gate.LockType, Name: "global-lock", Scope: gate.Scope{ID: scopeID, Kind: gate.GlobalScope},
				State: gate.RequirementBlocking, Evidence: lock}
			history := gate.Override{
				Key: check.Key, Version: "1", Actor: actor, RecordedAt: got.RecordedAt,
				Reason: "incident response", ObservedState: gate.RequirementBlocking, ObservedEvidence: lock,
			}
			store.list = gate.OverrideCollection{Eligible: []gate.Requirement{check}, Decisions: []gate.Override{history}}
			authService.authentication = productAuthentication(productidentity.RoleViewer, productidentity.AuthenticationMethodAPIKey,
				[]productidentity.Scope{productidentity.ScopeRead})
			out = serveGateRequest(t, handler, http.MethodGet, path, "", http.StatusOK)
			list := decodeGateResponse[api.GateOverrideCollection](t, out)
			if store.request.Subject != tc.subject || store.request.SubjectID != id || store.request.EvaluatorProcessID != process ||
				len(list.Eligible) != 1 || list.Eligible[0].Key != string(check.Key) || list.Eligible[0].Scope.ID != scopeID ||
				len(list.Decisions) != 1 || list.Decisions[0].Version != "1" || list.Decisions[0].ObservedEvidence == nil {
				t.Fatalf("list = %#v, request = %#v", list, store.request)
			}
			observed, ok := list.Decisions[0].ObservedEvidence.Payload.(*gate.ScopeLock)
			if !ok || observed.Actor != actor || observed.Reason != "maintenance" {
				t.Fatalf("observed lock = %#v", observed)
			}
			serveGateRequest(t, handler, http.MethodPost, path, body, http.StatusForbidden)
			store.listErr = gate.ErrSubjectNotFound
			serveGateRequest(t, handler, http.MethodGet, path, "", http.StatusNotFound)
		})
	}
}
