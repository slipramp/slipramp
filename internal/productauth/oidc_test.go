package productauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/slipramp/slipramp/internal/productidentity"
)

func TestDiscoveredOIDCProviderValidatesClaims(t *testing.T) {
	t.Parallel()

	issuer := newTestOIDCIssuer(t)
	clientContext := oidc.ClientContext(
		context.Background(),
		&http.Client{Transport: roundTripFunc(issuer.roundTrip)},
	)
	cfg := OIDCConfig{
		Issuer:       issuer.issuer,
		ClientID:     "slipramp",
		ClientSecret: "client-secret",
		RedirectURL:  "http://slipramp.example.com/api/v1/auth/callback",
		SubjectRoles: map[string]productidentity.Role{
			"ada-subject": productidentity.RoleAdmin,
		},
	}
	provider, err := DiscoverOIDCProvider(clientContext, cfg)
	if err != nil {
		t.Fatalf("DiscoverOIDCProvider() error = %v", err)
	}

	profile, err := provider.Authenticate(
		clientContext,
		"authorization-code",
		"code-verifier",
		issuer.nonce,
	)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if profile.Issuer != issuer.issuer || profile.Subject != "ada-subject" ||
		profile.DisplayName == nil || *profile.DisplayName != "Ada Lovelace" {
		t.Errorf("profile = %#v", profile)
	}

	tests := []struct {
		name      string
		configure func(*testOIDCIssuer)
		nonce     string
	}{
		{
			name: "audience",
			configure: func(issuer *testOIDCIssuer) {
				issuer.audience = "other-client"
			},
			nonce: issuer.nonce,
		},
		{
			name:  "nonce",
			nonce: "other-nonce",
		},
		{
			name: "expiry",
			configure: func(issuer *testOIDCIssuer) {
				issuer.expiry = time.Now().Add(-time.Hour)
			},
			nonce: issuer.nonce,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			originalAudience := issuer.audience
			originalExpiry := issuer.expiry
			t.Cleanup(func() {
				issuer.audience = originalAudience
				issuer.expiry = originalExpiry
			})
			if test.configure != nil {
				test.configure(issuer)
			}

			_, err := provider.Authenticate(
				clientContext,
				"authorization-code",
				"code-verifier",
				test.nonce,
			)
			if !isInvalidLogin(err) {
				t.Fatalf("Authenticate() error = %v, want invalid login", err)
			}
		})
	}
}

type testOIDCIssuer struct {
	issuer   string
	key      *rsa.PrivateKey
	audience string
	nonce    string
	expiry   time.Time
}

func newTestOIDCIssuer(t *testing.T) *testOIDCIssuer {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey() error = %v", err)
	}
	issuer := &testOIDCIssuer{
		issuer:   "https://identity.example.com",
		key:      key,
		audience: "slipramp",
		nonce:    "expected-nonce",
		expiry:   time.Now().Add(time.Hour),
	}
	return issuer
}

func (i *testOIDCIssuer) roundTrip(request *http.Request) (*http.Response, error) {
	var value any
	switch request.URL.Path {
	case "/.well-known/openid-configuration":
		value = map[string]any{
			"issuer":                 i.issuer,
			"authorization_endpoint": i.issuer + "/authorize",
			"token_endpoint":         i.issuer + "/token",
			"jwks_uri":               i.issuer + "/keys",
		}
	case "/keys":
		value = jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
			Key:       &i.key.PublicKey,
			KeyID:     "test-key",
			Algorithm: string(jose.RS256),
			Use:       "sig",
		}}}
	case "/token":
		value = map[string]any{
			"access_token": "access-token",
			"token_type":   "Bearer",
			"id_token":     i.signedIDToken(),
		}
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

func (i *testOIDCIssuer) signedIDToken() string {
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
			"nonce": i.nonce,
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
