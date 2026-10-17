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
	"github.com/slipramp/slipramp/internal/repository"
)

const maximumIdempotencyKeyLength = 128

type Store interface {
	CreateRun(ctx context.Context, record Run) (Run, error)
	GetRunByIdempotencyKey(
		ctx context.Context,
		sourceType SourceType,
		idempotencyKey string,
	) (Run, error)
	GetPreparingRunForCluster(
		ctx context.Context,
		clusterName string,
	) (Run, error)
	ClaimQueuedRun(
		ctx context.Context,
		clusterName string,
		claimedAt time.Time,
	) (Run, error)
}

type RepositoryService interface {
	GetRepository(
		ctx context.Context,
		repositoryID uuid.UUID,
	) (repository.Repository, error)
}

type CatalogService interface {
	GetActiveRevision(
		ctx context.Context,
		repositoryID uuid.UUID,
	) (catalog.Revision, error)
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

func (s *Service) CreateRun(
	ctx context.Context,
	request CreateRequest,
) (Run, error) {
	if err := validateRequest(request); err != nil {
		return Run{}, err
	}

	parameters, err := normalizeParameters(request.Parameters)
	if err != nil {
		return Run{}, err
	}

	digest, err := requestDigest(request, parameters)
	if err != nil {
		return Run{}, fmt.Errorf("digest run request: %w", err)
	}

	if request.IdempotencyKey != nil {
		existing, err := s.runs.GetRunByIdempotencyKey(
			ctx,
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

	repositoryRecord, err := s.repositories.GetRepository(
		ctx,
		request.RepositoryID,
	)
	if errors.Is(err, repository.ErrNotFound) {
		return Run{}, ErrRepositoryNotFound
	}
	if err != nil {
		return Run{}, fmt.Errorf("get run repository: %w", err)
	}

	revision, err := s.catalogs.GetActiveRevision(ctx, request.RepositoryID)
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

	runtime, found := findRuntime(revision.Catalog.Runtimes, action.Runtime)
	if !found {
		return Run{}, ErrRuntimeNotFound
	}

	placementConfig, found := s.placements.Lookup(action.Placement)
	if !found {
		return Run{}, ErrPlacementNotFound
	}

	parameters, err = validateParameters(action.Parameters, parameters)
	if err != nil {
		return Run{}, err
	}

	createdAt := s.now().UTC()
	record := Run{
		ID:                s.newID(),
		State:             StateQueued,
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
		Snapshot: Snapshot{
			Repository: RepositorySnapshot{
				Name:     repositoryRecord.Name,
				CloneURL: repositoryRecord.CloneURL,
				Branch:   repositoryRecord.Branch,
				CheckoutCredentialName: cloneString(
					repositoryRecord.CheckoutCredentialName,
				),
			},
			Target:    target,
			Action:    action,
			Runtime:   runtime,
			Placement: placementSnapshot(action.Placement, placementConfig),
		},
		CreatedAt: createdAt,
	}

	created, err := s.runs.CreateRun(ctx, record)
	if err != nil {
		return Run{}, fmt.Errorf("create run: %w", err)
	}

	return created, nil
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

	if request.Source.Type != SourceTypeManual &&
		request.Source.Type != SourceTypeAPI {
		return fmt.Errorf("%w: run source is invalid", ErrInvalidRequest)
	}

	if request.IdempotencyKey != nil {
		key := *request.IdempotencyKey
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf(
				"%w: idempotency key must not be blank",
				ErrInvalidRequest,
			)
		}
		if len(key) > maximumIdempotencyKeyLength {
			return fmt.Errorf(
				"%w: idempotency key must not exceed %d characters",
				ErrInvalidRequest,
				maximumIdempotencyKeyLength,
			)
		}
		if strings.IndexFunc(key, unicode.IsControl) >= 0 {
			return fmt.Errorf(
				"%w: idempotency key must not contain control characters",
				ErrInvalidRequest,
			)
		}
	}

	return nil
}

func validateParameters(
	declarations map[string]catalog.Parameter,
	values map[string]any,
) (map[string]any, error) {
	validated := make(map[string]any, len(values))

	for _, name := range slices.Sorted(maps.Keys(values)) {
		declaration, found := declarations[name]
		if !found {
			return nil, fmt.Errorf(
				"%w: parameter %q is not declared by the action",
				ErrInvalidParameters,
				name,
			)
		}

		value, err := validateParameterValue(name, declaration, values[name])
		if err != nil {
			return nil, err
		}

		validated[name] = value
	}

	for _, name := range slices.Sorted(maps.Keys(declarations)) {
		if declarations[name].Required {
			if _, supplied := validated[name]; !supplied {
				return nil, fmt.Errorf(
					"%w: required parameter %q was not supplied",
					ErrInvalidParameters,
					name,
				)
			}
		}
	}

	return validated, nil
}

func normalizeParameters(values map[string]any) (map[string]any, error) {
	normalized := make(map[string]any, len(values))
	for _, name := range slices.Sorted(maps.Keys(values)) {
		value := values[name]
		switch typed := value.(type) {
		case string:
			normalized[name] = typed
		case bool:
			normalized[name] = typed
		default:
			number, ok := numberValue(value)
			if !ok {
				return nil, fmt.Errorf(
					"%w: parameter %q must be a string, boolean, or number",
					ErrInvalidParameters,
					name,
				)
			}

			normalized[name] = number
		}
	}

	return normalized, nil
}

func validateParameterValue(
	name string,
	declaration catalog.Parameter,
	value any,
) (any, error) {
	switch declaration.Type {
	case catalog.ParameterTypeString:
		if typed, ok := value.(string); ok {
			return typed, nil
		}
	case catalog.ParameterTypeBoolean:
		if typed, ok := value.(bool); ok {
			return typed, nil
		}
	case catalog.ParameterTypeNumber:
		if typed, ok := numberValue(value); ok {
			return typed, nil
		}
	case catalog.ParameterTypeEnum:
		typed, ok := value.(string)
		if ok && slices.Contains(declaration.Values, typed) {
			return typed, nil
		}
		if ok {
			return nil, fmt.Errorf(
				"%w: parameter %q must be one of %s",
				ErrInvalidParameters,
				name,
				strings.Join(declaration.Values, ", "),
			)
		}
	}

	return nil, fmt.Errorf(
		"%w: parameter %q must be a %s",
		ErrInvalidParameters,
		name,
		declaration.Type,
	)
}

func numberValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint:
		return float64(typed), true
	case uint32:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	default:
		return 0, false
	}
}

func requestDigest(request CreateRequest, parameters map[string]any) (string, error) {
	payload := struct {
		RepositoryID uuid.UUID      `json:"repositoryId"`
		Target       string         `json:"target"`
		Action       string         `json:"action"`
		Parameters   map[string]any `json:"parameters"`
		Source       Source         `json:"source"`
	}{
		RepositoryID: request.RepositoryID,
		Target:       request.TargetName,
		Action:       request.ActionName,
		Parameters:   parameters,
		Source:       request.Source,
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
