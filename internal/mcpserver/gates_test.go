package mcpserver

import (
	"context"
	"errors"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/gate"
	"github.com/slipramp/slipramp/internal/productidentity"
)

type recordingGates struct {
	state         gate.ScopeState
	actor         productidentity.ActorSnapshot
	unlockVersion int64
	request       gate.OverrideRequest
	requirement   gate.Requirement
	err           error
	calls         int
	wakeups       int
}

func (g *recordingGates) Notify() { g.wakeups++ }

func (g *recordingGates) GetServerGateScope(_ context.Context, owner gate.Scope) (gate.ScopeState, error) {
	g.calls++
	g.state.Owner = owner
	return g.state, g.err
}

func (g *recordingGates) ChangeServerGateLock(ctx context.Context, owner gate.Scope, actor productidentity.ActorSnapshot,
	request *gate.LockRequest) (gate.ScopeState, error) {
	g.actor = actor
	g.state.Lock = &gate.ScopeLock{Actor: actor, Reason: request.Reason, CreatedAt: g.state.CheckedAt, ExpiresAt: request.ExpiresAt}
	g.state.LockVersion++
	return g.GetServerGateScope(ctx, owner)
}

func (g *recordingGates) UnlockServerGateVersion(ctx context.Context, owner gate.Scope, actor productidentity.ActorSnapshot,
	version int64) (gate.ScopeState, error) {
	g.actor, g.unlockVersion = actor, version
	g.state.Lock = nil
	g.state.LockVersion++
	return g.GetServerGateScope(ctx, owner)
}

func (g *recordingGates) ChangeServerGateWindow(ctx context.Context, owner gate.Scope, actor productidentity.ActorSnapshot,
	window *gate.WeeklyWindow) (gate.ScopeState, error) {
	g.actor = actor
	g.state.Window = nil
	if window != nil {
		schedule, err := window.Compile()
		if err != nil {
			return gate.ScopeState{}, err
		}
		observation := schedule.Evaluate(g.state.CheckedAt)
		g.state.Window = &gate.ScopeWindow{Definition: schedule.Definition, State: observation.State, Observation: observation}
	}
	g.state.WindowVersion++
	return g.GetServerGateScope(ctx, owner)
}

func (g *recordingGates) ListGateOverrides(
	_ context.Context, kind gate.SubjectType, id, process uuid.UUID,
) (gate.OverrideCollection, error) {
	g.calls++
	g.request = gate.OverrideRequest{Subject: kind, SubjectID: id, EvaluatorProcessID: process}
	return gate.OverrideCollection{Eligible: []gate.Requirement{g.requirement}}, g.err
}

func (g *recordingGates) RecordGateOverride(_ context.Context, request gate.OverrideRequest) (gate.Requirement, error) {
	g.calls++
	g.request = request
	if err := request.Validate(); err != nil {
		return gate.Requirement{}, err
	}
	check := g.requirement
	check.Overrides = []gate.Override{{
		Reference: request.Reference, Actor: request.Actor, Reason: request.Reason,
		RecordedAt: request.RecordedAt, ObservedState: check.State, ObservedEvidence: check.Evidence,
	}}
	return check, g.err
}

func gateClient(t *testing.T, auth productidentity.Authentication, store *recordingGates, process uuid.UUID) *mcp.ClientSession {
	t.Helper()
	deps := testDependencies(auth)
	deps.Gates, deps.WindowWake, deps.GateEvaluatorProcessID = store, store, process
	handler, err := NewHTTP(deps, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return connectTestClient(t, handler, testAPIKey)
}

func rejectedGateTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any, message string) {
	t.Helper()
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil || !result.IsError {
		t.Fatalf("%s returned %v, %v; want tool error", name, result, err)
	}
	if len(result.Content) != 1 || !strings.Contains(result.Content[0].(*mcp.TextContent).Text, message) {
		t.Fatalf("%s error = %v, want %q", name, result.Content, message)
	}
}

func TestGatePolicyTools(t *testing.T) {
	t.Parallel()
	owner := uuid.New()
	store := &recordingGates{state: gate.ScopeState{ID: uuid.New(), CheckedAt: time.Now().UTC()}}
	auth := testAuthentication(productidentity.ScopeRead, productidentity.ScopeGatesManage)
	auth.Principal.Role = productidentity.RoleAdmin
	client := gateClient(t, auth, store, uuid.New())

	var state api.ServerGateScope
	decodeStructuredContent(t, callTool(t, client, "get_gate_scope", map[string]any{"scope": "global"}), &state)
	if state.ID != store.state.ID || state.Kind != api.GlobalGateScope || state.LockVersion != "0" {
		t.Fatalf("initial scope = %#v", state)
	}
	expiry := store.state.CheckedAt.Add(time.Hour)
	lock := map[string]any{"scope": "target", "ownerId": owner, "reason": " maintenance ", "expiresAt": expiry}
	decodeStructuredContent(t, callTool(t, client, "lock_gate_scope", lock), &state)
	if state.TargetID == nil || *state.TargetID != owner || state.Lock == nil || state.Lock.Reason != "maintenance" ||
		state.Lock.Actor.PrincipalID != auth.Principal.ID || !state.Lock.ExpiresAt.Equal(expiry) {
		t.Fatalf("locked scope = %#v", state)
	}
	callTool(t, client, "unlock_gate_scope", map[string]any{"scope": "target", "ownerId": owner, "lockVersion": state.LockVersion})
	if store.unlockVersion != 1 || store.state.Lock != nil {
		t.Fatalf("unlock version = %d, lock = %#v", store.unlockVersion, store.state.Lock)
	}

	stage := map[string]any{"scope": "stage", "ownerId": owner}
	set := maps.Clone(stage)
	set["window"] = gate.WeeklyWindow{TimeZone: "America/Denver", Weekly: []gate.WeeklyInterval{{
		Days: []string{"monday"}, Start: "09:00", End: "17:00",
	}}}
	state = api.ServerGateScope{}
	decodeStructuredContent(t, callTool(t, client, "set_gate_window", set), &state)
	if state.StageID == nil || *state.StageID != owner || state.Window == nil ||
		state.Window.Definition.TimeZone != "America/Denver" || state.WindowVersion != "1" || store.actor.PrincipalID != auth.Principal.ID {
		t.Fatalf("set window = %#v", state)
	}
	callTool(t, client, "remove_gate_window", stage)
	if store.state.Window != nil || store.wakeups != 2 {
		t.Fatalf("removed window = %#v, wakeups = %d", store.state.Window, store.wakeups)
	}
	rejectedGateTool(t, client, "get_gate_scope", map[string]any{"scope": "global", "ownerId": owner}, "omit ownerId")
	rejectedGateTool(t, client, "get_gate_scope", map[string]any{"scope": "target"}, "UUID")
	rejectedGateTool(t, client, "get_gate_scope", map[string]any{"scope": "repository"}, "scope must be")
	callTool(t, client, "lock_gate_scope", map[string]any{"scope": "global"})
	if store.state.Lock == nil || store.state.Lock.Reason != "" {
		t.Fatalf("lock without a reason = %#v", store.state.Lock)
	}
	rejectedGateTool(t, client, "lock_gate_scope", map[string]any{"scope": "global", "reason": strings.Repeat("a", 1025)}, gate.ErrLockReason.Error())
	rejectedGateTool(t, client, "unlock_gate_scope", map[string]any{"scope": "global", "lockVersion": "bad"}, "lockVersion")
	set["window"] = gate.WeeklyWindow{TimeZone: "Local"}
	rejectedGateTool(t, client, "set_gate_window", set, gate.ErrInvalidWindow.Error())
	store.err = gate.ErrLockVersion
	rejectedGateTool(t, client, "unlock_gate_scope", map[string]any{"scope": "global", "lockVersion": "1"}, gate.ErrLockVersion.Error())
	store.err = errors.New("internal database detail")
	rejectedGateTool(t, client, "get_gate_scope", stage, errInternalToolCall.Error())
}

func TestGateOverrideTools(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		subject gate.SubjectType
		scope   productidentity.Scope
	}{
		{gate.RunSubject, productidentity.ScopeRunsOverrideGates},
		{gate.PromotionSubject, productidentity.ScopePromotionsOverrideGates},
	} {
		t.Run(string(tc.subject), func(t *testing.T) {
			id, process := uuid.New(), uuid.New()
			check := gate.Requirement{
				Key: gate.RepositoryKey("quiet"), Version: "abc", Name: "quiet", Origin: gate.RepositoryOrigin,
				Scope: gate.Scope{Kind: gate.RepositoryScope, ID: uuid.New()}, State: gate.RequirementBlocking,
				Evidence: gate.PrometheusSample(7),
			}
			store := &recordingGates{requirement: check}
			auth := testAuthentication(productidentity.ScopeRead, tc.scope)
			client := gateClient(t, auth, store, process)
			args := map[string]any{"subject": tc.subject, "subjectId": id}
			var list api.GateOverrideCollection
			decodeStructuredContent(t, callTool(t, client, "list_gate_overrides", args), &list)
			if len(list.Eligible) != 1 || list.Eligible[0].Key != string(check.Key) || store.request.Subject != tc.subject ||
				store.request.SubjectID != id || store.request.EvaluatorProcessID != process {
				t.Fatalf("override list = %#v, request = %#v", list, store.request)
			}
			args["key"], args["version"], args["reason"] = list.Eligible[0].Key, *list.Eligible[0].Version, "incident recovery"
			var result api.GateRequirement
			decodeStructuredContent(t, callTool(t, client, "override_gate", args), &result)
			r := store.request
			if r.Subject != tc.subject || r.SubjectID != id || r.EvaluatorProcessID != process || r.Reference != check.Reference ||
				r.Reason != args["reason"] || r.Actor.PrincipalID != auth.Principal.ID || r.RecordedAt.IsZero() ||
				result.Overrides == nil || len(*result.Overrides) != 1 || *(*result.Overrides)[0].ObservedEvidence.PrometheusValue() != 7 {
				t.Fatalf("override request = %#v, result = %#v", r, result)
			}
			delete(args, "reason")
			decodeStructuredContent(t, callTool(t, client, "override_gate", args), &result)
			if store.request.Reason != "" {
				t.Fatalf("override without a reason = %#v", store.request)
			}
			store.err = gate.ErrOverrideConflict
			rejectedGateTool(t, client, "override_gate", args, gate.ErrOverrideConflict.Error())
			args["reason"] = strings.Repeat("a", 1025)
			rejectedGateTool(t, client, "override_gate", args, gate.ErrOverrideInvalid.Error())
		})
	}
}

func TestGateToolsEnforceRoleAndScopes(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	local := map[string]any{"scope": "target", "ownerId": id, "reason": "maintenance"}
	global := map[string]any{"scope": "global", "reason": "maintenance"}
	run := map[string]any{"subject": "run", "subjectId": id, "key": "repository/quiet", "version": "1", "reason": "maintenance"}
	promotion := maps.Clone(run)
	promotion["subject"] = "promotion"
	viewer, operator, admin := productidentity.RoleViewer, productidentity.RoleOperator, productidentity.RoleAdmin
	read, manage := productidentity.ScopeRead, productidentity.ScopeGatesManage
	waiveRun, waivePromotion := productidentity.ScopeRunsOverrideGates, productidentity.ScopePromotionsOverrideGates
	forbidden := gate.ErrScopeForbidden.Error()
	for _, tc := range []struct {
		name    string
		tool    string
		args    map[string]any
		role    productidentity.Role
		scope   productidentity.Scope
		message string
	}{
		{"manage cannot read", "get_gate_scope", map[string]any{"scope": "global"}, admin, manage, "not authorized"},
		{"run write cannot waive", "override_gate", run, operator, productidentity.ScopeRunsWrite, "not authorized"},
		{"manage cannot waive", "override_gate", run, admin, manage, "not authorized"},
		{"Run permission cannot waive Promotion", "override_gate", promotion, operator, waiveRun, "not authorized"},
		{"Promotion permission cannot waive Run", "override_gate", run, operator, waivePromotion, "not authorized"},
		{"viewer cannot waive", "override_gate", run, viewer, waiveRun, "not authorized"},
		{"operator cannot change global", "lock_gate_scope", global, operator, manage, forbidden},
		{"viewer cannot change local", "lock_gate_scope", local, viewer, manage, forbidden},
		{"read cannot change local", "lock_gate_scope", local, operator, read, forbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &recordingGates{}
			auth := testAuthentication(tc.scope)
			auth.Principal.Role = tc.role
			client := gateClient(t, auth, store, uuid.New())
			rejectedGateTool(t, client, tc.tool, tc.args, tc.message)
			if store.calls != 0 {
				t.Fatalf("unauthorized store calls = %d", store.calls)
			}
		})
	}
}
