package gate

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/productidentity"
)

const (
	LockType = "lock"

	GlobalScope ScopeKind = "global"
	TargetScope ScopeKind = "target"
	StageScope  ScopeKind = "stage"
)

var (
	ErrScopeNotFound  = errors.New("the Server Gate scope does not exist")
	ErrScopeForbidden = errors.New("the principal cannot manage this Gate scope")
	ErrLockReason     = errors.New("lock reason must be valid text of at most 1024 characters")
	ErrLockExpiry     = errors.New("lock expiry must be in the future")
	ErrLockConflict   = errors.New("a different lock is active at this scope")
	ErrLockVersion    = errors.New("the active lock version has changed")
)

type LockRequest struct {
	Reason    string     `json:"reason,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

// Reject unknown fields so a misspelled expiry cannot create an indefinite hold.
func (r *LockRequest) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return errors.New("lock request must be a JSON object")
	}
	type fields LockRequest
	return decodePayload(data, (*fields)(r))
}

type ScopeLock struct {
	Reason    string                        `json:"reason"`
	ExpiresAt *time.Time                    `json:"expiresAt,omitempty"`
	Actor     productidentity.ActorSnapshot `json:"actor"`
	CreatedAt time.Time                     `json:"createdAt"`
}

func (*ScopeLock) Type() string { return LockType }

func (lock *ScopeLock) Validate() error {
	if lock == nil || lock.CreatedAt.IsZero() || lock.Actor.Validate() != nil {
		return errors.New("invalid Gate lock evidence")
	}
	if !validGateReason(lock.Reason) {
		return ErrLockReason
	}
	if lock.ExpiresAt != nil && !lock.ExpiresAt.After(lock.CreatedAt) {
		return ErrLockExpiry
	}
	return nil
}

func validGateReason(reason string) bool {
	return utf8.ValidString(reason) &&
		!strings.ContainsRune(reason, 0) && utf8.RuneCountInString(reason) <= 1024
}

type LockEvent struct {
	Action     string
	Version    int64
	Actor      productidentity.ActorSnapshot
	Reason     string
	RecordedAt time.Time
	ExpiresAt  *time.Time
}

// ScopeState is a snapshot of one scope at CheckedAt. Lock is present while the hold is active.
type ScopeState struct {
	ID            uuid.UUID
	Owner         Scope
	CheckedAt     time.Time
	LockVersion   int64
	Lock          *ScopeLock
	Events        []LockEvent
	WindowVersion int64
	Window        *ScopeWindow
	WindowEvents  []WindowEvent
}

type ScopeStore interface {
	GetServerGateScope(context.Context, Scope) (ScopeState, error)
	ChangeServerGateLock(context.Context, Scope, productidentity.ActorSnapshot, *LockRequest) (ScopeState, error)
	UnlockServerGateVersion(context.Context, Scope, productidentity.ActorSnapshot, int64) (ScopeState, error)
	ChangeServerGateWindow(context.Context, Scope, productidentity.ActorSnapshot, *WeeklyWindow) (ScopeState, error)
}

type ScopeService struct {
	store      ScopeStore
	windowWake EvaluationNotifier
}

func NewScopeService(store ScopeStore, windowWake EvaluationNotifier) ScopeService {
	return ScopeService{store: store, windowWake: windowWake}
}

func (s ScopeService) Inspect(ctx context.Context, owner Scope) (ScopeState, error) {
	return s.store.GetServerGateScope(ctx, owner)
}

// ChangeLock clears the current hold when request is nil.
func (s ScopeService) ChangeLock(ctx context.Context, scope Scope, auth productidentity.Authentication,
	request *LockRequest) (ScopeState, error) {
	actor, err := scopePolicyActor(scope, auth)
	if err != nil {
		return ScopeState{}, err
	}
	if request != nil {
		reason := strings.TrimSpace(request.Reason)
		if !validGateReason(reason) {
			return ScopeState{}, ErrLockReason
		}
		request = &LockRequest{Reason: reason, ExpiresAt: request.ExpiresAt}
	}
	return s.store.ChangeServerGateLock(ctx, scope, actor, request)
}

func scopePolicyActor(scope Scope, auth productidentity.Authentication) (productidentity.ActorSnapshot, error) {
	permission := productidentity.PermissionManageLocalGates
	switch scope.Kind {
	case GlobalScope:
		permission = productidentity.PermissionManageGlobalGates
	case TargetScope, StageScope:
	default:
		return productidentity.ActorSnapshot{}, ErrScopeNotFound
	}
	if !auth.Authorizes(permission) {
		return productidentity.ActorSnapshot{}, ErrScopeForbidden
	}
	return auth.Principal.ActorSnapshot()
}

func (s ScopeService) UnlockVersion(ctx context.Context, scope Scope, auth productidentity.Authentication,
	version int64) (ScopeState, error) {
	actor, err := scopePolicyActor(scope, auth)
	if err != nil {
		return ScopeState{}, err
	}
	return s.store.UnlockServerGateVersion(ctx, scope, actor, version)
}

func (s ScopeService) ChangeWindow(ctx context.Context, scope Scope, auth productidentity.Authentication,
	request *WeeklyWindow) (ScopeState, error) {
	actor, err := scopePolicyActor(scope, auth)
	if err != nil {
		return ScopeState{}, err
	}
	state, err := s.store.ChangeServerGateWindow(ctx, scope, actor, request)
	if err == nil && s.windowWake != nil {
		s.windowWake.Notify()
	}
	return state, err
}
