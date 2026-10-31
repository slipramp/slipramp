package handler

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"

	"github.com/slipramp/slipramp/internal/controlleridentity"
)

type controllerAuthenticator struct {
	identities             controllerIdentityService
	enrollmentTokenDigests map[string][sha256.Size]byte
}

type controllerIdentityService interface {
	AuthenticateController(ctx context.Context, bearerToken string) (controlleridentity.Principal, error)
}

func newControllerAuthenticator(
	identities controllerIdentityService,
	enrollmentTokens map[string]string,
) *controllerAuthenticator {
	tokenDigests := make(map[string][sha256.Size]byte, len(enrollmentTokens))
	for clusterName, token := range enrollmentTokens {
		tokenDigests[clusterName] = sha256.Sum256([]byte(token))
	}

	return &controllerAuthenticator{
		identities:             identities,
		enrollmentTokenDigests: tokenDigests,
	}
}

func (a *controllerAuthenticator) AuthenticateController(
	ctx context.Context,
	bearerToken string,
) (controlleridentity.Principal, error) {
	return a.identities.AuthenticateController(ctx, bearerToken)
}

func (a *controllerAuthenticator) AuthorizeAnyControllerEnrollment(bearerToken string) bool {
	providedDigest := sha256.Sum256([]byte(bearerToken))
	authorized := 0
	for _, expectedDigest := range a.enrollmentTokenDigests {
		authorized |= subtle.ConstantTimeCompare(providedDigest[:], expectedDigest[:])
	}

	return authorized == 1
}

func (a *controllerAuthenticator) AuthorizeControllerEnrollment(clusterName string, bearerToken string) bool {
	expectedDigest, configured := a.enrollmentTokenDigests[clusterName]
	providedDigest := sha256.Sum256([]byte(bearerToken))
	digestsMatch := subtle.ConstantTimeCompare(providedDigest[:], expectedDigest[:]) == 1

	return configured && digestsMatch
}
