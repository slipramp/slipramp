package mcpserver

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
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

func TestDiscoveredOAuthAccessTokenVerifier(t *testing.T) {
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

func TestDiscoverOAuthAccessTokenVerifierRequiresClientCapabilities(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		configure func(*testOAuthIssuer)
		wantError string
	}{
		{
			name: "dynamic client registration",
			configure: func(issuer *testOAuthIssuer) {
				issuer.registrationEndpoint = ""
			},
			wantError: "dynamic client registration",
		},
		{
			name: "S256 PKCE",
			configure: func(issuer *testOAuthIssuer) {
				issuer.codeChallengeMethods = nil
			},
			wantError: "S256 PKCE",
		},
		{
			name: "MCP scopes",
			configure: func(issuer *testOAuthIssuer) {
				issuer.scopesSupported = []string{"slipramp:read"}
			},
			wantError: "slipramp:runs:write",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			issuer := newTestOAuthIssuer(t)
			test.configure(issuer)
			ctx := oidc.ClientContext(
				context.Background(),
				&http.Client{Transport: roundTripFunc(issuer.roundTrip)},
			)
			_, err := DiscoverOAuthAccessTokenVerifier(ctx, OAuthConfig{
				Issuer:   issuer.issuer,
				Resource: issuer.resource,
			})
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("DiscoverOAuthAccessTokenVerifier() error = %v, want %q", err, test.wantError)
			}
		})
	}
}

func TestDiscoveredOAuthAccessTokenVerifierRejectsWrongAudience(t *testing.T) {
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

func TestDiscoveredOAuthAccessTokenVerifierRejectsMissingMCPScopes(t *testing.T) {
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

func TestOAuthConfigAndMetadataURL(t *testing.T) {
	t.Parallel()

	if got := OAuthMetadataURL("https://slipramp.example.com/mcp"); got != "https://slipramp.example.com/.well-known/oauth-protected-resource/mcp" {
		t.Errorf("OAuthMetadataURL() = %q", got)
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

func TestLocalKeycloakRealmMCPTokenClaims(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile(filepath.Join("..", "..", "deploy", "local", "keycloak-realm.json"))
	if err != nil {
		t.Fatalf("read local Keycloak realm: %v", err)
	}
	var realm struct {
		DefaultDefaultClientScopes []string `json:"defaultDefaultClientScopes"`
		ClientScopes               []struct {
			Name            string `json:"name"`
			ProtocolMappers []struct {
				ProtocolMapper string            `json:"protocolMapper"`
				Config         map[string]string `json:"config"`
			} `json:"protocolMappers"`
		} `json:"clientScopes"`
	}
	if err := json.Unmarshal(contents, &realm); err != nil {
		t.Fatalf("decode local Keycloak realm: %v", err)
	}
	if !slices.Contains(realm.DefaultDefaultClientScopes, "basic") {
		t.Fatal("local Keycloak realm does not apply the basic client scope by default")
	}

	resource := "http://localhost:8080/mcp"
	claims := map[string]bool{
		"subject":       false,
		"readAudience":  false,
		"writeAudience": false,
	}
	for _, scope := range realm.ClientScopes {
		for _, mapper := range scope.ProtocolMappers {
			switch {
			case scope.Name == "basic" && mapper.ProtocolMapper == "oidc-sub-mapper":
				claims["subject"] = mapper.Config["access.token.claim"] == "true"
			case scope.Name == "slipramp:read" && mapper.ProtocolMapper == "oidc-audience-mapper":
				claims["readAudience"] = mapper.Config["included.custom.audience"] == resource
			case scope.Name == "slipramp:runs:write" && mapper.ProtocolMapper == "oidc-audience-mapper":
				claims["writeAudience"] = mapper.Config["included.custom.audience"] == resource
			}
		}
	}
	for claim, configured := range claims {
		if !configured {
			t.Errorf("local Keycloak MCP %s claim mapper is not configured", claim)
		}
	}
}

type testOAuthIssuer struct {
	issuer               string
	resource             string
	audience             string
	registrationEndpoint string
	codeChallengeMethods []string
	scopesSupported      []string
	tokenScopes          string
	expiry               time.Time
	key                  *rsa.PrivateKey
}

func newTestOAuthIssuer(t *testing.T) *testOAuthIssuer {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey() error = %v", err)
	}
	resource := "https://slipramp.example.com/mcp"

	return &testOAuthIssuer{
		issuer:               "https://identity.example.com/realms/slipramp",
		resource:             resource,
		audience:             resource,
		registrationEndpoint: "https://identity.example.com/realms/slipramp/register",
		codeChallengeMethods: []string{"S256"},
		scopesSupported:      OAuthScopeNames(),
		tokenScopes:          "openid slipramp:read slipramp:runs:write unknown",
		expiry:               time.Now().Add(time.Hour).Truncate(time.Second),
		key:                  key,
	}
}

func (i *testOAuthIssuer) roundTrip(request *http.Request) (*http.Response, error) {
	var value any
	switch request.URL.Path {
	case "/realms/slipramp/.well-known/openid-configuration":
		value = map[string]any{
			"issuer":                           i.issuer,
			"authorization_endpoint":           i.issuer + "/authorize",
			"token_endpoint":                   i.issuer + "/token",
			"jwks_uri":                         i.issuer + "/keys",
			"registration_endpoint":            i.registrationEndpoint,
			"code_challenge_methods_supported": i.codeChallengeMethods,
			"scopes_supported":                 i.scopesSupported,
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
