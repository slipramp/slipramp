package controlleridentity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"
)

type identityStore struct {
	credential Credential
	cluster    string
	err        error
}

func (s *identityStore) EnrollControllerCredential(
	_ context.Context,
	credential Credential,
) error {
	s.credential = credential
	return s.err
}

func (s *identityStore) GetControllerClusterByCredentialSHA256(
	_ context.Context,
	credentialSHA256 string,
) (string, error) {
	if s.err != nil {
		return "", s.err
	}

	digest := sha256.Sum256([]byte("controller-token"))
	if credentialSHA256 != hex.EncodeToString(digest[:]) {
		return "", ErrCredentialNotFound
	}

	return s.cluster, nil
}

func TestServiceEnrollsController(t *testing.T) {
	t.Parallel()

	store := &identityStore{}
	service := NewService(store)
	now := time.Date(2026, time.October, 17, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	digest := sha256.Sum256([]byte("controller-token"))

	err := service.EnrollController(
		context.Background(),
		"colo",
		hex.EncodeToString(digest[:]),
	)
	if err != nil {
		t.Fatalf("EnrollController() error = %v", err)
	}
	if store.credential.ClusterName != "colo" ||
		store.credential.SHA256 != hex.EncodeToString(digest[:]) ||
		!store.credential.EnrolledAt.Equal(now) {
		t.Errorf("enrolled credential = %#v", store.credential)
	}
}

func TestServiceAuthenticatesController(t *testing.T) {
	t.Parallel()

	service := NewService(&identityStore{cluster: "colo"})
	principal, err := service.AuthenticateController(
		context.Background(),
		"controller-token",
	)
	if err != nil {
		t.Fatalf("AuthenticateController() error = %v", err)
	}
	if principal.ClusterName != "colo" {
		t.Errorf("principal cluster = %q, want colo", principal.ClusterName)
	}

	_, err = service.AuthenticateController(context.Background(), "other-token")
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("unknown credential error = %v, want unauthorized", err)
	}
}
