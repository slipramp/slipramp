package productauth

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/productidentity"
)

const oauthAccessTokenLabel = "OAuth access token"

// OAuthAccessToken describes identity and authorization claims from an access
// token after its issuer, signature, audience, and lifetime have been verified.
type OAuthAccessToken struct {
	CredentialID uuid.UUID
	Profile      OIDCProfile
	Scopes       []productidentity.Scope
}

// AuthenticateOAuthAccessToken maps a verified authorization-server identity
// to the same enrolled Principal used by browser OIDC login. The first OAuth
// authentication may provision that Principal; later requests reuse it.
func (s *Service) AuthenticateOAuthAccessToken(
	ctx context.Context,
	token OAuthAccessToken,
) (productidentity.Authentication, error) {
	if token.Profile.Issuer != s.cfg.OIDC.Issuer {
		return productidentity.Authentication{}, ErrEnrollmentDenied
	}
	role, enrolled := s.cfg.OIDC.SubjectRoles[token.Profile.Subject]
	if !enrolled {
		return productidentity.Authentication{}, ErrEnrollmentDenied
	}

	principal, identity, err := s.store.GetOIDCUser(ctx, token.Profile.Issuer, token.Profile.Subject)
	if errors.Is(err, productidentity.ErrIdentityNotFound) {
		principal, err = s.resolveOIDCUser(ctx, token.Profile, role)
	} else if err != nil {
		return productidentity.Authentication{}, fmt.Errorf("get OAuth Principal: %w", err)
	} else if principal.DisabledAt != nil {
		return productidentity.Authentication{}, productidentity.ErrUnauthenticated
	} else if principal.Role != role {
		displayName, displayNameErr := profileDisplayName(token.Profile)
		if displayNameErr != nil {
			return productidentity.Authentication{}, displayNameErr
		}
		principal, err = s.updateOIDCUser(
			ctx,
			principal,
			identity,
			token.Profile,
			role,
			displayName,
			s.now().UTC(),
		)
	}
	if err != nil {
		return productidentity.Authentication{}, fmt.Errorf("resolve OAuth Principal: %w", err)
	}
	if principal.DisabledAt != nil {
		return productidentity.Authentication{}, productidentity.ErrUnauthenticated
	}

	authentication := productidentity.Authentication{
		Principal:       principal,
		Method:          productidentity.AuthenticationMethodOAuthToken,
		CredentialID:    token.CredentialID,
		CredentialLabel: oauthAccessTokenLabel,
		Scopes:          token.Scopes,
	}
	if err := authentication.Validate(); err != nil {
		return productidentity.Authentication{}, fmt.Errorf("validate OAuth authentication: %w", err)
	}

	return authentication, nil
}
