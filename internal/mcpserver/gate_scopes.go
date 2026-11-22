package mcpserver

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/gate"
	"github.com/slipramp/slipramp/internal/productidentity"
)

type gateScopeInput struct {
	Scope   gate.ScopeKind `json:"scope" jsonschema:"Policy scope: global, target, or stage."`
	OwnerID string         `json:"ownerId,omitempty" jsonschema:"Target or Stage UUID. Omit for global policy."`
}

func (input gateScopeInput) owner() (gate.Scope, error) {
	switch input.Scope {
	case gate.GlobalScope:
		if input.OwnerID != "" {
			return gate.Scope{}, errors.New("omit ownerId for global policy")
		}
		return gate.Scope{Kind: gate.GlobalScope}, nil
	case gate.TargetScope, gate.StageScope:
		id, err := requiredUUID(input.OwnerID, "ownerId")
		return gate.Scope{Kind: input.Scope, ID: id}, err
	default:
		return gate.Scope{}, errors.New("scope must be global, target, or stage")
	}
}

func (input gateScopeInput) attributes() []any {
	owner, err := input.owner()
	if err != nil {
		return nil
	}
	attributes := []any{"gate_scope", owner.Kind}
	switch owner.Kind {
	case gate.TargetScope:
		return append(attributes, "target_id", owner.ID)
	case gate.StageScope:
		return append(attributes, "stage_id", owner.ID)
	}
	return attributes
}

type lockGateScopeInput struct {
	gateScopeInput
	Reason    string     `json:"reason" jsonschema:"Reason for the hold (1 to 1024 characters)."`
	ExpiresAt *time.Time `json:"expiresAt,omitempty" jsonschema:"Optional future RFC 3339 timestamp. Omit to hold until released."`
}

type unlockGateScopeInput struct {
	gateScopeInput
	LockVersion string `json:"lockVersion" jsonschema:"Observed lockVersion from get_gate_scope or lock_gate_scope."`
}

type setGateWindowInput struct {
	gateScopeInput
	Window gate.WeeklyWindow `json:"window" jsonschema:"IANA time zone and weekly intervals with lowercase weekdays and local HH:MM start and end times."`
}

func (s *toolHandler) getGateScope(ctx context.Context, _ *mcp.CallToolRequest, input gateScopeInput) (
	*mcp.CallToolResult, api.ServerGateScope, error,
) {
	if _, err := authorize(ctx, productidentity.PermissionRead); err != nil {
		return nil, api.ServerGateScope{}, err
	}
	owner, err := input.owner()
	if err != nil {
		return nil, api.ServerGateScope{}, err
	}
	return s.gateScopeResult(s.gateScopes.Inspect(ctx, owner))
}

func (input gateScopeInput) ownerAndAuthentication(ctx context.Context) (gate.Scope, productidentity.Authentication, error) {
	owner, err := input.owner()
	if err != nil {
		return gate.Scope{}, productidentity.Authentication{}, err
	}
	auth, err := currentAuthentication(ctx)
	return owner, auth, err
}

func (s *toolHandler) lockGateScope(ctx context.Context, _ *mcp.CallToolRequest, input lockGateScopeInput) (
	*mcp.CallToolResult, api.ServerGateScope, error,
) {
	owner, auth, err := input.ownerAndAuthentication(ctx)
	if err != nil {
		return nil, api.ServerGateScope{}, err
	}
	return s.gateScopeResult(s.gateScopes.ChangeLock(ctx, owner, auth, &gate.LockRequest{
		Reason: input.Reason, ExpiresAt: input.ExpiresAt,
	}))
}

func (s *toolHandler) unlockGateScope(ctx context.Context, _ *mcp.CallToolRequest, input unlockGateScopeInput) (
	*mcp.CallToolResult, api.ServerGateScope, error,
) {
	owner, auth, err := input.ownerAndAuthentication(ctx)
	if err != nil {
		return nil, api.ServerGateScope{}, err
	}
	version, err := strconv.ParseInt(input.LockVersion, 10, 64)
	if err != nil || version < 0 || strconv.FormatInt(version, 10) != input.LockVersion {
		return nil, api.ServerGateScope{}, errors.New("lockVersion must be the version returned for this scope")
	}
	return s.gateScopeResult(s.gateScopes.UnlockVersion(ctx, owner, auth, version))
}

func (s *toolHandler) setGateWindow(ctx context.Context, _ *mcp.CallToolRequest, input setGateWindowInput) (
	*mcp.CallToolResult, api.ServerGateScope, error,
) {
	return s.changeGateWindow(ctx, input.gateScopeInput, &input.Window)
}

func (s *toolHandler) removeGateWindow(ctx context.Context, _ *mcp.CallToolRequest, input gateScopeInput) (
	*mcp.CallToolResult, api.ServerGateScope, error,
) {
	return s.changeGateWindow(ctx, input, nil)
}

func (s *toolHandler) changeGateWindow(ctx context.Context, input gateScopeInput, window *gate.WeeklyWindow) (
	*mcp.CallToolResult, api.ServerGateScope, error,
) {
	owner, auth, err := input.ownerAndAuthentication(ctx)
	if err != nil {
		return nil, api.ServerGateScope{}, err
	}
	return s.gateScopeResult(s.gateScopes.ChangeWindow(ctx, owner, auth, window))
}

func (s *toolHandler) gateScopeResult(state gate.ScopeState, err error) (*mcp.CallToolResult, api.ServerGateScope, error) {
	if err == nil {
		return nil, api.NewServerGateScope(state), nil
	}
	if errors.Is(err, gate.ErrScopeNotFound) || errors.Is(err, gate.ErrScopeForbidden) ||
		errors.Is(err, gate.ErrLockReason) || errors.Is(err, gate.ErrLockExpiry) ||
		errors.Is(err, gate.ErrLockConflict) || errors.Is(err, gate.ErrLockVersion) || errors.Is(err, gate.ErrInvalidWindow) {
		return nil, api.ServerGateScope{}, err
	}
	return nil, api.ServerGateScope{}, s.internalError("access Gate scope", err)
}
