package promotion

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/repository"
)

type BranchDriftStatus string

const (
	BranchDriftStatusCurrent BranchDriftStatus = "current"
	BranchDriftStatusDrifted BranchDriftStatus = "drifted"
	BranchDriftStatusUnknown BranchDriftStatus = "unknown"
)

type BranchDriftListFilter struct {
	Integration string
	Project     string
	Application string
	Repository  string
	Status      BranchDriftStatus
	Limit       int
	Offset      int
}

type BranchDriftStage struct {
	Stage                          Stage
	Status                         BranchDriftStatus
	AcknowledgedSourceRevision     string
	AcknowledgedRenderBaseRevision string
	AcknowledgedRenderRevision     string
	AcknowledgedRenderAuthoredAt   *time.Time
	SourceBranchRevision           string
	SourceBranchAuthoredAt         *time.Time
	TargetBranchRevision           string
	SourceDrift                    bool
	TargetDrift                    bool
	NoOpRender                     bool
	Message                        string
}

type BranchDriftRepository struct {
	Repository repository.Repository
	ScannedAt  time.Time
	Error      string
	Stages     []BranchDriftStage
}

type BranchDriftList struct {
	Items  []BranchDriftRepository
	Total  int
	Limit  int
	Offset int
}

type BranchDriftStore interface {
	ListPromotionStages(context.Context) ([]Stage, error)
	ListRepositories(context.Context) ([]repository.Repository, error)
}

type BranchDriftService struct {
	integrations IntegrationSet
	store        BranchDriftStore
	scanner      repository.BranchScanner
	cachePeriod  time.Duration
	fetchTimeout time.Duration
	cache        map[uuid.UUID]branchDriftCacheEntry
	scanLock     chan struct{}
	now          func() time.Time
}

type branchDriftCacheEntry struct {
	fingerprint [sha256.Size]byte
	scannedAt   time.Time
	error       string
	stages      map[uuid.UUID]branchDriftEvidence
}

type branchDriftEvidence struct {
	status               BranchDriftStatus
	acknowledgement      successfulRenderAcknowledgement
	acknowledgedTargetAt *time.Time
	sourceBranchRevision string
	sourceBranchAt       *time.Time
	targetBranchRevision string
	sourceDrift          bool
	targetDrift          bool
	message              string
}

type successfulRenderAcknowledgement struct {
	SourceRevision string `json:"sourceRevision"`
	TargetRevision string `json:"targetRevision"`
	ResultRevision string `json:"resultRevision"`
}

func NewBranchDriftService(
	integrations IntegrationSet,
	store BranchDriftStore,
	scanner repository.BranchScanner,
	cachePeriod time.Duration,
	fetchTimeout time.Duration,
) *BranchDriftService {
	return &BranchDriftService{
		integrations: integrations,
		store:        store,
		scanner:      scanner,
		cachePeriod:  cachePeriod,
		fetchTimeout: fetchTimeout,
		cache:        make(map[uuid.UUID]branchDriftCacheEntry),
		scanLock:     make(chan struct{}, 1),
		now:          time.Now,
	}
}

func (s *BranchDriftService) List(
	ctx context.Context,
	filter BranchDriftListFilter,
) (BranchDriftList, error) {
	if err := validateListWindow(filter.Limit, filter.Offset); err != nil {
		return BranchDriftList{}, err
	}
	if filter.Status != "" && !filter.Status.Valid() {
		return BranchDriftList{}, fmt.Errorf("%w: branch drift status filter is invalid", ErrInvalid)
	}

	stages, err := s.store.ListPromotionStages(ctx)
	if err != nil {
		return BranchDriftList{}, err
	}
	repositories, err := s.store.ListRepositories(ctx)
	if err != nil {
		return BranchDriftList{}, err
	}

	repositoriesByName := make(map[string]repository.Repository, len(repositories))
	for _, record := range repositories {
		repositoriesByName[record.Name] = record
	}
	stagesByRepository := make(map[string][]Stage)
	matchingRepositories := make(map[string]struct{})
	for _, stage := range stages {
		if stage.ArchivedAt != nil {
			continue
		}
		if _, configured := s.integrations.Lookup(stage.Integration); !configured {
			continue
		}
		stagesByRepository[stage.RepositoryName] = append(stagesByRepository[stage.RepositoryName], stage)
		if stageMatchesBranchDriftFilter(stage, filter) {
			matchingRepositories[stage.RepositoryName] = struct{}{}
		}
	}

	names := slices.Sorted(maps.Keys(matchingRepositories))
	total := len(names)
	if filter.Status == "" {
		names = page(names, filter.Offset, filter.Limit)
	}

	select {
	case s.scanLock <- struct{}{}:
		defer func() { <-s.scanLock }()
	case <-ctx.Done():
		return BranchDriftList{}, ctx.Err()
	}

	items := make([]BranchDriftRepository, 0, len(names))
	for _, name := range names {
		groupStages := stagesByRepository[name]
		slices.SortFunc(groupStages, compareBranchDriftStages)
		record, ok := repositoriesByName[name]
		var item BranchDriftRepository
		if !ok {
			item = missingBranchDriftRepository(name, groupStages, filter, s.currentTime())
		} else {
			entry, scanErr := s.cachedScan(ctx, record, groupStages)
			if scanErr != nil {
				return BranchDriftList{}, scanErr
			}
			item = BranchDriftRepository{
				Repository: record,
				ScannedAt:  entry.scannedAt,
				Error:      entry.error,
				Stages:     make([]BranchDriftStage, 0, len(groupStages)),
			}
			for _, stage := range groupStages {
				if !stageMatchesBranchDriftFilter(stage, filter) {
					continue
				}
				item.Stages = append(item.Stages, branchDriftStage(stage, entry.stages[stage.ID]))
			}
		}
		item.Stages = filterBranchDriftStages(item.Stages, filter.Status)
		if len(item.Stages) > 0 {
			items = append(items, item)
		}
	}
	if filter.Status != "" {
		total = len(items)
		items = page(items, filter.Offset, filter.Limit)
	}

	return BranchDriftList{Items: items, Total: total, Limit: filter.Limit, Offset: filter.Offset}, nil
}

func (s BranchDriftStatus) Valid() bool {
	switch s {
	case BranchDriftStatusCurrent, BranchDriftStatusDrifted, BranchDriftStatusUnknown:
		return true
	default:
		return false
	}
}

func (s *BranchDriftService) cachedScan(
	ctx context.Context,
	record repository.Repository,
	stages []Stage,
) (branchDriftCacheEntry, error) {
	fingerprint := branchDriftFingerprint(record, stages)
	now := s.currentTime()
	if cached, ok := s.cache[record.ID]; ok && cached.fingerprint == fingerprint &&
		now.Before(cached.scannedAt.Add(s.cachePeriod)) {
		return cached, nil
	}

	entry := branchDriftCacheEntry{
		fingerprint: fingerprint,
		scannedAt:   now,
		stages:      make(map[uuid.UUID]branchDriftEvidence, len(stages)),
	}
	requests := make([]repository.BranchComparisonRequest, 0, len(stages))
	requestStageIDs := make([]uuid.UUID, 0, len(stages))
	for _, stage := range stages {
		acknowledgement, err := decodeSuccessfulRenderAcknowledgement(stage.SuccessfulRenderAcknowledgement)
		if err != nil {
			entry.stages[stage.ID] = branchDriftEvidence{
				status:  BranchDriftStatusUnknown,
				message: err.Error(),
			}
			continue
		}

		requests = append(requests, repository.BranchComparisonRequest{
			TargetBranch:             stage.TargetBranch,
			AcknowledgedSourceCommit: acknowledgement.SourceRevision,
			AcknowledgedTargetCommit: acknowledgement.ResultRevision,
			SourcePaths:              slices.Clone(stage.SourcePaths),
		})
		requestStageIDs = append(requestStageIDs, stage.ID)
		entry.stages[stage.ID] = branchDriftEvidence{
			status:          BranchDriftStatusUnknown,
			acknowledgement: acknowledgement,
		}
	}

	if len(requests) > 0 {
		fetchContext, cancel := context.WithTimeout(ctx, s.fetchTimeout)
		comparisons, scanErr := s.scanner.ScanBranches(fetchContext, record, requests)
		cancel()
		if scanErr != nil {
			entry.error = scanErr.Error()
			for _, stageID := range requestStageIDs {
				evidence := entry.stages[stageID]
				evidence.message = "Repository branches could not be scanned."
				entry.stages[stageID] = evidence
			}
		} else if len(comparisons) != len(requests) {
			return branchDriftCacheEntry{}, fmt.Errorf(
				"branch drift scanner returned %d comparisons for %d requests",
				len(comparisons),
				len(requests),
			)
		} else {
			for index, comparison := range comparisons {
				stageID := requestStageIDs[index]
				evidence := entry.stages[stageID]
				evidence.sourceBranchRevision = comparison.SourceBranchCommit
				evidence.sourceBranchAt = comparison.SourceBranchAuthoredAt
				evidence.targetBranchRevision = comparison.TargetBranchCommit
				evidence.acknowledgedTargetAt = comparison.AcknowledgedTargetAuthoredAt
				evidence.sourceDrift = comparison.SourceDrift
				evidence.targetDrift = comparison.TargetDrift
				if comparison.Error != "" {
					evidence.message = comparison.Error
				} else if comparison.SourceDrift || comparison.TargetDrift {
					evidence.status = BranchDriftStatusDrifted
				} else {
					evidence.status = BranchDriftStatusCurrent
				}
				entry.stages[stageID] = evidence
			}
		}
	}

	s.cache[record.ID] = entry

	return entry, nil
}

func decodeSuccessfulRenderAcknowledgement(value json.RawMessage) (successfulRenderAcknowledgement, error) {
	if len(value) == 0 {
		return successfulRenderAcknowledgement{}, fmt.Errorf(
			"Stage has no successful-render acknowledgement",
		)
	}

	var acknowledgement successfulRenderAcknowledgement
	if err := json.Unmarshal(value, &acknowledgement); err != nil {
		return successfulRenderAcknowledgement{}, fmt.Errorf("decode successful-render acknowledgement: %w", err)
	}
	for label, revision := range map[string]string{
		"source revision":        acknowledgement.SourceRevision,
		"render base revision":   acknowledgement.TargetRevision,
		"render result revision": acknowledgement.ResultRevision,
	} {
		if err := validateRevision(label, revision); err != nil {
			return successfulRenderAcknowledgement{}, fmt.Errorf(
				"Stage successful-render acknowledgement is invalid: %w",
				err,
			)
		}
	}

	return acknowledgement, nil
}

func branchDriftStage(stage Stage, evidence branchDriftEvidence) BranchDriftStage {
	return BranchDriftStage{
		Stage:                          stage,
		Status:                         evidence.status,
		AcknowledgedSourceRevision:     evidence.acknowledgement.SourceRevision,
		AcknowledgedRenderBaseRevision: evidence.acknowledgement.TargetRevision,
		AcknowledgedRenderRevision:     evidence.acknowledgement.ResultRevision,
		AcknowledgedRenderAuthoredAt:   evidence.acknowledgedTargetAt,
		SourceBranchRevision:           evidence.sourceBranchRevision,
		SourceBranchAuthoredAt:         evidence.sourceBranchAt,
		TargetBranchRevision:           evidence.targetBranchRevision,
		SourceDrift:                    evidence.sourceDrift,
		TargetDrift:                    evidence.targetDrift,
		NoOpRender: evidence.acknowledgement.TargetRevision != "" &&
			evidence.acknowledgement.TargetRevision == evidence.acknowledgement.ResultRevision,
		Message: evidence.message,
	}
}

func branchDriftFingerprint(record repository.Repository, stages []Stage) [sha256.Size]byte {
	type fingerprintStage struct {
		ID                              uuid.UUID
		TargetBranch                    string
		SourcePaths                     []string
		SuccessfulRenderAcknowledgement string
	}
	type fingerprintValue struct {
		CloneURL              string
		Branch                string
		RefreshCredentialName *string
		Stages                []fingerprintStage
	}
	value := fingerprintValue{
		CloneURL:              record.CloneURL,
		Branch:                record.Branch,
		RefreshCredentialName: record.RefreshCredentialName,
		Stages:                make([]fingerprintStage, 0, len(stages)),
	}
	for _, stage := range stages {
		value.Stages = append(value.Stages, fingerprintStage{
			ID:                              stage.ID,
			TargetBranch:                    stage.TargetBranch,
			SourcePaths:                     slices.Clone(stage.SourcePaths),
			SuccessfulRenderAcknowledgement: string(stage.SuccessfulRenderAcknowledgement),
		})
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("encode branch drift cache fingerprint: %v", err))
	}

	return sha256.Sum256(encoded)
}

func missingBranchDriftRepository(
	name string,
	stages []Stage,
	filter BranchDriftListFilter,
	scannedAt time.Time,
) BranchDriftRepository {
	item := BranchDriftRepository{
		Repository: repository.Repository{Name: name},
		ScannedAt:  scannedAt,
		Error:      "The matched Slipramp Repository is unavailable.",
	}
	for _, stage := range stages {
		if !stageMatchesBranchDriftFilter(stage, filter) {
			continue
		}
		item.Stages = append(item.Stages, BranchDriftStage{
			Stage:   stage,
			Status:  BranchDriftStatusUnknown,
			Message: "Repository branches could not be scanned.",
		})
	}

	return item
}

func stageMatchesBranchDriftFilter(stage Stage, filter BranchDriftListFilter) bool {
	return (filter.Integration == "" || stage.Integration == filter.Integration) &&
		(filter.Project == "" || stage.Project == filter.Project) &&
		(filter.Application == "" || stage.Application == filter.Application) &&
		(filter.Repository == "" || stage.RepositoryName == filter.Repository)
}

func filterBranchDriftStages(stages []BranchDriftStage, status BranchDriftStatus) []BranchDriftStage {
	if status == "" {
		return stages
	}

	filtered := make([]BranchDriftStage, 0, len(stages))
	for _, stage := range stages {
		if stage.Status == status {
			filtered = append(filtered, stage)
		}
	}

	return filtered
}

func compareBranchDriftStages(left, right Stage) int {
	for _, values := range [][2]string{
		{left.Integration, right.Integration},
		{left.Project, right.Project},
		{left.Application, right.Application},
		{left.Environment, right.Environment},
		{left.Name, right.Name},
		{left.ID.String(), right.ID.String()},
	} {
		if value := compareText(values[0], values[1]); value != 0 {
			return value
		}
	}

	return 0
}

func (s *BranchDriftService) currentTime() time.Time {
	return s.now().UTC()
}
