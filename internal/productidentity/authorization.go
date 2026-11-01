package productidentity

import (
	"fmt"

	"github.com/google/uuid"
)

const (
	AuthenticationMethodWebSession AuthenticationMethod = "web-session"
	AuthenticationMethodAPIKey     AuthenticationMethod = "api-key"
	AuthenticationMethodOAuthToken AuthenticationMethod = "oauth-access-token"

	PermissionRead                   Permission = "read"
	PermissionRunsWrite              Permission = "runs:write"
	PermissionRunsReview             Permission = "runs:review"
	PermissionInboxWrite             Permission = "inbox:write"
	PermissionAdmin                  Permission = "admin"
	PermissionAPIKeyRevoke           Permission = "api-keys:revoke"
	PermissionSensitiveArtifactsRead Permission = "sensitive-artifacts:read"
	PermissionPromotionsSubmit       Permission = "promotions:submit"
	PermissionPromotionsWrite        Permission = "promotions:write"
)

type AuthenticationMethod string

type Permission string

// Authentication is the product identity established for one request. It is
// deliberately independent from controller and Run artifact credentials.
type Authentication struct {
	Principal       Principal
	Method          AuthenticationMethod
	CredentialID    uuid.UUID
	CredentialLabel string
	Scopes          []Scope
}

func (a Authentication) Validate() error {
	if err := a.Principal.Validate(); err != nil {
		return fmt.Errorf("validate authenticated principal: %w", err)
	}
	if a.CredentialID == uuid.Nil {
		return fmt.Errorf("%w: credential ID is required", ErrInvalid)
	}

	switch a.Method {
	case AuthenticationMethodWebSession:
		if a.Principal.Kind != KindUser {
			return fmt.Errorf("%w: web session requires a user principal", ErrInvalid)
		}
		if a.CredentialLabel != "" {
			return fmt.Errorf("%w: web session must not have a credential label", ErrInvalid)
		}
		if len(a.Scopes) != 0 {
			return fmt.Errorf("%w: web session must not have credential scopes", ErrInvalid)
		}
	case AuthenticationMethodAPIKey, AuthenticationMethodOAuthToken:
		if err := validateName(
			"credential authentication label",
			a.CredentialLabel,
			maximumAPIKeyNameLength,
		); err != nil {
			return err
		}
		if err := validateScopes(a.Scopes); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%w: authentication method is invalid", ErrInvalid)
	}

	return nil
}

func (a Authentication) Authorizes(permission Permission) bool {
	if a.Principal.DisabledAt != nil || !roleAuthorizes(a.Principal.Role, permission) {
		return false
	}

	switch a.Method {
	case AuthenticationMethodWebSession:
		return true
	case AuthenticationMethodAPIKey, AuthenticationMethodOAuthToken:
		return scopesAuthorize(a.Scopes, permission)
	default:
		return false
	}
}

func roleAuthorizes(role Role, permission Permission) bool {
	switch permission {
	case PermissionRead:
		return role == RoleViewer || role == RoleOperator || role == RoleAdmin
	case PermissionRunsWrite, PermissionRunsReview, PermissionInboxWrite:
		return role == RoleOperator || role == RoleAdmin
	case PermissionPromotionsSubmit, PermissionPromotionsWrite:
		return role == RoleOperator || role == RoleAdmin
	case PermissionAdmin:
		return role == RoleAdmin
	case PermissionAPIKeyRevoke:
		return role == RoleViewer || role == RoleOperator || role == RoleAdmin
	case PermissionSensitiveArtifactsRead:
		return role == RoleOperator || role == RoleAdmin
	default:
		return false
	}
}

func scopesAuthorize(scopes []Scope, permission Permission) bool {
	for _, scope := range scopes {
		if !scope.valid() {
			continue
		}
		if scope == ScopeAdmin {
			return true
		}
		if permission == PermissionAPIKeyRevoke {
			return true
		}

		switch permission {
		case PermissionRead:
			if scope == ScopeRead {
				return true
			}
		case PermissionSensitiveArtifactsRead:
			if scope == ScopeRead {
				return true
			}
		case PermissionRunsWrite:
			if scope == ScopeRunsWrite {
				return true
			}
		case PermissionRunsReview:
			if scope == ScopeRunsReview {
				return true
			}
		case PermissionInboxWrite:
			if scope == ScopeInboxWrite {
				return true
			}
		case PermissionPromotionsSubmit:
			if scope == ScopePromotionsSubmit {
				return true
			}
		case PermissionPromotionsWrite:
			if scope == ScopePromotionsWrite {
				return true
			}
		case PermissionAdmin:
		}
	}

	return false
}
