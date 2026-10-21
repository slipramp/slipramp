package store

import (
	"fmt"

	"github.com/slipramp/slipramp/internal/productidentity"
)

func credentialDigest(value []byte) (productidentity.CredentialDigest, error) {
	var digest productidentity.CredentialDigest
	if len(value) != len(digest) {
		return digest, fmt.Errorf("digest has %d bytes, want %d", len(value), len(digest))
	}

	copy(digest[:], value)

	return digest, nil
}
