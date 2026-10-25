package catalog

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/placement"
)

func TestServiceExplainsActiveActionWithCurrentPlacementsAndOmittedParameters(t *testing.T) {
	t.Parallel()

	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	placements, err := placement.NewSet(map[string]placement.Config{
		"production": {
			ClusterName:        "colo",
			JobNamespace:       "slipramp-runs",
			ServiceAccountName: "operations",
			Timeout:            20 * time.Minute,
			Resources: placement.ResourceRequirements{
				Requests: map[string]string{"cpu": "250m"},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}
	store := &explanationStore{revision: Revision{
		ID:        uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1201"),
		CommitSHA: "0123456789abcdef0123456789abcdef01234567",
		Catalog: Catalog{
			Runtimes: []Runtime{{
				Metadata: Metadata{Name: "tools"},
				Spec: RuntimeSpec{
					Image: "example.com/tools:1",
				},
				Source: Source{Path: ".slipramp/catalog.yaml", Document: 1},
			}},
			Targets: []Target{{
				Metadata: Metadata{Name: "network"},
				Spec: TargetSpec{
					Runtime:   "tools",
					Placement: "production",
					Actions: map[string]Action{
						"plan": {
							Command: []string{"tool", "plan"},
							Parameters: map[string]Parameter{
								"region": {
									Type:        ParameterTypeString,
									Required:    true,
									Environment: "REGION",
								},
							},
						},
					},
				},
				Source: Source{Path: ".slipramp/catalog.yaml", Document: 2},
			}},
		},
	}}

	explanation, err := NewService(store, placements).
		ExplainActiveAction(t.Context(), repositoryID, "network", "plan")
	if err != nil {
		t.Fatalf("ExplainActiveAction() error = %v", err)
	}
	if store.repositoryID != repositoryID {
		t.Errorf("repository ID = %s, want %s", store.repositoryID, repositoryID)
	}
	if explanation.Level != ValidationLevelDeployment ||
		explanation.Placement.Cluster.Value != "colo" ||
		explanation.Placement.Namespace.Value != "slipramp-runs" ||
		explanation.Timeout.Value != "20m0s" ||
		explanation.Resources.Requests["cpu"].Value != "250m" {
		t.Errorf("placement explanation = %#v", explanation)
	}
	if len(explanation.Parameters) != 1 || explanation.Parameters[0].Supplied ||
		explanation.Parameters[0].Value != nil {
		t.Errorf("omitted required parameter = %#v", explanation.Parameters)
	}
}

type explanationStore struct {
	repositoryID uuid.UUID
	revision     Revision
}

func (s *explanationStore) GetActiveCatalog(context.Context, uuid.UUID) (ActiveCatalog, error) {
	return ActiveCatalog{}, nil
}

func (s *explanationStore) GetActiveCatalogRevision(
	_ context.Context,
	repositoryID uuid.UUID,
) (Revision, error) {
	s.repositoryID = repositoryID

	return s.revision, nil
}

func (s *explanationStore) GetCatalogRevision(context.Context, uuid.UUID, uuid.UUID) (Revision, error) {
	return Revision{}, nil
}

func (s *explanationStore) ListActiveTargets(context.Context, TargetListFilter) (TargetList, error) {
	return TargetList{}, nil
}
