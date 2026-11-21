package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/slipramp/slipramp/internal/cluster"
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
		BuildInfo:   cluster.ControllerBuildInfo{Version: "1.2.3"},
	}
	if err := database.EnrollControllerCredential(ctx, credential); err != nil {
		t.Fatalf("EnrollControllerCredential() error = %v", err)
	}

	clusterName, err := database.GetControllerClusterByCredentialSHA256(ctx, credential.SHA256)
	if err != nil {
		t.Fatalf("GetControllerClusterByCredentialSHA256() error = %v", err)
	}
	if clusterName != "colo" {
		t.Errorf("cluster name = %q, want colo", clusterName)
	}
	observedCluster, err := database.GetCluster(ctx, "colo")
	if err != nil {
		t.Fatalf("GetCluster() error = %v", err)
	}
	if observedCluster.ControllerBuildInfo == nil || observedCluster.ControllerBuildInfo.Version != "1.2.3" {
		t.Errorf("Controller build information = %#v, want version 1.2.3", observedCluster.ControllerBuildInfo)
	}

	credential.EnrolledAt = enrolledAt.Add(time.Minute)
	credential.BuildInfo.Version = "1.2.4"
	if err := database.EnrollControllerCredential(ctx, credential); err != nil {
		t.Fatalf("idempotent EnrollControllerCredential() error = %v", err)
	}
	observedCluster, err = database.GetCluster(ctx, "colo")
	if err != nil {
		t.Fatalf("updated GetCluster() error = %v", err)
	}
	if observedCluster.ControllerBuildInfo == nil || observedCluster.ControllerBuildInfo.Version != "1.2.4" {
		t.Errorf("updated Controller build information = %#v, want version 1.2.4", observedCluster.ControllerBuildInfo)
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
		BuildInfo:   cluster.ControllerBuildInfo{Version: "1.2.3"},
	}
	if err := database.EnrollControllerCredential(ctx, other); !errors.Is(
		err,
		controlleridentity.ErrEnrollmentConflict,
	) {
		t.Errorf("duplicate credential error = %v, want conflict", err)
	}

	_, err = database.GetControllerClusterByCredentialSHA256(context.Background(), strings.Repeat("c", 64))
	if !errors.Is(err, controlleridentity.ErrCredentialNotFound) {
		t.Errorf("unknown credential error = %v, want not found", err)
	}
}
