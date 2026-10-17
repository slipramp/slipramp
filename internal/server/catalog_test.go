package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/catalog"
)

func TestGetRepositoryCatalogReturnsActiveRevision(t *testing.T) {
	t.Parallel()

	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	revisionID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1201")
	createdAt := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	commitSHA := "0123456789abcdef0123456789abcdef01234567"
	catalogs := &recordingCatalogService{
		revision: catalog.Revision{
			ID:        revisionID,
			CommitSHA: commitSHA,
			CreatedAt: createdAt,
			Catalog: catalog.Catalog{
				Runtimes: []catalog.Runtime{{
					APIVersion: catalog.APIVersion,
					Kind:       catalog.RuntimeKind,
					Metadata: catalog.Metadata{
						Name:  "terraform",
						Title: "Terraform",
					},
					Spec: catalog.RuntimeSpec{
						Image: "hashicorp/terraform:1.14.3",
					},
					Source: catalog.Source{
						Path:     ".slipramp/catalog.yaml",
						Document: 1,
					},
				}},
				Targets: []catalog.Target{{
					APIVersion: catalog.APIVersion,
					Kind:       catalog.TargetKind,
					Metadata: catalog.Metadata{
						Name:  "network",
						Title: "Home network",
					},
					Spec: catalog.TargetSpec{
						Runtime:   "terraform",
						Placement: "infrastructure",
						Actions: map[string]catalog.Action{
							"preview": {
								Intent:  "preview",
								Command: []string{"terraform", "plan"},
								Artifacts: map[string]catalog.Artifact{
									"plan": {
										Path: "plan.tfplan",
									},
								},
							},
						},
					},
					Source: catalog.Source{
						Path:     ".slipramp/catalog.yaml",
						Document: 2,
					},
				}},
			},
		},
	}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"/api/v1/repositories/"+repositoryID.String()+"/catalog",
		nil,
	)
	response := httptest.NewRecorder()

	newHandler(
		&recordingClusterService{},
		nil,
		catalogs,
		nil,
		nil,
	).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}

	var body api.CatalogRevision
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if catalogs.repositoryID != repositoryID {
		t.Errorf(
			"repository ID = %s, want %s",
			catalogs.repositoryID,
			repositoryID,
		)
	}
	if body.ID != revisionID || body.CommitSha != commitSHA ||
		!body.CreatedAt.Equal(createdAt) {
		t.Errorf("catalog revision = %#v", body)
	}
	if len(body.Runtimes) != 1 ||
		body.Runtimes[0].Metadata.Name != "terraform" ||
		body.Runtimes[0].Spec.Image != "hashicorp/terraform:1.14.3" {
		t.Errorf("catalog runtimes = %#v", body.Runtimes)
	}
	if len(body.Targets) != 1 ||
		body.Targets[0].Metadata.Name != "network" ||
		len(body.Targets[0].Spec.Actions["preview"].Command) != 2 ||
		body.Targets[0].Spec.Actions["preview"].Artifacts == nil {
		t.Errorf("catalog targets = %#v", body.Targets)
	}
}

func TestGetRepositoryCatalogMapsExpectedFailures(t *testing.T) {
	t.Parallel()

	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	tests := []struct {
		name          string
		path          string
		err           error
		wantStatus    int
		wantCalls     int
		privateDetail string
	}{
		{
			name:       "invalid repository identity",
			path:       "/api/v1/repositories/not-a-uuid/catalog",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "active catalog not found",
			path:       "/api/v1/repositories/" + repositoryID.String() + "/catalog",
			err:        catalog.ErrNotFound,
			wantStatus: http.StatusNotFound,
			wantCalls:  1,
		},
		{
			name:          "persistence failure",
			path:          "/api/v1/repositories/" + repositoryID.String() + "/catalog",
			err:           errors.New("database unavailable"),
			wantStatus:    http.StatusInternalServerError,
			wantCalls:     1,
			privateDetail: "database unavailable",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			catalogs := &recordingCatalogService{err: test.err}
			request := httptest.NewRequestWithContext(
				context.Background(),
				http.MethodGet,
				test.path,
				nil,
			)
			response := httptest.NewRecorder()

			newHandler(
				&recordingClusterService{},
				nil,
				catalogs,
				nil,
				nil,
			).ServeHTTP(response, request)

			assertProblem(t, response, test.wantStatus)
			if catalogs.calls != test.wantCalls {
				t.Errorf(
					"catalog service calls = %d, want %d",
					catalogs.calls,
					test.wantCalls,
				)
			}
			if test.privateDetail != "" &&
				strings.Contains(response.Body.String(), test.privateDetail) {
				t.Errorf("internal error details were exposed in the problem response")
			}
		})
	}
}

type recordingCatalogService struct {
	calls        int
	repositoryID uuid.UUID
	revision     catalog.Revision
	err          error
}

func (s *recordingCatalogService) GetActiveRevision(
	_ context.Context,
	repositoryID uuid.UUID,
) (catalog.Revision, error) {
	s.calls++
	s.repositoryID = repositoryID

	return s.revision, s.err
}
