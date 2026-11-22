package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/gate"
	"github.com/slipramp/slipramp/internal/productidentity"
)

type recordingGateScopeStore struct {
	scope gate.Scope
	actor productidentity.ActorSnapshot
	err   error
}

func (s *recordingGateScopeStore) GetServerGateScope(_ context.Context, owner gate.Scope) (gate.ScopeState, error) {
	s.scope = owner
	return gate.ScopeState{ID: uuid.New(), Owner: owner, CheckedAt: time.Now().UTC()}, s.err
}

func (s *recordingGateScopeStore) ChangeServerGateLock(
	_ context.Context, owner gate.Scope,
	actor productidentity.ActorSnapshot, request *gate.LockRequest,
) (gate.ScopeState, error) {
	s.scope, s.actor = owner, actor
	at := time.Now().UTC()
	state := gate.ScopeState{ID: uuid.New(), Owner: owner, CheckedAt: at, LockVersion: 1}
	if request != nil {
		state.Lock = &gate.ScopeLock{Reason: request.Reason, ExpiresAt: request.ExpiresAt, Actor: actor, CreatedAt: at}
		state.Events = []gate.LockEvent{{Action: "lock", Version: 1, Actor: actor, Reason: request.Reason, RecordedAt: at}}
	}
	return state, s.err
}

func (s *recordingGateScopeStore) ChangeServerGateWindow(_ context.Context, owner gate.Scope, actor productidentity.ActorSnapshot, w *gate.WeeklyWindow) (gate.ScopeState, error) {
	now := time.Now().UTC()
	state := gate.ScopeState{ID: uuid.New(), Owner: owner, WindowVersion: 1, CheckedAt: now}
	if w != nil {
		schedule, err := w.Compile()
		if err != nil {
			return gate.ScopeState{}, err
		}
		o := schedule.Evaluate(now)
		state.Window = &gate.ScopeWindow{Definition: schedule.Definition, Observation: o, State: o.State}
	}
	s.scope, s.actor = owner, actor
	return state, s.err
}

func TestServerGateScopeEndpoints(t *testing.T) {
	global := gate.Scope{Kind: gate.GlobalScope}
	target := gate.Scope{Kind: gate.TargetScope, ID: uuid.New()}
	stage := gate.Scope{Kind: gate.StageScope, ID: uuid.New()}
	manage, read := productidentity.ScopeGatesManage, productidentity.ScopeRead
	viewer, operator, admin := productidentity.RoleViewer, productidentity.RoleOperator, productidentity.RoleAdmin
	put, remove, get := http.MethodPut, http.MethodDelete, http.MethodGet
	const lockBody = `{"reason":"  scheduled outage  "}`
	const windowBody = `{"timeZone":"America/Denver","weekly":[{"days":["monday"],"start":"09:00","end":"17:00"}]}`
	for _, tc := range []struct {
		name, method string
		owner        gate.Scope
		policy       string
		role         productidentity.Role
		scope        productidentity.Scope
		body         string
		storeErr     error
		status       int
	}{
		{"inspect global", get, global, "", viewer, read, "", nil, http.StatusOK},
		{"inspect Target", get, target, "", viewer, read, "", nil, http.StatusOK},
		{"inspect Stage", get, stage, "", viewer, read, "", nil, http.StatusOK},
		{"missing scope", get, target, "", viewer, read, "", gate.ErrScopeNotFound, http.StatusNotFound},
		{"manage scope cannot inspect", get, global, "", admin, manage, "", nil, http.StatusForbidden},
		{"admin locks global", put, global, "lock", admin, manage, lockBody, nil, http.StatusOK},
		{"admin unlocks global", remove, global, "lock", admin, manage, "", nil, http.StatusOK},
		{"operator locks Target", put, target, "lock", operator, manage, lockBody, nil, http.StatusOK},
		{"operator unlocks Target", remove, target, "lock", operator, manage, "", nil, http.StatusOK},
		{"operator locks Stage", put, stage, "lock", operator, manage, lockBody, nil, http.StatusOK},
		{"operator unlocks Stage", remove, stage, "lock", operator, manage, "", nil, http.StatusOK},
		{"operator cannot lock global", put, global, "lock", operator, manage, lockBody, nil, http.StatusForbidden},
		{"admin key cannot elevate operator", remove, global, "lock", operator, productidentity.ScopeAdmin, "", nil, http.StatusForbidden},
		{"viewer cannot lock Stage", put, stage, "lock", viewer, manage, lockBody, nil, http.StatusForbidden},
		{"override scope cannot manage", put, target, "lock", admin, productidentity.ScopeRunsOverrideGates, lockBody, nil, http.StatusForbidden},
		{"blank reason", put, global, "lock", admin, manage, `{"reason":" "}`, nil, http.StatusBadRequest},
		{"oversized reason", put, target, "lock", operator, manage, `{"reason":"` + strings.Repeat("é", 1025) + `"}`, nil, http.StatusBadRequest},
		{"misspelled expiry", put, global, "lock", admin, manage, `{"reason":"hold","expireAt":"2027-01-01T12:00:00Z"}`, nil, http.StatusBadRequest},
		{"expired lock", put, stage, "lock", operator, manage, lockBody, gate.ErrLockExpiry, http.StatusBadRequest},
		{"conflicting lock", put, target, "lock", operator, manage, lockBody, gate.ErrLockConflict, http.StatusConflict},
		{"admin sets global window", put, global, "window", admin, manage, windowBody, nil, http.StatusOK},
		{"admin removes global window", remove, global, "window", admin, manage, "", nil, http.StatusOK},
		{"operator sets Target window", put, target, "window", operator, manage, windowBody, nil, http.StatusOK},
		{"operator removes Target window", remove, target, "window", operator, manage, "", nil, http.StatusOK},
		{"operator sets Stage window", put, stage, "window", operator, manage, windowBody, nil, http.StatusOK},
		{"operator removes Stage window", remove, stage, "window", operator, manage, "", nil, http.StatusOK},
		{"read scope cannot remove window", remove, stage, "window", operator, read, "", nil, http.StatusForbidden},
		{"invalid time zone", put, target, "window", operator, manage, strings.Replace(windowBody, "America/Denver", "Local", 1), gate.ErrInvalidWindow, http.StatusBadRequest},
		{"misspelled interval start", put, stage, "window", operator, manage, strings.Replace(windowBody, "start", "starts", 1), nil, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &recordingGateScopeStore{err: tc.storeErr}
			auth := productAuthentication(tc.role, productidentity.AuthenticationMethodAPIKey, []productidentity.Scope{tc.scope})
			handler := NewHTTP(Dependencies{GateScopes: store,
				ProductAuthentication: &recordingProductAuthenticationService{authentication: auth}}, HTTPOptions{})
			path := "/api/v1/gate-scopes/" + string(tc.owner.Kind)
			if tc.owner != global {
				path += "s/" + tc.owner.ID.String()
			}
			if tc.policy != "" {
				path += "/" + tc.policy
			}
			out := serveGateRequest(t, handler, tc.method, path, tc.body, tc.status)
			if tc.status != http.StatusOK {
				if tc.storeErr == nil && store.scope.Kind != "" {
					t.Fatal("rejected operation reached the store")
				}
				return
			}
			result := decodeGateResponse[api.ServerGateScope](t, out)
			if store.scope != tc.owner || string(result.Kind) != string(tc.owner.Kind) || result.Events == nil ||
				result.WindowEvents == nil || result.ID == uuid.Nil || result.CheckedAt.IsZero() {
				t.Fatalf("scope response = %#v", result)
			}
			if tc.owner == target && (result.TargetID == nil || *result.TargetID != target.ID) ||
				tc.owner == stage && (result.StageID == nil || *result.StageID != stage.ID) {
				t.Fatalf("scope owner = %#v", result)
			}
			if tc.method != get && store.actor.PrincipalID != auth.Principal.ID {
				t.Fatal("missing actor")
			}
			switch tc.policy {
			case "lock":
				if result.LockVersion != "1" || (tc.method == put) != (result.Lock != nil) {
					t.Fatalf("lock response = %#v", result)
				}
				if result.Lock != nil && (result.Lock.Reason != "scheduled outage" || len(result.Events) != 1 ||
					result.Events[0].Version != result.LockVersion || result.Events[0].PriorVersion != "0") {
					t.Fatalf("lock response = %#v", result)
				}
			case "window":
				if result.WindowVersion != "1" || (tc.method == put) != (result.Window != nil) {
					t.Fatalf("window response = %#v", result)
				}
			}
		})
	}
}
