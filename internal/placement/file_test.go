package placement

import (
	"strings"
	"testing"
)

func TestDecodeActionVolumes(t *testing.T) {
	t.Parallel()

	configured, err := Decode(strings.NewReader(`
apiVersion: slipramp.com/v1alpha1
kind: PlacementConfig
placements:
  infrastructure:
    cluster: colo
    jobNamespace: slipramp-runs
    actionVolumes:
      - name: vault-secrets
        csi:
          driver: secrets-store.csi.k8s.io
          readOnly: true
          volumeAttributes:
            secretProviderClass: infrastructure
    actionVolumeMounts:
      - name: vault-secrets
        mountPath: /run/operator/vault
        readOnly: true
`))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	placement, found := configured.Lookup("infrastructure")
	if !found || len(placement.ActionVolumes) != 1 || len(placement.ActionVolumeMounts) != 1 {
		t.Fatalf("Placement action volumes = %#v, mounts = %#v", placement.ActionVolumes, placement.ActionVolumeMounts)
	}
	volume := placement.ActionVolumes[0]
	if volume.CSI == nil || volume.CSI.Driver != "secrets-store.csi.k8s.io" ||
		volume.CSI.VolumeAttributes["secretProviderClass"] != "infrastructure" {
		t.Errorf("Action volume = %#v", volume)
	}
	if mount := placement.ActionVolumeMounts[0]; mount.MountPath != "/run/operator/vault" || !mount.ReadOnly {
		t.Errorf("Action volume mount = %#v", mount)
	}
}

func TestDecodeRunCapacity(t *testing.T) {
	t.Parallel()

	configured, err := Decode(strings.NewReader(`
apiVersion: slipramp.com/v1alpha1
kind: PlacementConfig
clusters:
  colo:
    maximumActiveRuns: 6
placements:
  infrastructure:
    cluster: colo
    jobNamespace: slipramp-runs
    maximumActiveRuns: 2
`))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	clusterMaximum, found := configured.ClusterMaximumActiveRuns("colo")
	if !found || clusterMaximum != 6 {
		t.Errorf("cluster maximum = (%d, %t), want (6, true)", clusterMaximum, found)
	}
	placement, found := configured.Lookup("infrastructure")
	if !found || placement.MaximumActiveRuns != 2 {
		t.Errorf("Placement maximum = (%d, %t), want (2, true)", placement.MaximumActiveRuns, found)
	}
}

func TestDecodeRejectsInvalidRunCapacity(t *testing.T) {
	t.Parallel()

	_, err := Decode(strings.NewReader(`
apiVersion: slipramp.com/v1alpha1
kind: PlacementConfig
clusters:
  colo:
    maximumActiveRuns: 2
placements:
  infrastructure:
    cluster: colo
    jobNamespace: slipramp-runs
    maximumActiveRuns: 3
`))
	if err == nil {
		t.Fatal("Decode() error = nil, want an error")
	}
}
