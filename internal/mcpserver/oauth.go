package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/productauth"
	"github.com/slipramp/slipramp/internal/productidentity"
)

var oauthScopes = []productidentity.Scope{
	productidentity.ScopeRead,
	productidentity.ScopeRunsWrite,
}

type Config struct {
	OAuth OAuthConfig
}

func (c Config) Validate() error {
	return c.OAuth.Validate()
}

type OAuthConfig struct {
	Issuer   string
	Resource string
}

func (c OAuthConfig) Enabled() bool {
	return c.Issuer != "" || c.Resource != ""
}

func (c OAuthConfig) Validate() error {
	if !c.Enabled() {
		return nil
	}
	if err := validateOAuthURL("issuer", c.Issuer); err != nil {
		return err
	}
	if err := validateOAuthURL("resource", c.Resource); err != nil {
		return err
	}

	resource, _ := url.Parse(c.Resource)
	if resource.Path != "/mcp" {
		return errors.New("MCP OAuth resource URL path must be /mcp")
	}

	return nil
}

func validateOAuthURL(label string, value string) error {
	parsed, err := url.Parse(value)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" {
		return fmt.Errorf("MCP OAuth %s must be an absolute URL", label)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("MCP OAuth %s must use HTTP or HTTPS", label)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("MCP OAuth %s must not contain user information, a query, or a fragment", label)
	}

	return nil
}

func OAuthMetadataURL(resource string) string {
	parsed, _ := url.Parse(resource)
	parsed.Path = "/.well-known/oauth-protected-resource" + parsed.EscapedPath()
	parsed.RawPath = ""

	return parsed.String()
}

type OAuthAccessTokenVerifier interface {
	VerifyAccessToken(context.Context, string) (VerifiedOAuthAccessToken, error)
}

type VerifiedOAuthAccessToken struct {
	Credential productauth.OAuthAccessToken
	Expiration time.Time
}

type discoveredOAuthAccessTokenVerifier struct {
	issuer   string
	verifier *oidc.IDTokenVerifier
}

func DiscoverOAuthAccessTokenVerifier(
	ctx context.Context,
	cfg OAuthConfig,
) (OAuthAccessTokenVerifier, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if !cfg.Enabled() {
		return nil, nil
	}

	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("discover MCP OAuth authorization server: %w", err)
	}
	var metadata struct {
		RegistrationEndpoint          string   `json:"registration_endpoint"`
		ScopesSupported               []string `json:"scopes_supported"`
		CodeChallengeMethodsSupported []string `json:"code_challenge_methods_supported"`
	}
	if err := provider.Claims(&metadata); err != nil {
		return nil, fmt.Errorf("decode MCP OAuth authorization-server metadata: %w", err)
	}
	if metadata.RegistrationEndpoint == "" {
		return nil, errors.New("MCP OAuth authorization server does not advertise dynamic client registration")
	}
	if !slices.Contains(metadata.CodeChallengeMethodsSupported, "S256") {
		return nil, errors.New("MCP OAuth authorization server does not advertise S256 PKCE")
	}
	for _, scope := range oauthScopes {
		if !slices.Contains(metadata.ScopesSupported, string(scope)) {
			return nil, fmt.Errorf("MCP OAuth authorization server does not advertise scope %q", scope)
		}
	}

	return &discoveredOAuthAccessTokenVerifier{
		issuer: cfg.Issuer,
		verifier: provider.Verifier(&oidc.Config{
			ClientID: cfg.Resource,
		}),
	}, nil
}

func (v *discoveredOAuthAccessTokenVerifier) VerifyAccessToken(
	ctx context.Context,
	rawToken string,
) (VerifiedOAuthAccessToken, error) {
	token, err := v.verifier.Verify(ctx, rawToken)
	if err != nil {
		return VerifiedOAuthAccessToken{}, fmt.Errorf("verify OAuth access token: %w", err)
	}

	var claims struct {
		Subject           string  `json:"sub"`
		Scope             string  `json:"scope"`
		Email             *string `json:"email"`
		PreferredUsername *string `json:"preferred_username"`
		DisplayName       *string `json:"name"`
	}
	if err := token.Claims(&claims); err != nil {
		return VerifiedOAuthAccessToken{}, fmt.Errorf("decode OAuth access token claims: %w", err)
	}
	if strings.TrimSpace(claims.Subject) == "" {
		return VerifiedOAuthAccessToken{}, errors.New("OAuth access token subject is missing")
	}

	grantedScopes := strings.Fields(claims.Scope)
	scopes := make([]productidentity.Scope, 0, len(oauthScopes))
	for _, supported := range oauthScopes {
		if slices.Contains(grantedScopes, string(supported)) {
			scopes = append(scopes, supported)
		}
	}
	if len(scopes) == 0 {
		return VerifiedOAuthAccessToken{}, errors.New("OAuth access token grants no supported MCP scopes")
	}

	credentialID := uuid.NewSHA1(uuid.NameSpaceURL, []byte(v.issuer+"\x00"+rawToken))

	return VerifiedOAuthAccessToken{
		Credential: productauth.OAuthAccessToken{
			CredentialID: credentialID,
			Profile: productauth.OIDCProfile{
				Issuer:            v.issuer,
				Subject:           claims.Subject,
				Email:             claims.Email,
				PreferredUsername: claims.PreferredUsername,
				DisplayName:       claims.DisplayName,
			},
			Scopes: scopes,
		},
		Expiration: token.Expiry,
	}, nil
}

func OAuthScopeNames() []string {
	names := make([]string, 0, len(oauthScopes))
	for _, scope := range oauthScopes {
		names = append(names, string(scope))
	}

	return names
}
