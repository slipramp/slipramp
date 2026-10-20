// Package productauth owns product-user OIDC login and local browser sessions.
package productauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/slipramp/slipramp/internal/productidentity"
)

const (
	SessionCookieName = "slipramp_session"
	StateCookieName   = "slipramp_oidc_state"

	randomSecretBytes        = 32
	maximumCreateTries       = 3
	maximumLoginTransactions = 1024
)

var (
	ErrUnavailable      = errors.New("browser authentication is unavailable")
	ErrInvalidLogin     = errors.New("invalid OIDC login")
	ErrEnrollmentDenied = errors.New("OIDC identity is not enrolled")
)

type Store interface {
	GetOIDCUser(
		ctx context.Context,
		issuer string,
		subject string,
	) (productidentity.Principal, productidentity.OIDCIdentity, error)
	CreateOIDCUser(
		context.Context,
		productidentity.Principal,
		productidentity.OIDCIdentity,
	) (productidentity.Principal, productidentity.OIDCIdentity, error)
	UpdateOIDCUser(
		context.Context,
		productidentity.Principal,
		productidentity.OIDCIdentity,
	) (productidentity.Principal, productidentity.OIDCIdentity, error)
	CreateWebSession(
		context.Context,
		productidentity.WebSession,
	) (productidentity.WebSession, error)
	GetWebSessionPrincipal(
		ctx context.Context,
		tokenDigest productidentity.CredentialDigest,
	) (productidentity.WebSession, productidentity.Principal, error)
	TouchWebSession(context.Context, uuid.UUID, time.Time) error
	DeleteWebSession(context.Context, uuid.UUID) error
}

type loginTransaction struct {
	nonce        string
	codeVerifier string
	expiresAt    time.Time
}

type Service struct {
	store          Store
	provider       OIDCProvider
	cfg            Config
	secureCookies  bool
	now            func() time.Time
	newID          func() uuid.UUID
	randomSecret   func() (string, error)
	transactionsMu sync.Mutex
	transactions   map[string]loginTransaction
}

type LoginStart struct {
	AuthorizationURL string
	State            string
	ExpiresAt        time.Time
}

type SessionCredential struct {
	Token     string
	ExpiresAt time.Time
}

func NewService(store Store, provider OIDCProvider, cfg Config) (*Service, error) {
	if store == nil {
		return nil, errors.New("product authentication store is required")
	}
	if provider == nil {
		return nil, errors.New("OIDC provider is required")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if !cfg.OIDC.Enabled() {
		return nil, errors.New("OIDC configuration is required")
	}
	cfg.OIDC.SubjectRoles = maps.Clone(cfg.OIDC.SubjectRoles)

	return &Service{
		store:         store,
		provider:      provider,
		cfg:           cfg,
		secureCookies: strings.HasPrefix(cfg.OIDC.RedirectURL, "https://"),
		now:           time.Now,
		newID:         uuid.New,
		randomSecret:  newRandomSecret,
		transactions:  make(map[string]loginTransaction),
	}, nil
}

func (s *Service) SecureCookies() bool {
	return s.secureCookies
}

func (s *Service) BeginLogin() (LoginStart, error) {
	now := s.now().UTC()
	state, err := s.randomSecret()
	if err != nil {
		return LoginStart{}, fmt.Errorf("generate OIDC state: %w", err)
	}
	nonce, err := s.randomSecret()
	if err != nil {
		return LoginStart{}, fmt.Errorf("generate OIDC nonce: %w", err)
	}
	codeVerifier := oauth2.GenerateVerifier()
	expiresAt := now.Add(s.cfg.LoginTransactionLifetime)

	s.transactionsMu.Lock()
	defer s.transactionsMu.Unlock()

	s.pruneLoginTransactions(now)
	if len(s.transactions) >= maximumLoginTransactions {
		return LoginStart{}, ErrUnavailable
	}
	s.transactions[state] = loginTransaction{
		nonce:        nonce,
		codeVerifier: codeVerifier,
		expiresAt:    expiresAt,
	}

	return LoginStart{
		AuthorizationURL: s.provider.AuthorizationURL(
			state,
			nonce,
			oauth2.S256ChallengeFromVerifier(codeVerifier),
		),
		State:     state,
		ExpiresAt: expiresAt,
	}, nil
}

func (s *Service) CompleteLogin(
	ctx context.Context,
	code string,
	state string,
	cookieState string,
) (SessionCredential, error) {
	if strings.TrimSpace(code) == "" {
		return SessionCredential{}, fmt.Errorf("%w: authorization code is missing", ErrInvalidLogin)
	}

	transaction, err := s.takeLoginTransaction(state, cookieState)
	if err != nil {
		return SessionCredential{}, err
	}

	profile, err := s.provider.Authenticate(
		ctx,
		code,
		transaction.codeVerifier,
		transaction.nonce,
	)
	if err != nil {
		if isInvalidLogin(err) {
			return SessionCredential{}, err
		}

		return SessionCredential{}, fmt.Errorf("authenticate with OIDC provider: %w", err)
	}

	role, enrolled := s.cfg.OIDC.SubjectRoles[profile.Subject]
	if !enrolled {
		return SessionCredential{}, ErrEnrollmentDenied
	}

	principal, err := s.resolveOIDCUser(ctx, profile, role)
	if err != nil {
		return SessionCredential{}, err
	}
	if principal.DisabledAt != nil {
		return SessionCredential{}, productidentity.ErrUnauthenticated
	}

	return s.createSession(ctx, principal.ID)
}

func (s *Service) AuthenticateSession(
	ctx context.Context,
	token string,
) (productidentity.Authentication, error) {
	digest, err := sessionDigest(token)
	if err != nil {
		return productidentity.Authentication{}, productidentity.ErrUnauthenticated
	}

	session, principal, err := s.store.GetWebSessionPrincipal(ctx, digest)
	if errors.Is(err, productidentity.ErrCredentialNotFound) {
		return productidentity.Authentication{}, productidentity.ErrUnauthenticated
	}
	if err != nil {
		return productidentity.Authentication{}, fmt.Errorf("get product session: %w", err)
	}

	now := s.now().UTC()
	if !now.Before(session.ExpiresAt) {
		if err := s.store.DeleteWebSession(ctx, session.ID); err != nil &&
			!errors.Is(err, productidentity.ErrCredentialNotFound) {
			return productidentity.Authentication{}, fmt.Errorf("delete expired product session: %w", err)
		}

		return productidentity.Authentication{}, productidentity.ErrUnauthenticated
	}
	if principal.DisabledAt != nil {
		return productidentity.Authentication{}, productidentity.ErrUnauthenticated
	}
	if err := s.store.TouchWebSession(ctx, session.ID, now); err != nil {
		return productidentity.Authentication{}, fmt.Errorf("touch product session: %w", err)
	}

	authentication := productidentity.Authentication{
		Principal:    principal,
		Method:       productidentity.AuthenticationMethodWebSession,
		CredentialID: session.ID,
	}
	if err := authentication.Validate(); err != nil {
		return productidentity.Authentication{}, fmt.Errorf(
			"validate product session authentication: %w",
			err,
		)
	}

	return authentication, nil
}

func (s *Service) RevokeSession(ctx context.Context, token string) error {
	authentication, err := s.AuthenticateSession(ctx, token)
	if err != nil {
		return err
	}
	if err := s.store.DeleteWebSession(ctx, authentication.CredentialID); err != nil &&
		!errors.Is(err, productidentity.ErrCredentialNotFound) {
		return fmt.Errorf("delete product session: %w", err)
	}

	return nil
}

func (s *Service) takeLoginTransaction(
	state string,
	cookieState string,
) (loginTransaction, error) {
	if state == "" || cookieState == "" ||
		subtle.ConstantTimeCompare([]byte(state), []byte(cookieState)) != 1 {
		return loginTransaction{}, fmt.Errorf("%w: state cookie does not match", ErrInvalidLogin)
	}

	now := s.now().UTC()
	s.transactionsMu.Lock()
	defer s.transactionsMu.Unlock()

	transaction, found := s.transactions[state]
	delete(s.transactions, state)
	if !found || !now.Before(transaction.expiresAt) {
		return loginTransaction{}, fmt.Errorf("%w: login transaction is missing or expired", ErrInvalidLogin)
	}

	return transaction, nil
}

func (s *Service) pruneLoginTransactions(now time.Time) {
	for state, transaction := range s.transactions {
		if !now.Before(transaction.expiresAt) {
			delete(s.transactions, state)
		}
	}
}

func (s *Service) resolveOIDCUser(
	ctx context.Context,
	profile OIDCProfile,
	role productidentity.Role,
) (productidentity.Principal, error) {
	now := s.now().UTC()
	displayName, err := profileDisplayName(profile)
	if err != nil {
		return productidentity.Principal{}, err
	}

	principal, identity, err := s.store.GetOIDCUser(ctx, profile.Issuer, profile.Subject)
	if err == nil {
		principal.DisplayName = displayName
		principal.Role = role
		principal.UpdatedAt = now
		identity.Email = normalizedProfileValue(profile.Email)
		identity.PreferredUsername = normalizedProfileValue(profile.PreferredUsername)
		identity.DisplayName = normalizedProfileValue(profile.DisplayName)
		identity.LastAuthenticatedAt = now
		identity.UpdatedAt = now
		if err := principal.Validate(); err != nil {
			return productidentity.Principal{}, err
		}
		if err := identity.Validate(); err != nil {
			return productidentity.Principal{}, err
		}

		updated, _, updateErr := s.store.UpdateOIDCUser(ctx, principal, identity)
		if updateErr != nil {
			return productidentity.Principal{}, fmt.Errorf("update OIDC user: %w", updateErr)
		}

		return updated, nil
	}
	if !errors.Is(err, productidentity.ErrIdentityNotFound) {
		return productidentity.Principal{}, fmt.Errorf("get OIDC user: %w", err)
	}

	principal = productidentity.Principal{
		ID:          s.newID(),
		Kind:        productidentity.KindUser,
		DisplayName: displayName,
		Role:        role,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	identity = productidentity.OIDCIdentity{
		ID:                  s.newID(),
		PrincipalID:         principal.ID,
		Issuer:              profile.Issuer,
		Subject:             profile.Subject,
		Email:               normalizedProfileValue(profile.Email),
		PreferredUsername:   normalizedProfileValue(profile.PreferredUsername),
		DisplayName:         normalizedProfileValue(profile.DisplayName),
		LastAuthenticatedAt: now,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if err := principal.Validate(); err != nil {
		return productidentity.Principal{}, err
	}
	if err := identity.Validate(); err != nil {
		return productidentity.Principal{}, err
	}

	created, _, err := s.store.CreateOIDCUser(ctx, principal, identity)
	if errors.Is(err, productidentity.ErrIdentityConflict) {
		return s.resolveOIDCUser(ctx, profile, role)
	}
	if err != nil {
		return productidentity.Principal{}, fmt.Errorf("create OIDC user: %w", err)
	}

	return created, nil
}

func (s *Service) createSession(
	ctx context.Context,
	principalID uuid.UUID,
) (SessionCredential, error) {
	for range maximumCreateTries {
		token, err := s.randomSecret()
		if err != nil {
			return SessionCredential{}, fmt.Errorf("generate product session token: %w", err)
		}
		digest, err := sessionDigest(token)
		if err != nil {
			return SessionCredential{}, fmt.Errorf("digest product session token: %w", err)
		}

		now := s.now().UTC()
		expiresAt := now.Add(s.cfg.SessionLifetime)
		record := productidentity.WebSession{
			ID:          s.newID(),
			PrincipalID: principalID,
			TokenDigest: digest,
			CreatedAt:   now,
			ExpiresAt:   expiresAt,
			LastSeenAt:  now,
		}
		if err := record.Validate(); err != nil {
			return SessionCredential{}, err
		}

		_, err = s.store.CreateWebSession(ctx, record)
		if errors.Is(err, productidentity.ErrCredentialConflict) {
			continue
		}
		if err != nil {
			return SessionCredential{}, fmt.Errorf("create product session: %w", err)
		}

		return SessionCredential{Token: token, ExpiresAt: expiresAt}, nil
	}

	return SessionCredential{}, productidentity.ErrCredentialConflict
}

func profileDisplayName(profile OIDCProfile) (string, error) {
	for _, candidate := range []*string{
		profile.DisplayName,
		profile.PreferredUsername,
		profile.Email,
	} {
		if candidate == nil {
			continue
		}
		value := strings.TrimSpace(*candidate)
		if validDisplayName(value) {
			return value, nil
		}
	}
	if validDisplayName(profile.Subject) {
		return profile.Subject, nil
	}

	return "", fmt.Errorf("%w: OIDC profile has no usable display name", productidentity.ErrInvalid)
}

func validDisplayName(value string) bool {
	return value != "" && len(value) <= 200 &&
		strings.IndexFunc(value, unicode.IsControl) == -1
}

func normalizedProfileValue(value *string) *string {
	if value == nil {
		return nil
	}

	normalized := strings.TrimSpace(*value)
	if normalized == "" {
		return nil
	}

	return &normalized
}

func newRandomSecret() (string, error) {
	secret := make([]byte, randomSecretBytes)
	if _, err := rand.Read(secret); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(secret), nil
}

func sessionDigest(token string) (productidentity.CredentialDigest, error) {
	secret, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(secret) != randomSecretBytes {
		return productidentity.CredentialDigest{}, errors.New("invalid session token")
	}

	return productidentity.CredentialDigest(sha256.Sum256([]byte(token))), nil
}
