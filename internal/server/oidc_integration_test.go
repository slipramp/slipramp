package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/slipramp/slipramp/internal/productidentity"
)

func configureIntegrationOIDC(t *testing.T, cfg *Config) {
	t.Helper()

	var issuer *httptest.Server
	issuer = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(writer, request)

			return
		}

		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(map[string]any{
			"issuer":                 issuer.URL,
			"authorization_endpoint": issuer.URL + "/authorize",
			"token_endpoint":         issuer.URL + "/token",
			"jwks_uri":               issuer.URL + "/keys",
		}); err != nil {
			t.Errorf("encode integration OIDC discovery document: %v", err)
		}
	}))
	t.Cleanup(issuer.Close)

	cfg.ProductAuthentication.OIDC.Issuer = issuer.URL
	cfg.ProductAuthentication.OIDC.ClientID = "slipramp-integration"
	cfg.ProductAuthentication.OIDC.ClientSecret = "integration-client-secret"
	cfg.ProductAuthentication.OIDC.RedirectURL =
		"http://slipramp.test/api/v1/auth/callback"
	cfg.ProductAuthentication.OIDC.SubjectRoles = map[string]productidentity.Role{
		"integration-admin": productidentity.RoleAdmin,
	}
}
