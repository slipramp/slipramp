package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/slipramp/slipramp/internal/controlleridentity"
)

func TestControllerCredentialPersistenceIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	enrolledAt := time.Date(2026, time.October, 17, 12, 0, 0, 0, time.UTC)
	credential := controlleridentity.Credential{
		ClusterName: "colo",
		SHA256:      strings.Repeat("a", 64),
		EnrolledAt:  enrolledAt,
	}
	if err := database.EnrollControllerCredential(ctx, credential); err != nil {
		t.Fatalf("EnrollControllerCredential() error = %v", err)
	}

	clusterName, err := database.GetControllerClusterByCredentialSHA256(
		ctx,
		credential.SHA256,
	)
	if err != nil {
		t.Fatalf("GetControllerClusterByCredentialSHA256() error = %v", err)
	}
	if clusterName != "colo" {
		t.Errorf("cluster name = %q, want colo", clusterName)
	}

	credential.EnrolledAt = enrolledAt.Add(time.Minute)
	if err := database.EnrollControllerCredential(ctx, credential); err != nil {
		t.Fatalf("idempotent EnrollControllerCredential() error = %v", err)
	}

	credential.SHA256 = strings.Repeat("b", 64)
	if err := database.EnrollControllerCredential(ctx, credential); !errors.Is(
		err,
		controlleridentity.ErrEnrollmentConflict,
	) {
		t.Errorf("replacement enrollment error = %v, want conflict", err)
	}

	other := controlleridentity.Credential{
		ClusterName: "edge",
		SHA256:      strings.Repeat("a", 64),
		EnrolledAt:  enrolledAt,
	}
	if err := database.EnrollControllerCredential(ctx, other); !errors.Is(
		err,
		controlleridentity.ErrEnrollmentConflict,
	) {
		t.Errorf("duplicate credential error = %v, want conflict", err)
	}

	_, err = database.GetControllerClusterByCredentialSHA256(
		context.Background(),
		strings.Repeat("c", 64),
	)
	if !errors.Is(err, controlleridentity.ErrCredentialNotFound) {
		t.Errorf("unknown credential error = %v, want not found", err)
	}
}
