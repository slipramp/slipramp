package auth

import (
	"errors"
	"regexp"
)

var bearerTokenPattern = regexp.MustCompile(`^[A-Za-z0-9\-._~+/]+=*$`)

// ValidateBearerToken applies the b64token grammar used by HTTP bearer
// credentials.
func ValidateBearerToken(token string) error {
	if !bearerTokenPattern.MatchString(token) {
		return errors.New("bearer token contains invalid characters")
	}

	return nil
}
