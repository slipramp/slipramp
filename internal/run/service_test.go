package run

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/placement"
	"github.com/slipramp/slipramp/internal/repository"
)

func TestCreateRunFreezesResolvedExecution(t *testing.T) {
	t.Parallel()

	service, runs := testService(t)
	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1202")
	createdAt := time.Date(2026, time.October, 16, 12, 5, 0, 0, time.UTC)
	service.newID = func() uuid.UUID { return runID }
	service.now = func() time.Time { return createdAt }
	idempotencyKey := "manual-network-preview"

	created, err := service.CreateRun(
		context.Background(),
		CreateRequest{
			RepositoryID:   testRepositoryID,
			TargetName:     "network",
			ActionName:     "preview",
			Parameters:     map[string]any{"mode": "safe", "refresh": true},
			Source:         Source{Type: SourceTypeManual},
			IdempotencyKey: &idempotencyKey,
		},
	)
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	if created.ID != runID || created.Phase != PhaseQueued ||
		created.RepositoryID != testRepositoryID ||
		created.CatalogRevisionID != testRevisionID ||
		created.CommitSHA != testCommitSHA ||
		created.TargetName != "network" || created.ActionName != "preview" ||
		created.PlacementName != "infrastructure" ||
		created.ClusterName != "colo" ||
		created.Source.Type != SourceTypeManual ||
		!created.CreatedAt.Equal(createdAt) {
		t.Errorf("created Run = %#v", created)
	}
	if created.RequestDigest == "" || runs.created.RequestDigest == "" {
		t.Error("request digest was not recorded")
	}
	if created.Snapshot.Repository.CloneURL !=
		"https://github.com/example/infrastructure.git" ||
		created.Snapshot.Runtime.Spec.Image != "hashicorp/terraform:1.14.3" ||
		created.Snapshot.Action.Command[1] != "plan" ||
		created.Snapshot.Action.Timeout != "30m" ||
		created.Snapshot.Placement.JobNamespace != "slipramp-jobs" ||
		created.Snapshot.Placement.Credentials["backend"].SecretName !=
			"terraform-backend" {
		t.Errorf("frozen snapshot = %#v", created.Snapshot)
	}
}

func TestCreateRunRejectsInvalidParametersBeforePersistence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		parameters map[string]any
		want       string
	}{
		{
			name:       "required missing",
			parameters: map[string]any{"refresh": true},
			want:       "required parameter \"mode\" was not supplied",
		},
		{
			name:       "unknown",
			parameters: map[string]any{"mode": "safe", "other": "value"},
			want:       "parameter \"other\" is not declared",
		},
		{
			name:       "wrong type",
			parameters: map[string]any{"mode": "safe", "refresh": "true"},
			want:       "parameter \"refresh\" must be a boolean",
		},
		{
			name:       "enum outside values",
			parameters: map[string]any{"mode": "dangerous"},
			want:       "parameter \"mode\" must be one of safe, fast",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			service, runs := testService(t)
			_, err := service.CreateRun(
				context.Background(),
				CreateRequest{
					RepositoryID: testRepositoryID,
					TargetName:   "network",
					ActionName:   "preview",
					Parameters:   test.parameters,
					Source:       Source{Type: SourceTypeAPI},
				},
			)
			if !errors.Is(err, ErrInvalidParameters) ||
				!contains(err, test.want) {
				t.Errorf("CreateRun() error = %v, want %q", err, test.want)
			}
			if runs.calls != 0 {
				t.Errorf("persistence calls = %d, want 0", runs.calls)
			}
		})
	}
}

func TestCreateRunMapsMissingResolutionInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		configure func(*Service)
		request   CreateRequest
		want      error
	}{
		{
			name: "repository",
			configure: func(service *Service) {
				service.repositories = &recordingRepositories{err: repository.ErrNotFound}
			},
			want: ErrRepositoryNotFound,
		},
		{
			name: "catalog",
			configure: func(service *Service) {
				service.catalogs = &recordingCatalogs{err: catalog.ErrNotFound}
			},
			want: ErrCatalogNotFound,
		},
		{
			name:    "target",
			request: CreateRequest{TargetName: "missing"},
			want:    ErrTargetNotFound,
		},
		{
			name:    "action",
			request: CreateRequest{ActionName: "missing"},
			want:    ErrActionNotFound,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			service, _ := testService(t)
			if test.configure != nil {
				test.configure(service)
			}
			request := CreateRequest{
				RepositoryID: testRepositoryID,
				TargetName:   "network",
				ActionName:   "preview",
				Parameters:   map[string]any{"mode": "safe"},
				Source:       Source{Type: SourceTypeAPI},
			}
			if test.request.TargetName != "" {
				request.TargetName = test.request.TargetName
			}
			if test.request.ActionName != "" {
				request.ActionName = test.request.ActionName
			}

			_, err := service.CreateRun(context.Background(), request)
			if !errors.Is(err, test.want) {
				t.Errorf("CreateRun() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestCreateRunReturnsIdempotentRunBeforeCatalogResolution(t *testing.T) {
	t.Parallel()

	service, runs := testService(t)
	idempotencyKey := "manual-network-preview"
	request := CreateRequest{
		RepositoryID:   testRepositoryID,
		TargetName:     "network",
		ActionName:     "preview",
		Parameters:     map[string]any{"mode": "safe"},
		Source:         Source{Type: SourceTypeManual},
		IdempotencyKey: &idempotencyKey,
	}
	digest, err := requestDigest(request, request.Parameters)
	if err != nil {
		t.Fatalf("requestDigest() error = %v", err)
	}
	want := Run{
		ID:            uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1209"),
		RequestDigest: digest,
	}
	runs.existing = want
	runs.getError = nil
	service.repositories = &recordingRepositories{err: errors.New("must not be called")}
	service.catalogs = &recordingCatalogs{err: errors.New("must not be called")}

	created, err := service.CreateRun(context.Background(), request)
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if created.ID != want.ID {
		t.Errorf("created ID = %s, want %s", created.ID, want.ID)
	}
	if runs.calls != 0 {
		t.Errorf("creation calls = %d, want 0", runs.calls)
	}
}

func TestCreateRunPinsArtifactInputToProducerRevision(t *testing.T) {
	t.Parallel()

	service, runs := testService(t)
	revision := testRevision()
	target := &revision.Catalog.Targets[0]
	preview := target.Spec.Actions["preview"]
	preview.Artifacts = map[string]catalog.Artifact{
		"plan": {Path: "/artifacts/plan.tfplan"},
	}
	target.Spec.Actions["preview"] = preview
	target.Spec.Actions["apply"] = catalog.Action{
		Command: []string{"terraform", "apply"},
		Inputs: map[string]catalog.Input{
			"plan": {
				Artifact:  catalog.ArtifactReference{FromAction: "preview"},
				MountPath: "/inputs/plan.tfplan",
			},
		},
	}
	service.catalogs = &recordingCatalogs{revision: revision}
	artifactID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1300")
	producerRunID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1301")
	runs.artifacts = map[uuid.UUID]ArtifactInput{
		artifactID: {
			ArtifactID: artifactID, ProducerRunID: producerRunID, Name: "plan",
			MediaType: "application/octet-stream", SizeBytes: 12,
			SHA256: strings.Repeat("a", 64), RetentionState: "retained",
			RepositoryID: testRepositoryID, CatalogRevisionID: testRevisionID,
			CommitSHA: testCommitSHA, TargetName: "network", ActionName: "preview",
		},
	}

	created, err := service.CreateRun(context.Background(), CreateRequest{
		RepositoryID: testRepositoryID,
		TargetName:   "network",
		ActionName:   "apply",
		Inputs:       map[string]uuid.UUID{"plan": artifactID},
		Source:       Source{Type: SourceTypeManual},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	input := created.Snapshot.Inputs["plan"]
	if created.CatalogRevisionID != testRevisionID || created.CommitSHA != testCommitSHA ||
		input.ArtifactID != artifactID || input.ProducerRunID != producerRunID {
		t.Errorf("created input-pinned Run = %#v", created)
	}
	if runs.created.Snapshot.Inputs["plan"].SHA256 != strings.Repeat("a", 64) {
		t.Errorf("persisted input = %#v", runs.created.Snapshot.Inputs["plan"])
	}
}

func TestCreateRunRejectsIncompatibleArtifactInput(t *testing.T) {
	t.Parallel()

	service, runs := testService(t)
	revision := testRevision()
	revision.Catalog.Targets[0].Spec.Actions["apply"] = catalog.Action{
		Command: []string{"terraform", "apply"},
		Inputs: map[string]catalog.Input{
			"plan": {
				Artifact:  catalog.ArtifactReference{FromAction: "preview"},
				MountPath: "/inputs/plan.tfplan",
			},
		},
	}
	service.catalogs = &recordingCatalogs{revision: revision}
	artifactID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1300")
	runs.artifacts = map[uuid.UUID]ArtifactInput{
		artifactID: {
			ArtifactID: artifactID, ProducerRunID: uuid.New(), Name: "plan",
			SHA256: strings.Repeat("a", 64), RetentionState: "retained",
			RepositoryID: testRepositoryID, CatalogRevisionID: testRevisionID,
			CommitSHA: testCommitSHA, TargetName: "network", ActionName: "destroy",
		},
	}

	_, err := service.CreateRun(context.Background(), CreateRequest{
		RepositoryID: testRepositoryID,
		TargetName:   "network",
		ActionName:   "apply",
		Inputs:       map[string]uuid.UUID{"plan": artifactID},
		Source:       Source{Type: SourceTypeAPI},
	})
	if !errors.Is(err, ErrInvalidInputs) {
		t.Fatalf("CreateRun() error = %v, want invalid inputs", err)
	}
	if runs.calls != 0 {
		t.Errorf("persistence calls = %d, want 0", runs.calls)
	}
}

const testCommitSHA = "0123456789abcdef0123456789abcdef01234567"

var (
	testRepositoryID = uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	testRevisionID   = uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1201")
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
	service := NewService(
		runs,
		&recordingRepositories{
			record: repository.Repository{
				Config: repository.Config{
					Name:     "infrastructure",
					CloneURL: "https://github.com/example/infrastructure.git",
					Branch:   "main",
				},
				ID: testRepositoryID,
			},
		},
		&recordingCatalogs{revision: testRevision()},
		placements,
	)

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
							},
						},
					},
				},
			}},
		},
	}
}

type recordingRuns struct {
	calls             int
	created           Run
	err               error
	existing          Run
	getError          error
	assignedCalls     int
	assigned          Run
	assignedError     error
	claimCalls        int
	claimClusterName  string
	claimedAt         time.Time
	claimed           Run
	claimError        error
	observationCalls  int
	observation       Observation
	observationFrom   []Phase
	observationError  error
	cancelCalls       int
	cancelID          uuid.UUID
	cancelAt          time.Time
	canceled          Run
	cancelError       error
	nextCancelCalls   int
	nextCancelCluster string
	nextCancel        Run
	nextCancelError   error
	artifacts         map[uuid.UUID]ArtifactInput
}

func (s *recordingRuns) GetArtifactInputCandidate(
	_ context.Context,
	artifactID uuid.UUID,
) (ArtifactInput, error) {
	input, found := s.artifacts[artifactID]
	if !found {
		return ArtifactInput{}, ErrArtifactNotFound
	}

	return input, nil
}

func (s *recordingRuns) CreateRun(_ context.Context, record Run) (Run, error) {
	s.calls++
	s.created = record

	return record, s.err
}

func (s *recordingRuns) GetRunByIdempotencyKey(
	context.Context,
	SourceType,
	string,
) (Run, error) {
	if s.getError == nil && s.existing.ID != uuid.Nil {
		return s.existing, nil
	}
	if s.getError != nil {
		return Run{}, s.getError
	}

	return Run{}, ErrRunNotFound
}

func (s *recordingRuns) CancelRun(
	_ context.Context,
	runID uuid.UUID,
	requestedAt time.Time,
) (Run, error) {
	s.cancelCalls++
	s.cancelID = runID
	s.cancelAt = requestedAt

	return s.canceled, s.cancelError
}

func (s *recordingRuns) GetAssignedRunForCluster(
	_ context.Context,
	_ string,
) (Run, error) {
	s.assignedCalls++

	return s.assigned, s.assignedError
}

func (s *recordingRuns) ClaimQueuedRun(
	_ context.Context,
	clusterName string,
	claimedAt time.Time,
) (Run, error) {
	s.claimCalls++
	s.claimClusterName = clusterName
	s.claimedAt = claimedAt

	return s.claimed, s.claimError
}

func (s *recordingRuns) GetRunToCancelForCluster(
	_ context.Context,
	clusterName string,
) (Run, error) {
	s.nextCancelCalls++
	s.nextCancelCluster = clusterName

	return s.nextCancel, s.nextCancelError
}

func (s *recordingRuns) ApplyRunObservation(
	_ context.Context,
	observation Observation,
	allowedCurrentPhases []Phase,
) error {
	s.observationCalls++
	s.observation = observation
	s.observationFrom = append([]Phase(nil), allowedCurrentPhases...)

	return s.observationError
}

type recordingRepositories struct {
	record repository.Repository
	err    error
}

func (s *recordingRepositories) GetRepository(
	context.Context,
	uuid.UUID,
) (repository.Repository, error) {
	return s.record, s.err
}

type recordingCatalogs struct {
	revision catalog.Revision
	err      error
}

func (s *recordingCatalogs) GetActiveRevision(
	context.Context,
	uuid.UUID,
) (catalog.Revision, error) {
	return s.revision, s.err
}

func (s *recordingCatalogs) GetRevision(
	context.Context,
	uuid.UUID,
	uuid.UUID,
) (catalog.Revision, error) {
	return s.revision, s.err
}

func contains(err error, text string) bool {
	return err != nil && strings.Contains(err.Error(), text)
}
