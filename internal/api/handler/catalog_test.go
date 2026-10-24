package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
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
	desiredCommitSHA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	catalogs := &recordingCatalogService{
		targetStates: map[string]catalog.TargetState{
			"network": {DesiredCommitSHA: desiredCommitSHA},
		},
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

	newAdminTestHandler(&recordingClusterService{}, nil, nil, catalogs, nil, nil, nil).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}

	var body api.CatalogRevision
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if catalogs.repositoryID != repositoryID {
		t.Errorf("repository ID = %s, want %s", catalogs.repositoryID, repositoryID)
	}
	if body.ID != revisionID || body.Commit.Sha != commitSHA ||
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
		body.Targets[0].Status.DesiredCommit.Sha != desiredCommitSHA ||
		len(body.Targets[0].Spec.Actions["preview"].Command) != 2 ||
		body.Targets[0].Spec.Actions["preview"].Artifacts == nil {
		t.Errorf("catalog targets = %#v", body.Targets)
	}
}

func TestListTargetsReturnsPaginatedSummaries(t *testing.T) {
	t.Parallel()

	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	revisionID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1201")
	commitSHA := "0123456789abcdef0123456789abcdef01234567"
	catalogs := &recordingCatalogService{
		targets: catalog.TargetList{
			Total: 3,
			Items: []catalog.TargetSummary{{
				RepositoryID:      repositoryID,
				RepositoryName:    "infrastructure",
				CatalogRevisionID: revisionID,
				CatalogCommitSHA:  commitSHA,
				DesiredCommitSHA:  commitSHA,
				Target: catalog.Target{
					Metadata: catalog.Metadata{
						Name:  "network",
						Title: "Home network",
					},
					Spec: catalog.TargetSpec{
						Runtime:   "terraform",
						Placement: "infrastructure",
						Actions: map[string]catalog.Action{
							"sync":    {},
							"preview": {},
						},
					},
				},
			}},
		},
	}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"/api/v1/targets?repositoryId="+repositoryID.String()+
			"&runtime=terraform&placement=infrastructure&q=network&limit=1&offset=2",
		nil,
	)
	response := httptest.NewRecorder()

	newAdminTestHandler(&recordingClusterService{}, nil, nil, catalogs, nil, nil, nil).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}

	var body api.TargetSummaryList
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if catalogs.listCalls != 1 || catalogs.filter.RepositoryID == nil ||
		*catalogs.filter.RepositoryID != repositoryID ||
		catalogs.filter.Query != "network" || catalogs.filter.Runtime != "terraform" ||
		catalogs.filter.Placement != "infrastructure" || catalogs.filter.Limit != 1 ||
		catalogs.filter.Offset != 2 {
		t.Errorf("target filter = %#v", catalogs.filter)
	}
	if body.Total != 3 || body.Limit != 1 || body.Offset != 2 || len(body.Items) != 1 {
		t.Fatalf("target list = %#v", body)
	}
	item := body.Items[0]
	if item.Name != "network" || item.Title == nil || *item.Title != "Home network" ||
		item.RepositoryName != "infrastructure" ||
		!slices.Equal(item.Actions, []string{"preview", "sync"}) {
		t.Errorf("target summary = %#v", item)
	}
}

func TestListTargetsRejectsInvalidFilters(t *testing.T) {
	t.Parallel()

	tests := []string{
		"/api/v1/targets?limit=0",
		"/api/v1/targets?limit=101",
		"/api/v1/targets?offset=-1",
		"/api/v1/targets?q=%20%20",
		"/api/v1/targets?runtime=%20%20",
		"/api/v1/targets?placement=INVALID_NAME",
	}
	for _, path := range tests {
		request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		catalogs := &recordingCatalogService{}

		newAdminTestHandler(&recordingClusterService{}, nil, nil, catalogs, nil, nil, nil).ServeHTTP(response, request)

		assertProblem(t, response, http.StatusBadRequest)
		if catalogs.listCalls != 0 {
			t.Errorf("catalog service calls = %d, want 0", catalogs.listCalls)
		}
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
			request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, test.path, nil)
			response := httptest.NewRecorder()

			newAdminTestHandler(
				&recordingClusterService{},
				nil,
				nil,
				catalogs,
				nil,
				nil,
				nil,
			).ServeHTTP(response, request)

			assertProblem(t, response, test.wantStatus)
			if catalogs.calls != test.wantCalls {
				t.Errorf("catalog service calls = %d, want %d", catalogs.calls, test.wantCalls)
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
	targetStates map[string]catalog.TargetState
	err          error
	listCalls    int
	filter       catalog.TargetListFilter
	targets      catalog.TargetList
}

func (s *recordingCatalogService) GetActive(
	_ context.Context,
	repositoryID uuid.UUID,
) (catalog.ActiveCatalog, error) {
	s.calls++
	s.repositoryID = repositoryID

	return catalog.ActiveCatalog{Revision: s.revision, TargetStates: s.targetStates}, s.err
}

func (s *recordingCatalogService) ListTargets(
	_ context.Context,
	filter catalog.TargetListFilter,
) (catalog.TargetList, error) {
	s.listCalls++
	s.filter = filter

	return s.targets, s.err
}
