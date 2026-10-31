// Package productidentity owns durable product principals and the credential
// metadata that resolves browser users and automation to them.
package productidentity

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	KindUser           Kind = "user"
	KindServiceAccount Kind = "service-account"

	RoleAdmin    Role = "admin"
	RoleOperator Role = "operator"
	RoleViewer   Role = "viewer"

	ScopeRead             Scope = "slipramp:read"
	ScopeRunsWrite        Scope = "slipramp:runs:write"
	ScopeRunsReview       Scope = "slipramp:runs:review"
	ScopeInboxWrite       Scope = "slipramp:inbox:write"
	ScopePromotionsSubmit Scope = "slipramp:promotions:submit"
	ScopePromotionsWrite  Scope = "slipramp:promotions:write"
	ScopeAdmin            Scope = "slipramp:admin"

	maximumDisplayNameLength  = 200
	maximumAPIKeyNameLength   = 200
	maximumIssuerLength       = 2048
	maximumSubjectLength      = 1024
	maximumProfileFieldLength = 320
	maximumTokenPrefixLength  = 64
)

var (
	ErrInvalid            = errors.New("invalid product identity")
	ErrUnauthenticated    = errors.New("product credential is missing or invalid")
	ErrPrincipalNotFound  = errors.New("product principal not found")
	ErrIdentityNotFound   = errors.New("OIDC identity not found")
	ErrCredentialNotFound = errors.New("product credential not found")
	ErrIdentityConflict   = errors.New("OIDC identity already exists")
	ErrCredentialConflict = errors.New("product credential already exists")
)

type Kind string

type Role string

type Scope string

type CredentialDigest [sha256.Size]byte

type Principal struct {
	ID          uuid.UUID
	Kind        Kind
	DisplayName string
	Role        Role
	DisabledAt  *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ActorSnapshot struct {
	PrincipalID uuid.UUID `json:"principalId"`
	Kind        Kind      `json:"kind"`
	DisplayName string    `json:"displayName"`
}

type OIDCIdentity struct {
	ID                  uuid.UUID
	PrincipalID         uuid.UUID
	Issuer              string
	Subject             string
	Email               *string
	PreferredUsername   *string
	DisplayName         *string
	LastAuthenticatedAt time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type APIKey struct {
	ID                   uuid.UUID
	PrincipalID          uuid.UUID
	CreatedByPrincipalID uuid.UUID
	Name                 string
	TokenPrefix          string
	TokenDigest          CredentialDigest
	Scopes               []Scope
	CreatedAt            time.Time
	ExpiresAt            *time.Time
	LastUsedAt           *time.Time
	RevokedAt            *time.Time
}

type WebSession struct {
	ID          uuid.UUID
	PrincipalID uuid.UUID
	TokenDigest CredentialDigest
	CreatedAt   time.Time
	ExpiresAt   time.Time
	LastSeenAt  time.Time
}

func (a ActorSnapshot) Validate() error {
	if a.PrincipalID == uuid.Nil {
		return fmt.Errorf("%w: actor principal ID is required", ErrInvalid)
	}
	if !a.Kind.valid() {
		return fmt.Errorf("%w: actor principal kind is invalid", ErrInvalid)
	}
	if err := validateName("actor display name", a.DisplayName, maximumDisplayNameLength); err != nil {
		return err
	}

	return nil
}

func (p Principal) ActorSnapshot() (ActorSnapshot, error) {
	if p.ID == uuid.Nil {
		return ActorSnapshot{}, fmt.Errorf("%w: principal ID is required", ErrInvalid)
	}
	if !p.Kind.valid() {
		return ActorSnapshot{}, fmt.Errorf("%w: principal kind is invalid", ErrInvalid)
	}
	if err := validateName("principal display name", p.DisplayName, maximumDisplayNameLength); err != nil {
		return ActorSnapshot{}, err
	}

	snapshot := ActorSnapshot{
		PrincipalID: p.ID,
		Kind:        p.Kind,
		DisplayName: p.DisplayName,
	}

	return snapshot, snapshot.Validate()
}

func (p Principal) Validate() error {
	if _, err := p.ActorSnapshot(); err != nil {
		return err
	}
	if !p.Role.valid() {
		return fmt.Errorf("%w: principal role is invalid", ErrInvalid)
	}
	if p.CreatedAt.IsZero() || p.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: principal timestamps are required", ErrInvalid)
	}

	return nil
}

func (i OIDCIdentity) Validate() error {
	if i.ID == uuid.Nil || i.PrincipalID == uuid.Nil {
		return fmt.Errorf("%w: OIDC identity and principal IDs are required", ErrInvalid)
	}
	if err := validateIssuer(i.Issuer); err != nil {
		return err
	}
	if err := validateName("OIDC subject", i.Subject, maximumSubjectLength); err != nil {
		return err
	}
	for _, field := range []struct {
		label string
		value *string
	}{
		{label: "OIDC email", value: i.Email},
		{label: "OIDC preferred username", value: i.PreferredUsername},
		{label: "OIDC display name", value: i.DisplayName},
	} {
		if field.value != nil {
			if err := validateName(field.label, *field.value, maximumProfileFieldLength); err != nil {
				return err
			}
		}
	}
	if i.LastAuthenticatedAt.IsZero() || i.CreatedAt.IsZero() || i.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: OIDC identity timestamps are required", ErrInvalid)
	}

	return nil
}

func (k APIKey) Validate() error {
	if k.ID == uuid.Nil || k.PrincipalID == uuid.Nil || k.CreatedByPrincipalID == uuid.Nil {
		return fmt.Errorf("%w: API key and principal IDs are required", ErrInvalid)
	}
	if err := validateName("API key name", k.Name, maximumAPIKeyNameLength); err != nil {
		return err
	}
	if err := validateName("API key token prefix", k.TokenPrefix, maximumTokenPrefixLength); err != nil {
		return err
	}
	if err := validateScopes(k.Scopes); err != nil {
		return err
	}
	if k.CreatedAt.IsZero() {
		return fmt.Errorf("%w: API key creation time is required", ErrInvalid)
	}
	if k.ExpiresAt != nil && !k.ExpiresAt.After(k.CreatedAt) {
		return fmt.Errorf("%w: API key expiry must follow creation", ErrInvalid)
	}

	return nil
}

func (s WebSession) Validate() error {
	if s.ID == uuid.Nil || s.PrincipalID == uuid.Nil {
		return fmt.Errorf("%w: web session and principal IDs are required", ErrInvalid)
	}
	if s.CreatedAt.IsZero() || s.ExpiresAt.IsZero() || s.LastSeenAt.IsZero() {
		return fmt.Errorf("%w: web session timestamps are required", ErrInvalid)
	}
	if !s.ExpiresAt.After(s.CreatedAt) {
		return fmt.Errorf("%w: web session expiry must follow creation", ErrInvalid)
	}
	if s.LastSeenAt.Before(s.CreatedAt) || s.LastSeenAt.After(s.ExpiresAt) {
		return fmt.Errorf("%w: web session last-seen time is outside its lifetime", ErrInvalid)
	}

	return nil
}

func (k Kind) valid() bool {
	return k == KindUser || k == KindServiceAccount
}

func (r Role) valid() bool {
	return r == RoleAdmin || r == RoleOperator || r == RoleViewer
}

func (s Scope) valid() bool {
	switch s {
	case ScopeRead,
		ScopeRunsWrite,
		ScopeRunsReview,
		ScopeInboxWrite,
		ScopePromotionsSubmit,
		ScopePromotionsWrite,
		ScopeAdmin:
		return true
	default:
		return false
	}
}

func validateIssuer(value string) error {
	if err := validateName("OIDC issuer", value, maximumIssuerLength); err != nil {
		return err
	}

	parsed, err := url.Parse(value)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" {
		return fmt.Errorf("%w: OIDC issuer must be an absolute URL", ErrInvalid)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("%w: OIDC issuer must use HTTP or HTTPS", ErrInvalid)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("%w: OIDC issuer must not contain a query or fragment", ErrInvalid)
	}

	return nil
}

func validateScopes(scopes []Scope) error {
	if len(scopes) == 0 {
		return fmt.Errorf("%w: at least one API key scope is required", ErrInvalid)
	}

	seen := make(map[Scope]struct{}, len(scopes))
	for _, scope := range scopes {
		if !scope.valid() {
			return fmt.Errorf("%w: API key scope %q is invalid", ErrInvalid, scope)
		}
		if _, found := seen[scope]; found {
			return fmt.Errorf("%w: API key scope %q is duplicated", ErrInvalid, scope)
		}
		seen[scope] = struct{}{}
	}

	return nil
}

func validateName(label, value string, maximumLength int) error {
	if strings.TrimSpace(value) == "" || value != strings.TrimSpace(value) {
		return fmt.Errorf("%w: %s must not be blank or have surrounding whitespace", ErrInvalid, label)
	}
	if utf8.RuneCountInString(value) > maximumLength {
		return fmt.Errorf("%w: %s must not exceed %d characters", ErrInvalid, label, maximumLength)
	}
	if strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return fmt.Errorf("%w: %s must not contain control characters", ErrInvalid, label)
	}

	return nil
}
