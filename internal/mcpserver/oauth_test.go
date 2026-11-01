package mcpserver

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/productidentity"
)

func TestOAuthAccessTokenVerifier(t *testing.T) {
	t.Parallel()

	issuer := newTestOAuthIssuer(t)
	ctx := oidc.ClientContext(
		context.Background(),
		&http.Client{Transport: roundTripFunc(issuer.roundTrip)},
	)
	verifier, err := DiscoverOAuthAccessTokenVerifier(ctx, OAuthConfig{
		Issuer:   issuer.issuer,
		Resource: issuer.resource,
	})
	if err != nil {
		t.Fatalf("DiscoverOAuthAccessTokenVerifier() error = %v", err)
	}

	verified, err := verifier.VerifyAccessToken(ctx, issuer.signedAccessToken())
	if err != nil {
		t.Fatalf("VerifyAccessToken() error = %v", err)
	}
	if verified.Credential.Profile.Issuer != issuer.issuer ||
		verified.Credential.Profile.Subject != "ada-subject" ||
		verified.Credential.CredentialID == uuid.Nil ||
		!slices.Equal(verified.Credential.Scopes, []productidentity.Scope{
			productidentity.ScopeRead,
			productidentity.ScopeRunsWrite,
		}) || !verified.Expiration.Equal(issuer.expiry) {
		t.Errorf("verified token = %#v", verified)
	}
}

func TestOAuthVerifierRejectsWrongAudience(t *testing.T) {
	t.Parallel()

	issuer := newTestOAuthIssuer(t)
	ctx := oidc.ClientContext(
		context.Background(),
		&http.Client{Transport: roundTripFunc(issuer.roundTrip)},
	)
	verifier, err := DiscoverOAuthAccessTokenVerifier(ctx, OAuthConfig{
		Issuer:   issuer.issuer,
		Resource: issuer.resource,
	})
	if err != nil {
		t.Fatalf("DiscoverOAuthAccessTokenVerifier() error = %v", err)
	}
	issuer.audience = "https://other.example.com/mcp"

	if _, err := verifier.VerifyAccessToken(ctx, issuer.signedAccessToken()); err == nil {
		t.Fatal("VerifyAccessToken() error = nil, want audience rejection")
	}
}

func TestOAuthVerifierRejectsUnsupportedScopes(t *testing.T) {
	t.Parallel()

	issuer := newTestOAuthIssuer(t)
	issuer.tokenScopes = "openid profile"
	ctx := oidc.ClientContext(
		context.Background(),
		&http.Client{Transport: roundTripFunc(issuer.roundTrip)},
	)
	verifier, err := DiscoverOAuthAccessTokenVerifier(ctx, OAuthConfig{
		Issuer:   issuer.issuer,
		Resource: issuer.resource,
	})
	if err != nil {
		t.Fatalf("DiscoverOAuthAccessTokenVerifier() error = %v", err)
	}

	if _, err := verifier.VerifyAccessToken(ctx, issuer.signedAccessToken()); err == nil ||
		!strings.Contains(err.Error(), "no supported MCP scopes") {
		t.Fatalf("VerifyAccessToken() error = %v", err)
	}
}

func TestOAuthConfigAndMetadata(t *testing.T) {
	t.Parallel()

	if got := ProtectedResourceMetadataURL("https://slipramp.example.com/mcp"); got != "https://slipramp.example.com/.well-known/oauth-protected-resource/mcp" {
		t.Errorf("ProtectedResourceMetadataURL() = %q", got)
	}
	if got := ProtectedResourceMetadataPath("https://slipramp.example.com/mcp"); got != "/.well-known/oauth-protected-resource/mcp" {
		t.Errorf("ProtectedResourceMetadataPath() = %q", got)
	}

	tests := []struct {
		name string
		cfg  OAuthConfig
	}{
		{name: "partial", cfg: OAuthConfig{Issuer: "https://identity.example.com"}},
		{name: "resource path", cfg: OAuthConfig{
			Issuer: "https://identity.example.com", Resource: "https://slipramp.example.com/other",
		}},
		{name: "resource query", cfg: OAuthConfig{
			Issuer: "https://identity.example.com", Resource: "https://slipramp.example.com/mcp?a=b",
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if err := test.cfg.Validate(); err == nil {
				t.Error("Validate() error = nil")
			}
		})
	}
}

type testOAuthIssuer struct {
	issuer      string
	resource    string
	audience    string
	tokenScopes string
	expiry      time.Time
	key         *rsa.PrivateKey
}

func newTestOAuthIssuer(t *testing.T) *testOAuthIssuer {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey() error = %v", err)
	}
	resource := "https://slipramp.example.com/mcp"

	return &testOAuthIssuer{
		issuer:      "https://identity.example.com/realms/slipramp",
		resource:    resource,
		audience:    resource,
		tokenScopes: "openid slipramp:read slipramp:runs:write unknown",
		expiry:      time.Now().Add(time.Hour).Truncate(time.Second),
		key:         key,
	}
}

func (i *testOAuthIssuer) roundTrip(request *http.Request) (*http.Response, error) {
	var value any
	switch request.URL.Path {
	case "/realms/slipramp/.well-known/openid-configuration":
		value = map[string]any{
			"issuer":                 i.issuer,
			"authorization_endpoint": i.issuer + "/authorize",
			"token_endpoint":         i.issuer + "/token",
			"jwks_uri":               i.issuer + "/keys",
		}
	case "/realms/slipramp/keys":
		value = jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
			Key:       &i.key.PublicKey,
			KeyID:     "test-key",
			Algorithm: string(jose.RS256),
			Use:       "sig",
		}}}
	default:
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader("not found")),
			Header:     make(http.Header),
			Request:    request,
		}, nil
	}

	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(string(encoded))),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Request:    request,
	}, nil
}

func (i *testOAuthIssuer) signedAccessToken() string {
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: i.key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "test-key"),
	)
	if err != nil {
		panic(err)
	}

	token, err := jwt.Signed(signer).
		Claims(jwt.Claims{
			Issuer:   i.issuer,
			Subject:  "ada-subject",
			Audience: jwt.Audience{i.audience},
			Expiry:   jwt.NewNumericDate(i.expiry),
			IssuedAt: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
		}).
		Claims(map[string]any{
			"scope": i.tokenScopes,
			"name":  "Ada Lovelace",
		}).
		Serialize()
	if err != nil {
		panic(err)
	}

	return token
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
