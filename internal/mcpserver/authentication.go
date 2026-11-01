package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"

	"github.com/slipramp/slipramp/internal/productauth"
	"github.com/slipramp/slipramp/internal/productidentity"
)

const authenticationExtraKey = "slipramp.authentication"

type AuthenticationService interface {
	AuthenticateAPIKey(context.Context, string) (productidentity.Authentication, error)
	AuthenticateOAuthAccessToken(
		context.Context,
		productauth.OAuthAccessToken,
	) (productidentity.Authentication, error)
}

func bearerTokenVerifier(
	authenticationService AuthenticationService,
	oauthAccessTokens OAuthAccessTokenVerifier,
	logger *slog.Logger,
) auth.TokenVerifier {
	return func(ctx context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		if !strings.HasPrefix(token, "slipramp_key_") {
			return verifyOAuthAccessToken(ctx, authenticationService, oauthAccessTokens, token, logger)
		}

		authentication, err := authenticationService.AuthenticateAPIKey(ctx, token)
		if errors.Is(err, productidentity.ErrUnauthenticated) {
			return nil, fmt.Errorf("%w: invalid Slipramp API key", auth.ErrInvalidToken)
		}
		if err != nil {
			logger.Error("authenticate MCP API key", "error", err)

			return nil, errors.New("Slipramp API key authentication failed") //nolint:staticcheck // Product name.
		}
		if err := authentication.Validate(); err != nil {
			logger.Error("validate MCP API key authentication", "error", err)

			return nil, errors.New("Slipramp API key authentication failed") //nolint:staticcheck // Product name.
		}
		if authentication.Method != productidentity.AuthenticationMethodAPIKey {
			logger.Error(
				"validate MCP API key authentication",
				"error", "authentication method is not API key",
			)

			return nil, errors.New("Slipramp API key authentication failed") //nolint:staticcheck // Product name.
		}

		return tokenInfo(authentication, time.Time{}), nil
	}
}

func verifyOAuthAccessToken(
	ctx context.Context,
	authenticationService AuthenticationService,
	verifier OAuthAccessTokenVerifier,
	rawToken string,
	logger *slog.Logger,
) (*auth.TokenInfo, error) {
	if verifier == nil {
		return nil, fmt.Errorf("%w: invalid Bearer credential", auth.ErrInvalidToken)
	}

	verified, err := verifier.VerifyAccessToken(ctx, rawToken)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid OAuth access token", auth.ErrInvalidToken)
	}
	authentication, err := authenticationService.AuthenticateOAuthAccessToken(ctx, verified.Credential)
	if errors.Is(err, productauth.ErrEnrollmentDenied) ||
		errors.Is(err, productidentity.ErrUnauthenticated) {
		return nil, fmt.Errorf("%w: OAuth identity is not authorized", auth.ErrInvalidToken)
	}
	if err != nil {
		logger.Error("authenticate MCP OAuth access token", "error", err)

		return nil, errors.New("Slipramp OAuth authentication failed") //nolint:staticcheck // Product name.
	}
	if err := authentication.Validate(); err != nil {
		logger.Error("validate MCP OAuth authentication", "error", err)

		return nil, errors.New("Slipramp OAuth authentication failed") //nolint:staticcheck // Product name.
	}
	if authentication.Method != productidentity.AuthenticationMethodOAuthToken {
		logger.Error(
			"validate MCP OAuth authentication",
			"error", "authentication method is not OAuth access token",
		)

		return nil, errors.New("Slipramp OAuth authentication failed") //nolint:staticcheck // Product name.
	}

	return tokenInfo(authentication, verified.Expiration), nil
}

func tokenInfo(authentication productidentity.Authentication, expiration time.Time) *auth.TokenInfo {
	scopes := make([]string, 0, len(authentication.Scopes))
	for _, scope := range authentication.Scopes {
		scopes = append(scopes, string(scope))
	}

	return &auth.TokenInfo{
		Scopes:     scopes,
		Expiration: expiration,
		UserID:     authentication.Principal.ID.String(),
		Extra: map[string]any{
			authenticationExtraKey: authentication,
		},
	}
}

func currentAuthentication(ctx context.Context) (productidentity.Authentication, error) {
	token := auth.TokenInfoFromContext(ctx)
	if token == nil {
		return productidentity.Authentication{}, errors.New("authenticated Slipramp identity is missing")
	}
	authentication, ok := token.Extra[authenticationExtraKey].(productidentity.Authentication)
	if !ok {
		return productidentity.Authentication{}, errors.New("authenticated Slipramp identity is invalid")
	}

	return authentication, nil
}

func authorize(ctx context.Context, permission productidentity.Permission) (productidentity.Authentication, error) {
	authentication, err := currentAuthentication(ctx)
	if err != nil {
		return productidentity.Authentication{}, err
	}
	if !authentication.Authorizes(permission) {
		return productidentity.Authentication{}, errors.New(
			"the authenticated credential is not authorized for this operation",
		)
	}

	return authentication, nil
}
