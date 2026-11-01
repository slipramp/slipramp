package run

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/notification"
	"github.com/slipramp/slipramp/internal/placement"
	"github.com/slipramp/slipramp/internal/repository"
)

const testCommitSHA = "0123456789abcdef0123456789abcdef01234567"

var (
	testRepositoryID = uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	testRevisionID   = uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1201")
	testTargetID     = uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1202")
)

func testService(t *testing.T) (*Service, *recordingRuns) {
	t.Helper()

	placements, err := placement.NewSet(map[string]placement.Config{
		"infrastructure": {
			ClusterName:  "colo",
			JobNamespace: "slipramp-jobs",
			Credentials: map[string]placement.CredentialSource{
				"backend": {
					SecretRef: placement.SecretReference{Name: "terraform-backend"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("create placements: %v", err)
	}

	runs := &recordingRuns{}
	service := NewService(Dependencies{
		Creation: runs,
		History:  runs,
		Reviews:  runs,
		Repositories: &recordingRepositories{
			record: repository.Repository{
				Config: repository.Config{
					Name:     "infrastructure",
					CloneURL: "https://github.com/example/infrastructure.git",
					Branch:   "main",
				},
				ID: testRepositoryID,
			},
		},
		Catalogs:   &recordingCatalogs{revision: testRevision()},
		Placements: placements,
	})

	return service, runs
}

func testRevision() catalog.Revision {
	return catalog.Revision{
		ID:        testRevisionID,
		CommitSHA: testCommitSHA,
		Catalog: catalog.Catalog{
			Runtimes: []catalog.Runtime{{
				APIVersion: catalog.APIVersion,
				Kind:       catalog.RuntimeKind,
				Metadata:   catalog.Metadata{Name: "terraform"},
				Spec: catalog.RuntimeSpec{
					Image: "hashicorp/terraform:1.14.3",
				},
			}},
			Targets: []catalog.Target{{
				APIVersion: catalog.APIVersion,
				Kind:       catalog.TargetKind,
				Metadata:   catalog.Metadata{Name: "network"},
				Spec: catalog.TargetSpec{
					Runtime:   "terraform",
					Placement: "infrastructure",
					Timeout:   "30m",
					Actions: map[string]catalog.Action{
						"preview": {
							Command: []string{"terraform", "plan"},
							Parameters: map[string]catalog.Parameter{
								"mode": {
									Type:     catalog.ParameterTypeEnum,
									Required: true,
									Values:   []string{"safe", "fast"},
									Argument: &catalog.ParameterArgument{Name: "--mode"},
								},
								"refresh": {
									Type:     catalog.ParameterTypeBoolean,
									Argument: &catalog.ParameterArgument{Name: "--refresh"},
								},
								"retries": {
									Type:     catalog.ParameterTypeNumber,
									Argument: &catalog.ParameterArgument{Name: "--retries"},
								},
								"message": {
									Type:     catalog.ParameterTypeString,
									Argument: &catalog.ParameterArgument{Name: "--message"},
								},
							},
						},
					},
				},
			}},
		},
	}
}

type recordingRuns struct {
	calls                  int
	created                Run
	err                    error
	existing               Run
	getError               error
	idempotencyPrincipalID *uuid.UUID
	artifacts              map[uuid.UUID]ArtifactInput
	listFilter             RunListFilter
	listed                 []Run
	listError              error
	loaded                 Run
	historyError           error
	reviewed               Run
	reviewError            error
	reviewTransition       ReviewTransition
	notificationPlan       *notification.EnqueuePlan
}

func (s *recordingRuns) DecideRunReview(_ context.Context, transition ReviewTransition) (Run, error) {
	s.reviewTransition = transition

	return s.reviewed, s.reviewError
}

func (s *recordingRuns) GetRun(context.Context, uuid.UUID) (Run, error) {
	if s.historyError != nil {
		return Run{}, s.historyError
	}
	if s.loaded.ID == uuid.Nil {
		return Run{}, ErrRunNotFound
	}

	return s.loaded, nil
}

func (s *recordingRuns) ListRuns(_ context.Context, filter RunListFilter) (RunList, error) {
	s.listFilter = filter

	return RunList{Items: s.listed, Total: int64(len(s.listed))}, s.listError
}

func (s *recordingRuns) GetArtifactInputCandidate(_ context.Context, artifactID uuid.UUID) (ArtifactInput, error) {
	input, found := s.artifacts[artifactID]
	if !found {
		return ArtifactInput{}, ErrArtifactNotFound
	}

	return input, nil
}

func (s *recordingRuns) CreateRun(
	_ context.Context,
	record Run,
	notificationPlan *notification.EnqueuePlan,
) (Run, error) {
	s.calls++
	s.created = record
	s.notificationPlan = notificationPlan

	return record, s.err
}

func (s *recordingRuns) GetRunByIdempotencyKey(
	_ context.Context,
	principalID *uuid.UUID,
	_ SourceType,
	_ string,
) (Run, error) {
	if principalID != nil {
		cloned := *principalID
		s.idempotencyPrincipalID = &cloned
	}
	if s.getError == nil && s.existing.ID != uuid.Nil {
		return s.existing, nil
	}
	if s.getError != nil {
		return Run{}, s.getError
	}

	return Run{}, ErrRunNotFound
}

type recordingRepositories struct {
	record repository.Repository
	err    error
}

func (s *recordingRepositories) GetRepository(context.Context, uuid.UUID) (repository.Repository, error) {
	return s.record, s.err
}

type recordingCatalogs struct {
	revision         catalog.Revision
	desiredCommitSHA string
	err              error
	targetErr        error
}

type pinnedCatalogs struct {
	active         catalog.Revision
	pinned         catalog.Revision
	targetRevision *catalog.Revision
	activeCalls    int
	pinnedCalls    int
}

func (s *pinnedCatalogs) GetActiveRevision(context.Context, uuid.UUID) (catalog.Revision, error) {
	s.activeCalls++

	return s.active, nil
}

func (s *pinnedCatalogs) GetTarget(
	context.Context,
	uuid.UUID,
) (catalog.TargetSummary, error) {
	revision := s.active
	if s.targetRevision != nil {
		revision = *s.targetRevision
	}
	return catalog.TargetSummary{
		ID:                testTargetID,
		RepositoryID:      testRepositoryID,
		CatalogRevisionID: revision.ID,
		DesiredCommitSHA:  revision.CommitSHA,
		Target:            revision.Catalog.Targets[0],
	}, nil
}

func (s *pinnedCatalogs) GetRevision(_ context.Context, _ uuid.UUID, revisionID uuid.UUID) (catalog.Revision, error) {
	s.pinnedCalls++
	if revisionID != s.pinned.ID {
		return catalog.Revision{}, catalog.ErrNotFound
	}

	return s.pinned, nil
}

func (s *recordingCatalogs) GetActiveRevision(context.Context, uuid.UUID) (catalog.Revision, error) {
	return s.revision, s.err
}

func (s *recordingCatalogs) GetTarget(
	context.Context,
	uuid.UUID,
) (catalog.TargetSummary, error) {
	if s.targetErr != nil {
		return catalog.TargetSummary{}, s.targetErr
	}
	revision := s.revision
	if len(revision.Catalog.Targets) == 0 {
		revision = testRevision()
	}

	desiredCommitSHA := s.desiredCommitSHA
	if desiredCommitSHA == "" {
		desiredCommitSHA = revision.CommitSHA
	}

	return catalog.TargetSummary{
		ID:                testTargetID,
		RepositoryID:      testRepositoryID,
		CatalogRevisionID: revision.ID,
		DesiredCommitSHA:  desiredCommitSHA,
		Target:            revision.Catalog.Targets[0],
	}, nil
}

func (s *recordingCatalogs) GetRevision(context.Context, uuid.UUID, uuid.UUID) (catalog.Revision, error) {
	return s.revision, s.err
}

func contains(err error, text string) bool {
	return err != nil && strings.Contains(err.Error(), text)
}
