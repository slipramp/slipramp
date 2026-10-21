package productauth

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/productidentity"
)

func TestServiceCompletesLoginAndAuthenticatesSession(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 19, 12, 0, 0, 0, time.UTC)
	profileName := "Ada Lovelace"
	provider := &recordingOIDCProvider{profile: OIDCProfile{
		Issuer:      "https://identity.example.com",
		Subject:     "ada-subject",
		DisplayName: &profileName,
	}}
	store := newProductAuthStore()
	service := newTestService(t, store, provider, now)

	login, err := service.BeginLogin()
	if err != nil {
		t.Fatalf("BeginLogin() error = %v", err)
	}
	if login.AuthorizationURL != "https://identity.example.com/authorize?state="+login.State {
		t.Errorf("AuthorizationURL = %q", login.AuthorizationURL)
	}

	credential, err := service.CompleteLogin(context.Background(), "authorization-code", login.State, login.State)
	if err != nil {
		t.Fatalf("CompleteLogin() error = %v", err)
	}
	if provider.code != "authorization-code" || provider.expectedNonce == "" ||
		provider.codeVerifier == "" {
		t.Errorf("provider exchange = %#v", provider)
	}
	if len(store.principals) != 1 || len(store.identities) != 1 || len(store.sessions) != 1 {
		t.Fatalf(
			"created principals=%d identities=%d sessions=%d",
			len(store.principals),
			len(store.identities),
			len(store.sessions),
		)
	}

	var principal productidentity.Principal
	for _, item := range store.principals {
		principal = item
	}
	if principal.DisplayName != profileName || principal.Role != productidentity.RoleAdmin {
		t.Errorf("created principal = %#v", principal)
	}

	authentication, err := service.AuthenticateSession(context.Background(), credential.Token)
	if err != nil {
		t.Fatalf("AuthenticateSession() error = %v", err)
	}
	if authentication.Principal.ID != principal.ID ||
		authentication.Method != productidentity.AuthenticationMethodWebSession ||
		authentication.CredentialID == uuid.Nil {
		t.Errorf("authentication = %#v", authentication)
	}
	if store.touchCount != 1 {
		t.Errorf("session touch count = %d, want 1", store.touchCount)
	}

	if err := service.RevokeSession(context.Background(), credential.Token); err != nil {
		t.Fatalf("RevokeSession() error = %v", err)
	}
	_, err = service.AuthenticateSession(context.Background(), credential.Token)
	if !errors.Is(err, productidentity.ErrUnauthenticated) {
		t.Fatalf("authentication after logout error = %v, want unauthenticated", err)
	}
}

func TestServiceUpdatesReturningOIDCUser(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 19, 12, 0, 0, 0, time.UTC)
	oldName := "Old Name"
	newName := "New Name"
	provider := &recordingOIDCProvider{profile: OIDCProfile{
		Issuer:      "https://identity.example.com",
		Subject:     "ada-subject",
		DisplayName: &newName,
	}}
	store := newProductAuthStore()
	principalID := uuid.New()
	identityID := uuid.New()
	store.principals[principalID] = productidentity.Principal{
		ID:          principalID,
		Kind:        productidentity.KindUser,
		DisplayName: oldName,
		Role:        productidentity.RoleViewer,
		CreatedAt:   now.Add(-time.Hour),
		UpdatedAt:   now.Add(-time.Hour),
	}
	store.identities[oidcIdentityKey(provider.profile.Issuer, provider.profile.Subject)] =
		productidentity.OIDCIdentity{
			ID:                  identityID,
			PrincipalID:         principalID,
			Issuer:              provider.profile.Issuer,
			Subject:             provider.profile.Subject,
			DisplayName:         &oldName,
			LastAuthenticatedAt: now.Add(-time.Hour),
			CreatedAt:           now.Add(-time.Hour),
			UpdatedAt:           now.Add(-time.Hour),
		}
	service := newTestService(t, store, provider, now)

	login, err := service.BeginLogin()
	if err != nil {
		t.Fatalf("BeginLogin() error = %v", err)
	}
	if _, err := service.CompleteLogin(
		context.Background(),
		"authorization-code",
		login.State,
		login.State,
	); err != nil {
		t.Fatalf("CompleteLogin() error = %v", err)
	}

	updated := store.principals[principalID]
	if updated.DisplayName != newName || updated.Role != productidentity.RoleAdmin ||
		!updated.UpdatedAt.Equal(now) {
		t.Errorf("updated principal = %#v", updated)
	}
}

func TestServiceRejectsInvalidExpiredAndReplayedLoginState(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 19, 12, 0, 0, 0, time.UTC)
	provider := &recordingOIDCProvider{profile: OIDCProfile{
		Issuer:  "https://identity.example.com",
		Subject: "ada-subject",
	}}
	service := newTestService(t, newProductAuthStore(), provider, now)

	login, err := service.BeginLogin()
	if err != nil {
		t.Fatalf("BeginLogin() error = %v", err)
	}
	_, err = service.CompleteLogin(context.Background(), "authorization-code", login.State, "other-state")
	if !errors.Is(err, ErrInvalidLogin) {
		t.Fatalf("mismatched state error = %v, want invalid login", err)
	}

	service.now = func() time.Time { return now.Add(11 * time.Minute) }
	_, err = service.CompleteLogin(context.Background(), "authorization-code", login.State, login.State)
	if !errors.Is(err, ErrInvalidLogin) {
		t.Fatalf("expired state error = %v, want invalid login", err)
	}

	service.now = func() time.Time { return now }
	login, err = service.BeginLogin()
	if err != nil {
		t.Fatalf("BeginLogin() error = %v", err)
	}
	if _, err := service.CompleteLogin(
		context.Background(),
		"authorization-code",
		login.State,
		login.State,
	); err != nil {
		t.Fatalf("CompleteLogin() error = %v", err)
	}
	_, err = service.CompleteLogin(context.Background(), "authorization-code", login.State, login.State)
	if !errors.Is(err, ErrInvalidLogin) {
		t.Fatalf("replayed state error = %v, want invalid login", err)
	}
}

func TestServiceLimitsActiveLoginTransactions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 19, 12, 0, 0, 0, time.UTC)
	service := newTestService(t, newProductAuthStore(), &recordingOIDCProvider{}, now)
	for index := range maximumLoginTransactions {
		service.transactions[fmt.Sprintf("state-%d", index)] = loginTransaction{
			expiresAt: now.Add(time.Minute),
		}
	}

	_, err := service.BeginLogin()
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("BeginLogin() error = %v, want unavailable", err)
	}
}

func TestServiceRejectsUnenrolledAndNonUserOIDCIdentities(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 19, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		subject   string
		configure func(*productAuthStore, OIDCProfile)
		want      error
	}{
		{
			name:    "unenrolled subject",
			subject: "not-enrolled",
			want:    ErrEnrollmentDenied,
		},
		{
			name:    "identity linked to service account",
			subject: "ada-subject",
			configure: func(store *productAuthStore, profile OIDCProfile) {
				principal := productidentity.Principal{
					ID: uuid.New(), Kind: productidentity.KindServiceAccount,
					DisplayName: "Automation", Role: productidentity.RoleAdmin,
					CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour),
				}
				identity := productidentity.OIDCIdentity{
					ID: uuid.New(), PrincipalID: principal.ID,
					Issuer: profile.Issuer, Subject: profile.Subject,
					LastAuthenticatedAt: now.Add(-time.Hour),
					CreatedAt:           now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour),
				}
				store.principals[principal.ID] = principal
				store.identities[oidcIdentityKey(profile.Issuer, profile.Subject)] = identity
			},
			want: productidentity.ErrInvalid,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			profile := OIDCProfile{
				Issuer:  "https://identity.example.com",
				Subject: test.subject,
			}
			provider := &recordingOIDCProvider{profile: profile}
			store := newProductAuthStore()
			if test.configure != nil {
				test.configure(store, profile)
			}
			service := newTestService(t, store, provider, now)
			login, err := service.BeginLogin()
			if err != nil {
				t.Fatalf("BeginLogin() error = %v", err)
			}
			_, err = service.CompleteLogin(context.Background(), "authorization-code", login.State, login.State)
			if !errors.Is(err, test.want) {
				t.Fatalf("CompleteLogin() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestServiceRejectsInactiveSessions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 19, 12, 0, 0, 0, time.UTC)
	disabledAt := now.Add(-time.Minute)
	tests := []struct {
		name        string
		fill        byte
		disabledAt  *time.Time
		expiresAt   time.Time
		wantDeleted bool
	}{
		{
			name:       "disabled principal",
			fill:       80,
			disabledAt: &disabledAt,
			expiresAt:  now.Add(time.Hour),
		},
		{
			name:        "expired session",
			fill:        81,
			expiresAt:   now,
			wantDeleted: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			store := newProductAuthStore()
			service := newTestService(t, store, &recordingOIDCProvider{}, now)
			token := encodedSecret(test.fill)
			digest, err := sessionDigest(token)
			if err != nil {
				t.Fatalf("sessionDigest() error = %v", err)
			}
			principal := productidentity.Principal{
				ID: uuid.New(), Kind: productidentity.KindUser,
				DisplayName: "Test User", Role: productidentity.RoleViewer,
				DisabledAt: test.disabledAt,
				CreatedAt:  now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour),
			}
			store.principals[principal.ID] = principal
			store.sessions[digest] = productidentity.WebSession{
				ID: uuid.New(), PrincipalID: principal.ID, TokenDigest: digest,
				CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: test.expiresAt,
				LastSeenAt: now.Add(-time.Hour),
			}

			_, err = service.AuthenticateSession(context.Background(), token)
			if !errors.Is(err, productidentity.ErrUnauthenticated) {
				t.Fatalf("AuthenticateSession() error = %v, want unauthenticated", err)
			}
			if deleted := len(store.sessions) == 0; deleted != test.wantDeleted {
				t.Errorf("session deleted = %t, want %t", deleted, test.wantDeleted)
			}
		})
	}
}

func newTestService(t *testing.T, store *productAuthStore, provider OIDCProvider, now time.Time) *Service {
	t.Helper()

	cfg := DefaultConfig()
	cfg.OIDC = OIDCConfig{
		Issuer:       "https://identity.example.com",
		ClientID:     "slipramp",
		ClientSecret: "client-secret",
		RedirectURL:  "https://slipramp.example.com/api/v1/auth/callback",
		SubjectRoles: map[string]productidentity.Role{
			"ada-subject": productidentity.RoleAdmin,
		},
	}
	service, err := NewService(store, provider, cfg)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.now = func() time.Time { return now }
	secretIndex := byte(1)
	service.randomSecret = func() (string, error) {
		secret := encodedSecret(secretIndex)
		secretIndex++

		return secret, nil
	}

	return service
}

func encodedSecret(fill byte) string {
	value := make([]byte, randomSecretBytes)
	for index := range value {
		value[index] = fill
	}

	return base64.RawURLEncoding.EncodeToString(value)
}

type recordingOIDCProvider struct {
	profile       OIDCProfile
	err           error
	code          string
	codeVerifier  string
	expectedNonce string
}

func (*recordingOIDCProvider) AuthorizationURL(state, _ string, _ string) string {
	return "https://identity.example.com/authorize?state=" + state
}

func (p *recordingOIDCProvider) Authenticate(
	_ context.Context,
	code string,
	codeVerifier string,
	expectedNonce string,
) (OIDCProfile, error) {
	p.code = code
	p.codeVerifier = codeVerifier
	p.expectedNonce = expectedNonce

	return p.profile, p.err
}

type productAuthStore struct {
	principals map[uuid.UUID]productidentity.Principal
	identities map[string]productidentity.OIDCIdentity
	apiKeys    map[productidentity.CredentialDigest]productidentity.APIKey
	sessions   map[productidentity.CredentialDigest]productidentity.WebSession
	keyTouches int
	touchCount int
}

func newProductAuthStore() *productAuthStore {
	return &productAuthStore{
		principals: make(map[uuid.UUID]productidentity.Principal),
		identities: make(map[string]productidentity.OIDCIdentity),
		apiKeys:    make(map[productidentity.CredentialDigest]productidentity.APIKey),
		sessions:   make(map[productidentity.CredentialDigest]productidentity.WebSession),
	}
}

func (s *productAuthStore) CreatePrincipal(
	_ context.Context,
	principal productidentity.Principal,
) (productidentity.Principal, error) {
	s.principals[principal.ID] = principal

	return principal, nil
}

func (s *productAuthStore) GetPrincipal(_ context.Context, principalID uuid.UUID) (productidentity.Principal, error) {
	principal, found := s.principals[principalID]
	if !found {
		return productidentity.Principal{}, productidentity.ErrPrincipalNotFound
	}

	return principal, nil
}

func (s *productAuthStore) ListServiceAccounts(context.Context) ([]productidentity.Principal, error) {
	principals := make([]productidentity.Principal, 0)
	for _, principal := range s.principals {
		if principal.Kind == productidentity.KindServiceAccount {
			principals = append(principals, principal)
		}
	}

	return principals, nil
}

func (s *productAuthStore) GetOIDCUser(
	_ context.Context,
	issuer string,
	subject string,
) (productidentity.Principal, productidentity.OIDCIdentity, error) {
	identity, found := s.identities[oidcIdentityKey(issuer, subject)]
	if !found {
		return productidentity.Principal{}, productidentity.OIDCIdentity{},
			productidentity.ErrIdentityNotFound
	}

	return s.principals[identity.PrincipalID], identity, nil
}

func (s *productAuthStore) CreateOIDCUser(
	_ context.Context,
	principal productidentity.Principal,
	identity productidentity.OIDCIdentity,
) (productidentity.Principal, productidentity.OIDCIdentity, error) {
	key := oidcIdentityKey(identity.Issuer, identity.Subject)
	if _, found := s.identities[key]; found {
		return productidentity.Principal{}, productidentity.OIDCIdentity{},
			productidentity.ErrIdentityConflict
	}
	s.principals[principal.ID] = principal
	s.identities[key] = identity

	return principal, identity, nil
}

func (s *productAuthStore) UpdateOIDCUser(
	_ context.Context,
	principal productidentity.Principal,
	identity productidentity.OIDCIdentity,
) (productidentity.Principal, productidentity.OIDCIdentity, error) {
	s.principals[principal.ID] = principal
	s.identities[oidcIdentityKey(identity.Issuer, identity.Subject)] = identity

	return principal, identity, nil
}

func (s *productAuthStore) CreateAPIKey(
	_ context.Context,
	key productidentity.APIKey,
) (productidentity.APIKey, error) {
	if _, found := s.apiKeys[key.TokenDigest]; found {
		return productidentity.APIKey{}, productidentity.ErrCredentialConflict
	}
	for _, existing := range s.apiKeys {
		if existing.TokenPrefix == key.TokenPrefix {
			return productidentity.APIKey{}, productidentity.ErrCredentialConflict
		}
	}
	s.apiKeys[key.TokenDigest] = key

	return key, nil
}

func (s *productAuthStore) GetAPIKey(_ context.Context, keyID uuid.UUID) (productidentity.APIKey, error) {
	for _, key := range s.apiKeys {
		if key.ID == keyID {
			return key, nil
		}
	}

	return productidentity.APIKey{}, productidentity.ErrCredentialNotFound
}

func (s *productAuthStore) GetAPIKeyPrincipal(
	_ context.Context,
	digest productidentity.CredentialDigest,
) (productidentity.APIKey, productidentity.Principal, error) {
	key, found := s.apiKeys[digest]
	if !found {
		return productidentity.APIKey{}, productidentity.Principal{},
			productidentity.ErrCredentialNotFound
	}

	return key, s.principals[key.PrincipalID], nil
}

func (s *productAuthStore) ListAPIKeys(_ context.Context, principalID uuid.UUID) ([]productidentity.APIKey, error) {
	keys := make([]productidentity.APIKey, 0)
	for _, key := range s.apiKeys {
		if key.PrincipalID == principalID {
			keys = append(keys, key)
		}
	}

	return keys, nil
}

func (s *productAuthStore) TouchAPIKey(_ context.Context, keyID uuid.UUID, lastUsedAt time.Time) error {
	for digest, key := range s.apiKeys {
		if key.ID != keyID || key.RevokedAt != nil ||
			(key.ExpiresAt != nil && !lastUsedAt.Before(*key.ExpiresAt)) {
			continue
		}
		key.LastUsedAt = &lastUsedAt
		s.apiKeys[digest] = key
		s.keyTouches++

		return nil
	}

	return productidentity.ErrCredentialNotFound
}

func (s *productAuthStore) RevokeAPIKey(_ context.Context, keyID uuid.UUID, revokedAt time.Time) error {
	for digest, key := range s.apiKeys {
		if key.ID != keyID {
			continue
		}
		if key.RevokedAt == nil {
			key.RevokedAt = &revokedAt
			s.apiKeys[digest] = key
		}

		return nil
	}

	return productidentity.ErrCredentialNotFound
}

func (s *productAuthStore) CreateWebSession(
	_ context.Context,
	session productidentity.WebSession,
) (productidentity.WebSession, error) {
	if _, found := s.sessions[session.TokenDigest]; found {
		return productidentity.WebSession{}, productidentity.ErrCredentialConflict
	}
	s.sessions[session.TokenDigest] = session

	return session, nil
}

func (s *productAuthStore) GetWebSessionPrincipal(
	_ context.Context,
	digest productidentity.CredentialDigest,
) (productidentity.WebSession, productidentity.Principal, error) {
	session, found := s.sessions[digest]
	if !found {
		return productidentity.WebSession{}, productidentity.Principal{},
			productidentity.ErrCredentialNotFound
	}

	return session, s.principals[session.PrincipalID], nil
}

func (s *productAuthStore) TouchWebSession(_ context.Context, sessionID uuid.UUID, lastSeenAt time.Time) error {
	for digest, session := range s.sessions {
		if session.ID != sessionID {
			continue
		}
		session.LastSeenAt = lastSeenAt
		s.sessions[digest] = session
		s.touchCount++

		return nil
	}

	return productidentity.ErrCredentialNotFound
}

func (s *productAuthStore) DeleteWebSession(_ context.Context, sessionID uuid.UUID) error {
	for digest, session := range s.sessions {
		if session.ID == sessionID {
			delete(s.sessions, digest)

			return nil
		}
	}

	return productidentity.ErrCredentialNotFound
}

func oidcIdentityKey(issuer, subject string) string {
	return fmt.Sprintf("%s\x00%s", issuer, subject)
}
