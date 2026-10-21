// Package controlleridentity owns enrolled controller credentials and the
// cluster identity established by them.
package controlleridentity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/slipramp/slipramp/internal/auth"
	"github.com/slipramp/slipramp/internal/cluster"
)

var (
	ErrInvalid            = errors.New("invalid controller identity")
	ErrCredentialNotFound = errors.New("controller credential not found")
	ErrUnauthorized       = errors.New("controller credential is not authorized")
	ErrEnrollmentConflict = errors.New("cluster is enrolled with another credential")

	sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type Credential struct {
	ClusterName string
	SHA256      string
	EnrolledAt  time.Time
}

func (c Credential) Validate() error {
	if err := cluster.ValidateName(c.ClusterName); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}

	if !sha256Pattern.MatchString(c.SHA256) {
		return fmt.Errorf("%w: credential SHA-256 digest must be 64 lowercase hexadecimal characters", ErrInvalid)
	}

	if c.EnrolledAt.IsZero() {
		return fmt.Errorf("%w: enrollment time is required", ErrInvalid)
	}

	return nil
}

type Principal struct {
	ClusterName string
}

type Store interface {
	EnrollControllerCredential(ctx context.Context, credential Credential) error
	GetControllerClusterByCredentialSHA256(ctx context.Context, credentialSHA256 string) (string, error)
}

type Service struct {
	credentials Store
	now         func() time.Time
}

func NewService(credentials Store) *Service {
	return &Service{
		credentials: credentials,
		now:         time.Now,
	}
}

func (s *Service) EnrollController(ctx context.Context, clusterName string, credentialSHA256 string) error {
	credential := Credential{
		ClusterName: clusterName,
		SHA256:      credentialSHA256,
		EnrolledAt:  s.now().UTC(),
	}
	if err := credential.Validate(); err != nil {
		return err
	}

	if err := s.credentials.EnrollControllerCredential(ctx, credential); err != nil {
		return fmt.Errorf("enroll controller credential: %w", err)
	}

	return nil
}

func (s *Service) AuthenticateController(ctx context.Context, bearerToken string) (Principal, error) {
	if err := auth.ValidateBearerToken(bearerToken); err != nil {
		return Principal{}, ErrUnauthorized
	}

	digest := sha256.Sum256([]byte(bearerToken))
	encodedDigest := hex.EncodeToString(digest[:])
	clusterName, err := s.credentials.GetControllerClusterByCredentialSHA256(ctx, encodedDigest)
	if errors.Is(err, ErrCredentialNotFound) {
		return Principal{}, ErrUnauthorized
	}
	if err != nil {
		return Principal{}, fmt.Errorf("authenticate controller credential: %w", err)
	}

	return Principal{ClusterName: clusterName}, nil
}
