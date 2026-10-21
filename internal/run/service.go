package run

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/placement"
	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/repository"
)

const maximumIdempotencyKeyLength = 128

type Store interface {
	CreateRun(ctx context.Context, record Run) (Run, error)
	GetRun(context.Context, uuid.UUID) (Run, error)
	ListRuns(context.Context, ListFilter) ([]Run, error)
	CancelRun(
		ctx context.Context,
		runID uuid.UUID,
		requestedAt time.Time,
		canceledBy *productidentity.ActorSnapshot,
	) (Run, error)
	GetRunByIdempotencyKey(
		ctx context.Context,
		principalID *uuid.UUID,
		sourceType SourceType,
		idempotencyKey string,
	) (Run, error)
	GetAssignedRunForCluster(ctx context.Context, clusterName string) (Run, error)
	ClaimQueuedRun(ctx context.Context, clusterName string, claimedAt time.Time) (Run, error)
	GetRunToCancelForCluster(ctx context.Context, clusterName string) (Run, error)
	GetArtifactInputCandidate(context.Context, uuid.UUID) (ArtifactInput, error)
	ApplyRunObservation(ctx context.Context, observation Observation, allowedCurrentPhases []Phase) error
}

type RepositoryService interface {
	GetRepository(ctx context.Context, repositoryID uuid.UUID) (repository.Repository, error)
}

type CatalogService interface {
	GetActiveRevision(ctx context.Context, repositoryID uuid.UUID) (catalog.Revision, error)
	GetRevision(context.Context, uuid.UUID, uuid.UUID) (catalog.Revision, error)
}

type Service struct {
	runs         Store
	repositories RepositoryService
	catalogs     CatalogService
	placements   placement.Set
	newID        func() uuid.UUID
	now          func() time.Time
}

func NewService(
	runs Store,
	repositories RepositoryService,
	catalogs CatalogService,
	placements placement.Set,
) *Service {
	return &Service{
		runs:         runs,
		repositories: repositories,
		catalogs:     catalogs,
		placements:   placements,
		newID:        uuid.New,
		now:          time.Now,
	}
}

func (s *Service) CreateRun(ctx context.Context, request CreateRequest) (Run, error) {
	if request.Inputs == nil {
		request.Inputs = map[string]uuid.UUID{}
	}
	if err := validateRequest(request); err != nil {
		return Run{}, err
	}

	parameters, err := catalog.NormalizeParameterValues(request.Parameters)
	if err != nil {
		return Run{}, invalidParameters(err)
	}

	digest, err := requestDigest(request, parameters)
	if err != nil {
		return Run{}, fmt.Errorf("digest run request: %w", err)
	}

	if request.IdempotencyKey != nil {
		existing, err := s.runs.GetRunByIdempotencyKey(
			ctx,
			actorPrincipalID(request.Actor),
			request.Source.Type,
			*request.IdempotencyKey,
		)
		if err == nil {
			if existing.RequestDigest != digest {
				return Run{}, ErrIdempotencyConflict
			}

			return existing, nil
		}
		if !errors.Is(err, ErrRunNotFound) {
			return Run{}, fmt.Errorf("get idempotent run: %w", err)
		}
	}

	repositoryRecord, err := s.repositories.GetRepository(ctx, request.RepositoryID)
	if errors.Is(err, repository.ErrNotFound) {
		return Run{}, ErrRepositoryNotFound
	}
	if err != nil {
		return Run{}, fmt.Errorf("get run repository: %w", err)
	}

	inputs, pinnedRevisionID, err := s.resolveInputCandidates(ctx, request)
	if err != nil {
		return Run{}, err
	}

	if request.CatalogRevisionID != nil {
		if pinnedRevisionID != uuid.Nil && pinnedRevisionID != *request.CatalogRevisionID {
			return Run{}, fmt.Errorf("%w: artifact inputs do not match the pinned catalog revision", ErrInvalidInputs)
		}
		pinnedRevisionID = *request.CatalogRevisionID
	}

	var revision catalog.Revision
	if pinnedRevisionID == uuid.Nil {
		revision, err = s.catalogs.GetActiveRevision(ctx, request.RepositoryID)
	} else {
		revision, err = s.catalogs.GetRevision(ctx, request.RepositoryID, pinnedRevisionID)
	}
	if errors.Is(err, catalog.ErrNotFound) {
		return Run{}, ErrCatalogNotFound
	}
	if err != nil {
		return Run{}, fmt.Errorf("get active run catalog: %w", err)
	}

	target, found := findTarget(revision.Catalog.Targets, request.TargetName)
	if !found {
		return Run{}, ErrTargetNotFound
	}

	action, found := target.EffectiveAction(request.ActionName)
	if !found {
		return Run{}, ErrActionNotFound
	}

	if err := validateArtifactInputs(action, inputs); err != nil {
		return Run{}, err
	}

	runtime, found := findRuntime(revision.Catalog.Runtimes, action.Runtime)
	if !found {
		return Run{}, ErrRuntimeNotFound
	}

	placementConfig, found := s.placements.Lookup(action.Placement)
	if !found {
		return Run{}, ErrPlacementNotFound
	}

	parameters, err = catalog.ValidateParameterValues(action.Parameters, parameters)
	if err != nil {
		return Run{}, invalidParameters(err)
	}

	createdAt := s.now().UTC()
	createdBy, err := actorSnapshot(request.Actor)
	if err != nil {
		return Run{}, err
	}
	record := Run{
		ID:                s.newID(),
		Phase:             PhaseQueued,
		RepositoryID:      request.RepositoryID,
		CatalogRevisionID: revision.ID,
		CommitSHA:         revision.CommitSHA,
		TargetName:        request.TargetName,
		ActionName:        request.ActionName,
		Parameters:        parameters,
		PlacementName:     action.Placement,
		ClusterName:       placementConfig.ClusterName,
		Source:            request.Source,
		IdempotencyKey:    cloneString(request.IdempotencyKey),
		RequestDigest:     digest,
		CreatedBy:         createdBy,
		Snapshot: Snapshot{
			Repository: RepositorySnapshot{
				Name:                   repositoryRecord.Name,
				CloneURL:               repositoryRecord.CloneURL,
				Branch:                 repositoryRecord.Branch,
				CheckoutCredentialName: cloneString(repositoryRecord.CheckoutCredentialName),
			},
			Target:    target,
			Action:    action,
			Runtime:   runtime,
			Placement: placementSnapshot(action.Placement, placementConfig),
			Inputs:    inputs,
		},
		CreatedAt: createdAt,
	}

	created, err := s.runs.CreateRun(ctx, record)
	if err != nil {
		return Run{}, fmt.Errorf("create run: %w", err)
	}

	return created, nil
}

func (s *Service) GetRun(ctx context.Context, runID uuid.UUID) (Run, error) {
	if runID == uuid.Nil {
		return Run{}, fmt.Errorf("%w: Run ID is required", ErrInvalidRequest)
	}

	record, err := s.runs.GetRun(ctx, runID)
	if err != nil {
		return Run{}, err
	}

	return record, nil
}

func (s *Service) ListRuns(ctx context.Context, filter ListFilter) ([]Run, error) {
	if filter.RepositoryID != nil && *filter.RepositoryID == uuid.Nil {
		return nil, fmt.Errorf("%w: repository ID must not be empty", ErrInvalidRequest)
	}
	if filter.TargetName != strings.TrimSpace(filter.TargetName) {
		return nil, fmt.Errorf("%w: target name must not have surrounding whitespace", ErrInvalidRequest)
	}
	if filter.TargetName != "" && filter.RepositoryID == nil {
		return nil, fmt.Errorf("%w: target filter requires a repository ID", ErrInvalidRequest)
	}
	if filter.Limit == 0 {
		filter.Limit = DefaultListLimit
	}
	if filter.Limit < 1 || filter.Limit > MaximumListLimit {
		return nil, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidRequest, MaximumListLimit)
	}

	records, err := s.runs.ListRuns(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("list Runs: %w", err)
	}

	return records, nil
}

func (s *Service) resolveInputCandidates(
	ctx context.Context,
	request CreateRequest,
) (map[string]ArtifactInput, uuid.UUID, error) {
	result := make(map[string]ArtifactInput, len(request.Inputs))
	var revisionID uuid.UUID
	for _, name := range slices.Sorted(maps.Keys(request.Inputs)) {
		if strings.TrimSpace(name) == "" {
			return nil, uuid.Nil, fmt.Errorf("%w: input name must not be blank", ErrInvalidInputs)
		}

		artifactID := request.Inputs[name]
		if artifactID == uuid.Nil {
			return nil, uuid.Nil, fmt.Errorf("%w: input %q artifact ID is required", ErrInvalidInputs, name)
		}

		candidate, err := s.runs.GetArtifactInputCandidate(ctx, artifactID)
		if errors.Is(err, ErrArtifactNotFound) {
			return nil, uuid.Nil, ErrArtifactNotFound
		}
		if err != nil {
			return nil, uuid.Nil, fmt.Errorf("get artifact input %q: %w", name, err)
		}

		if candidate.RepositoryID != request.RepositoryID ||
			candidate.TargetName != request.TargetName ||
			candidate.SHA256 == "" ||
			candidate.RetentionState != "retained" {
			return nil, uuid.Nil, fmt.Errorf(
				"%w: artifact for input %q is not a retained, sealed artifact from this target",
				ErrInvalidInputs,
				name,
			)
		}

		if revisionID == uuid.Nil {
			revisionID = candidate.CatalogRevisionID
		} else if revisionID != candidate.CatalogRevisionID {
			return nil, uuid.Nil, fmt.Errorf(
				"%w: all inputs must come from the same catalog revision",
				ErrInvalidInputs,
			)
		}

		result[name] = candidate
	}

	return result, revisionID, nil
}

func validateArtifactInputs(action catalog.EffectiveAction, inputs map[string]ArtifactInput) error {
	if len(inputs) != len(action.Inputs) {
		return fmt.Errorf("%w: action requires %d explicit input bindings", ErrInvalidInputs, len(action.Inputs))
	}

	for _, name := range slices.Sorted(maps.Keys(action.Inputs)) {
		declaration := action.Inputs[name]
		input, found := inputs[name]
		if !found {
			return fmt.Errorf("%w: required input %q was not supplied", ErrInvalidInputs, name)
		}

		if input.Name != name || input.ActionName != declaration.Artifact.FromAction {
			return fmt.Errorf(
				"%w: input %q must select artifact %q from action %q",
				ErrInvalidInputs,
				name,
				name,
				declaration.Artifact.FromAction,
			)
		}
	}

	for name := range inputs {
		if _, declared := action.Inputs[name]; !declared {
			return fmt.Errorf("%w: input %q is not declared by the action", ErrInvalidInputs, name)
		}
	}

	return nil
}

func validateRequest(request CreateRequest) error {
	if request.RepositoryID == uuid.Nil {
		return fmt.Errorf("%w: repository ID is required", ErrInvalidRequest)
	}

	if strings.TrimSpace(request.TargetName) == "" {
		return fmt.Errorf("%w: target is required", ErrInvalidRequest)
	}

	if strings.TrimSpace(request.ActionName) == "" {
		return fmt.Errorf("%w: action is required", ErrInvalidRequest)
	}

	if err := validateSource(request.Source); err != nil {
		return fmt.Errorf("%w: run source is invalid", ErrInvalidRequest)
	}
	if request.CatalogRevisionID != nil && *request.CatalogRevisionID == uuid.Nil {
		return fmt.Errorf("%w: catalog revision ID must not be empty", ErrInvalidRequest)
	}
	if err := validateSourceRequest(request); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}

	if _, err := actorSnapshot(request.Actor); err != nil {
		return err
	}

	if request.IdempotencyKey != nil {
		key := *request.IdempotencyKey
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("%w: idempotency key must not be blank", ErrInvalidRequest)
		}
		if len(key) > maximumIdempotencyKeyLength {
			return fmt.Errorf(
				"%w: idempotency key must not exceed %d characters",
				ErrInvalidRequest,
				maximumIdempotencyKeyLength,
			)
		}
		if strings.IndexFunc(key, unicode.IsControl) >= 0 {
			return fmt.Errorf("%w: idempotency key must not contain control characters", ErrInvalidRequest)
		}
	}

	return nil
}

func validateSource(source Source) error {
	switch source.Type {
	case SourceTypeManual, SourceTypeAPI:
		if source.Git != nil {
			return errors.New("manual and API sources must not contain Git details")
		}
	case SourceTypeGit:
		if source.Git == nil || source.Git.DeliveryID == uuid.Nil || strings.TrimSpace(source.Git.TriggerName) == "" {
			return errors.New("git source requires a trigger name and delivery ID")
		}
	default:
		return errors.New("unknown source type")
	}

	return nil
}

func validateSourceRequest(request CreateRequest) error {
	if request.Source.Type != SourceTypeGit {
		if request.CatalogRevisionID != nil {
			return errors.New("only Git-triggered Runs may pin a catalog revision")
		}

		return nil
	}

	if request.Actor != nil {
		return errors.New("git-triggered Runs must not have an actor")
	}
	if request.CatalogRevisionID == nil {
		return errors.New("git-triggered Runs must pin a catalog revision")
	}

	deliveryKey := request.Source.Git.DeliveryID.String()
	if request.IdempotencyKey == nil || *request.IdempotencyKey != deliveryKey {
		return errors.New("git-triggered Runs must use the delivery ID as their idempotency key")
	}

	return nil
}

func actorSnapshot(principal *productidentity.Principal) (*productidentity.ActorSnapshot, error) {
	if principal == nil {
		return nil, nil
	}

	snapshot, err := principal.ActorSnapshot()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}

	return &snapshot, nil
}

func actorPrincipalID(principal *productidentity.Principal) *uuid.UUID {
	if principal == nil {
		return nil
	}

	principalID := principal.ID

	return &principalID
}

func invalidParameters(err error) error {
	return fmt.Errorf("%w: %w", ErrInvalidParameters, err)
}

func requestDigest(request CreateRequest, parameters map[string]any) (string, error) {
	inputs := request.Inputs
	if inputs == nil {
		inputs = map[string]uuid.UUID{}
	}
	payload := struct {
		RepositoryID      uuid.UUID            `json:"repositoryId"`
		CatalogRevisionID *uuid.UUID           `json:"catalogRevisionId,omitempty"`
		Target            string               `json:"target"`
		Action            string               `json:"action"`
		Parameters        map[string]any       `json:"parameters"`
		Inputs            map[string]uuid.UUID `json:"inputs"`
		Source            Source               `json:"source"`
	}{
		RepositoryID:      request.RepositoryID,
		CatalogRevisionID: request.CatalogRevisionID,
		Target:            request.TargetName,
		Action:            request.ActionName,
		Parameters:        parameters,
		Inputs:            inputs,
		Source:            request.Source,
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	digest := sha256.Sum256(encoded)

	return hex.EncodeToString(digest[:]), nil
}

func findTarget(targets []catalog.Target, name string) (catalog.Target, bool) {
	for _, target := range targets {
		if target.Metadata.Name == name {
			return target, true
		}
	}

	return catalog.Target{}, false
}

func findRuntime(runtimes []catalog.Runtime, name string) (catalog.Runtime, bool) {
	for _, runtime := range runtimes {
		if runtime.Metadata.Name == name {
			return runtime, true
		}
	}

	return catalog.Runtime{}, false
}

func placementSnapshot(name string, cfg placement.Config) PlacementSnapshot {
	credentials := make(map[string]CredentialSnapshot, len(cfg.Credentials))
	for credentialName, source := range cfg.Credentials {
		credentials[credentialName] = CredentialSnapshot{
			SecretName: source.SecretRef.Name,
		}
	}

	timeout := ""
	if cfg.Timeout > 0 {
		timeout = cfg.Timeout.String()
	}

	return PlacementSnapshot{
		Name:               name,
		ClusterName:        cfg.ClusterName,
		JobNamespace:       cfg.JobNamespace,
		ServiceAccountName: cfg.ServiceAccountName,
		Timeout:            timeout,
		Resources: catalog.ResourceRequirements{
			Requests: maps.Clone(cfg.Resources.Requests),
			Limits:   maps.Clone(cfg.Resources.Limits),
		},
		NodeSelector: cfg.NodeSelector,
		Credentials:  credentials,
		PodTemplate:  cfg.PodTemplate,
	}
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}

	cloned := *value

	return &cloned
}
