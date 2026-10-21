package productauth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type OIDCProfile struct {
	Issuer            string
	Subject           string
	Email             *string
	PreferredUsername *string
	DisplayName       *string
}

type OIDCProvider interface {
	AuthorizationURL(state, nonce, codeVerifier string) string
	Authenticate(
		ctx context.Context,
		code string,
		codeVerifier string,
		expectedNonce string,
	) (OIDCProfile, error)
}

type discoveredOIDCProvider struct {
	issuer      string
	oauthConfig oauth2.Config
	verifier    *oidc.IDTokenVerifier
}

func DiscoverOIDCProvider(
	ctx context.Context,
	cfg OIDCConfig,
) (OIDCProvider, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("discover OIDC provider: %w", err)
	}

	return &discoveredOIDCProvider{
		issuer: cfg.Issuer,
		oauthConfig: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  cfg.RedirectURL,
			Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
		},
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
	}, nil
}

func (p *discoveredOIDCProvider) AuthorizationURL(
	state string,
	nonce string,
	codeVerifier string,
) string {
	return p.oauthConfig.AuthCodeURL(
		state,
		oidc.Nonce(nonce),
		oauth2.AccessTypeOnline,
		oauth2.S256ChallengeOption(codeVerifier),
	)
}

func (p *discoveredOIDCProvider) Authenticate(
	ctx context.Context,
	code string,
	codeVerifier string,
	expectedNonce string,
) (OIDCProfile, error) {
	token, err := p.oauthConfig.Exchange(
		ctx,
		code,
		oauth2.VerifierOption(codeVerifier),
	)
	if err != nil {
		if _, ok := errors.AsType[*oauth2.RetrieveError](err); ok {
			return OIDCProfile{}, fmt.Errorf(
				"%w: exchange authorization code: %w",
				ErrInvalidLogin,
				err,
			)
		}

		return OIDCProfile{}, fmt.Errorf("exchange OIDC authorization code: %w", err)
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return OIDCProfile{}, fmt.Errorf("%w: OIDC response is missing an ID token", ErrInvalidLogin)
	}

	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return OIDCProfile{}, fmt.Errorf("%w: verify ID token", ErrInvalidLogin)
	}
	if subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(expectedNonce)) != 1 {
		return OIDCProfile{}, fmt.Errorf("%w: ID token nonce does not match", ErrInvalidLogin)
	}

	var claims struct {
		Subject           string  `json:"sub"`
		Email             *string `json:"email"`
		PreferredUsername *string `json:"preferred_username"`
		DisplayName       *string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return OIDCProfile{}, fmt.Errorf("%w: decode ID token claims", ErrInvalidLogin)
	}
	if claims.Subject == "" {
		return OIDCProfile{}, fmt.Errorf("%w: ID token subject is missing", ErrInvalidLogin)
	}

	return OIDCProfile{
		Issuer:            p.issuer,
		Subject:           claims.Subject,
		Email:             claims.Email,
		PreferredUsername: claims.PreferredUsername,
		DisplayName:       claims.DisplayName,
	}, nil
}
