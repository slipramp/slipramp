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

	credential, err := service.CompleteLogin(
		context.Background(),
		"authorization-code",
		login.State,
		login.State,
	)
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

	authentication, err := service.AuthenticateSession(
		context.Background(),
		credential.Token,
	)
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
	_, err = service.CompleteLogin(
		context.Background(),
		"authorization-code",
		login.State,
		"other-state",
	)
	if !errors.Is(err, ErrInvalidLogin) {
		t.Fatalf("mismatched state error = %v, want invalid login", err)
	}

	service.now = func() time.Time { return now.Add(11 * time.Minute) }
	_, err = service.CompleteLogin(
		context.Background(),
		"authorization-code",
		login.State,
		login.State,
	)
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
	_, err = service.CompleteLogin(
		context.Background(),
		"authorization-code",
		login.State,
		login.State,
	)
	if !errors.Is(err, ErrInvalidLogin) {
		t.Fatalf("replayed state error = %v, want invalid login", err)
	}
}

func TestServiceLimitsActiveLoginTransactions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 19, 12, 0, 0, 0, time.UTC)
	service := newTestService(
		t,
		newProductAuthStore(),
		&recordingOIDCProvider{},
		now,
	)
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

func TestServiceRejectsUnenrolledAndDisabledUsers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 19, 12, 0, 0, 0, time.UTC)
	provider := &recordingOIDCProvider{profile: OIDCProfile{
		Issuer:  "https://identity.example.com",
		Subject: "not-enrolled",
	}}
	store := newProductAuthStore()
	service := newTestService(t, store, provider, now)
	login, err := service.BeginLogin()
	if err != nil {
		t.Fatalf("BeginLogin() error = %v", err)
	}
	_, err = service.CompleteLogin(
		context.Background(),
		"authorization-code",
		login.State,
		login.State,
	)
	if !errors.Is(err, ErrEnrollmentDenied) {
		t.Fatalf("unenrolled login error = %v, want enrollment denied", err)
	}

	disabledAt := now.Add(-time.Minute)
	token := encodedSecret(90)
	digest, err := sessionDigest(token)
	if err != nil {
		t.Fatalf("sessionDigest() error = %v", err)
	}
	principalID := uuid.New()
	sessionID := uuid.New()
	store.principals[principalID] = productidentity.Principal{
		ID:          principalID,
		Kind:        productidentity.KindUser,
		DisplayName: "Disabled User",
		Role:        productidentity.RoleAdmin,
		DisabledAt:  &disabledAt,
		CreatedAt:   now.Add(-time.Hour),
		UpdatedAt:   now.Add(-time.Hour),
	}
	store.sessions[digest] = productidentity.WebSession{
		ID:          sessionID,
		PrincipalID: principalID,
		TokenDigest: digest,
		CreatedAt:   now.Add(-time.Hour),
		ExpiresAt:   now.Add(time.Hour),
		LastSeenAt:  now.Add(-time.Minute),
	}
	_, err = service.AuthenticateSession(context.Background(), token)
	if !errors.Is(err, productidentity.ErrUnauthenticated) {
		t.Fatalf("disabled session error = %v, want unauthenticated", err)
	}
}

func TestServiceExpiresSession(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 19, 12, 0, 0, 0, time.UTC)
	store := newProductAuthStore()
	service := newTestService(t, store, &recordingOIDCProvider{}, now)
	token := encodedSecret(80)
	digest, err := sessionDigest(token)
	if err != nil {
		t.Fatalf("sessionDigest() error = %v", err)
	}
	principalID := uuid.New()
	sessionID := uuid.New()
	store.principals[principalID] = productidentity.Principal{
		ID: principalID, Kind: productidentity.KindUser,
		DisplayName: "Expired User", Role: productidentity.RoleViewer,
		CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour),
	}
	store.sessions[digest] = productidentity.WebSession{
		ID: sessionID, PrincipalID: principalID, TokenDigest: digest,
		CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: now,
		LastSeenAt: now.Add(-time.Hour),
	}

	_, err = service.AuthenticateSession(context.Background(), token)
	if !errors.Is(err, productidentity.ErrUnauthenticated) {
		t.Fatalf("expired session error = %v, want unauthenticated", err)
	}
	if len(store.sessions) != 0 {
		t.Errorf("expired session was not deleted")
	}
}

func newTestService(
	t *testing.T,
	store *productAuthStore,
	provider OIDCProvider,
	now time.Time,
) *Service {
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
	sessions   map[productidentity.CredentialDigest]productidentity.WebSession
	touchCount int
}

func newProductAuthStore() *productAuthStore {
	return &productAuthStore{
		principals: make(map[uuid.UUID]productidentity.Principal),
		identities: make(map[string]productidentity.OIDCIdentity),
		sessions:   make(map[productidentity.CredentialDigest]productidentity.WebSession),
	}
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

func (s *productAuthStore) TouchWebSession(
	_ context.Context,
	sessionID uuid.UUID,
	lastSeenAt time.Time,
) error {
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

func (s *productAuthStore) DeleteWebSession(
	_ context.Context,
	sessionID uuid.UUID,
) error {
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
