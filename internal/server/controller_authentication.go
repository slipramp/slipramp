package server

import (
	"crypto/sha256"
	"crypto/subtle"
)

type controllerAuthenticator struct {
	tokenDigests map[string][sha256.Size]byte
}

func newControllerAuthenticator(
	tokens map[string]string,
) *controllerAuthenticator {
	tokenDigests := make(map[string][sha256.Size]byte, len(tokens))
	for clusterName, token := range tokens {
		tokenDigests[clusterName] = sha256.Sum256([]byte(token))
	}

	return &controllerAuthenticator{tokenDigests: tokenDigests}
}

func (a *controllerAuthenticator) AuthenticateController(
	clusterName string,
	bearerToken string,
) bool {
	expectedDigest, configured := a.tokenDigests[clusterName]
	providedDigest := sha256.Sum256([]byte(bearerToken))
	digestsMatch := subtle.ConstantTimeCompare(
		providedDigest[:],
		expectedDigest[:],
	) == 1

	return configured && digestsMatch
}
