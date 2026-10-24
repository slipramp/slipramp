package promotion

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/slipramp/slipramp/internal/repository"
)

const branchDriftScanConcurrency = 4

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
	logger       *slog.Logger
	cacheMutex   sync.RWMutex
	cache        map[uuid.UUID]branchDriftCacheEntry
	refresh      chan struct{}
	now          func() time.Time
}

type branchDriftCacheEntry struct {
	fingerprint [sha256.Size]byte
	scannedAt   time.Time
	error       string
	stages      map[uuid.UUID]branchDriftEvidence
	refreshing  bool
}

type branchDriftEvidence struct {
	status               BranchDriftStatus
	acknowledgement      SuccessfulRenderAcknowledgement
	acknowledgedTargetAt *time.Time
	sourceBranchRevision string
	sourceBranchAt       *time.Time
	targetBranchRevision string
	sourceDrift          bool
	targetDrift          bool
	message              string
}

func NewBranchDriftService(
	integrations IntegrationSet,
	store BranchDriftStore,
	scanner repository.BranchScanner,
	cachePeriod time.Duration,
	fetchTimeout time.Duration,
	logger *slog.Logger,
) *BranchDriftService {
	return &BranchDriftService{
		integrations: integrations,
		store:        store,
		scanner:      scanner,
		cachePeriod:  cachePeriod,
		fetchTimeout: fetchTimeout,
		logger:       logger.With("component", "promotion_branch_drift"),
		cache:        make(map[uuid.UUID]branchDriftCacheEntry),
		refresh:      make(chan struct{}, 1),
		now:          time.Now,
	}
}

func (s *BranchDriftService) Run(ctx context.Context) {
	s.refreshAndLog(ctx)

	ticker := time.NewTicker(s.cachePeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.refresh:
		}

		s.refreshAndLog(ctx)
	}
}

func (s *BranchDriftService) Close() error {
	closer, ok := s.scanner.(interface{ Close() error })
	if !ok {
		return nil
	}

	return closer.Close()
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
	matchingStageCounts := make(map[string]int)
	for _, stage := range stages {
		if stage.ArchivedAt != nil {
			continue
		}
		if _, configured := s.integrations.Lookup(stage.Integration); !configured {
			continue
		}
		stagesByRepository[stage.RepositoryName] = append(stagesByRepository[stage.RepositoryName], stage)
		if stageMatchesBranchDriftFilter(stage, filter) {
			matchingStageCounts[stage.RepositoryName]++
		}
	}

	names := slices.Sorted(maps.Keys(matchingStageCounts))

	items := make([]BranchDriftRepository, 0, min(len(names), filter.Limit))
	total := 0
	for _, name := range names {
		groupStart := total
		if filter.Status == "" {
			total += matchingStageCounts[name]
			if total <= filter.Offset || groupStart >= filter.Offset+filter.Limit {
				continue
			}
		}

		groupStages := stagesByRepository[name]
		slices.SortFunc(groupStages, compareBranchDriftStages)
		record, ok := repositoriesByName[name]
		var item BranchDriftRepository
		if !ok {
			item = missingBranchDriftRepository(name, groupStages, filter, s.currentTime())
		} else {
			entry := s.readCache(record, groupStages)
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
		if filter.Status != "" {
			total += len(item.Stages)
		}

		start := max(0, filter.Offset-groupStart)
		end := min(len(item.Stages), filter.Offset+filter.Limit-groupStart)
		if start < end {
			item.Stages = item.Stages[start:end]
			items = append(items, item)
		}
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

func (s *BranchDriftService) scanRepository(
	ctx context.Context,
	record repository.Repository,
	stages []Stage,
	fingerprint [sha256.Size]byte,
) (branchDriftCacheEntry, error) {
	entry := branchDriftCacheEntry{
		fingerprint: fingerprint,
		stages:      make(map[uuid.UUID]branchDriftEvidence, len(stages)),
	}
	requests := make([]repository.BranchComparisonRequest, 0, len(stages))
	requestStageIDs := make([]uuid.UUID, 0, len(stages))
	for _, stage := range stages {
		acknowledgement, err := validatedRenderAcknowledgement(stage.SuccessfulRenderAcknowledgement)
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
		if ctx.Err() != nil {
			return branchDriftCacheEntry{}, ctx.Err()
		}
		if scanErr != nil {
			entry.error = scanErr.Error()
			for _, stageID := range requestStageIDs {
				evidence := entry.stages[stageID]
				evidence.message = "Repository branches could not be scanned."
				entry.stages[stageID] = evidence
			}
		} else if len(comparisons) != len(requests) {
			entry.error = fmt.Sprintf(
				"branch drift scanner returned %d comparisons for %d requests",
				len(comparisons),
				len(requests),
			)
			for _, stageID := range requestStageIDs {
				evidence := entry.stages[stageID]
				evidence.message = "Repository branches could not be scanned."
				entry.stages[stageID] = evidence
			}
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
	entry.scannedAt = s.currentTime()
	return entry, nil
}

func (s *BranchDriftService) Refresh(ctx context.Context) (int, error) {
	stages, err := s.store.ListPromotionStages(ctx)
	if err != nil {
		return 0, err
	}
	repositories, err := s.store.ListRepositories(ctx)
	if err != nil {
		return 0, err
	}

	repositoriesByName := make(map[string]repository.Repository, len(repositories))
	for _, record := range repositories {
		repositoriesByName[record.Name] = record
	}
	stagesByRepository := make(map[string][]Stage)
	for _, stage := range stages {
		if stage.ArchivedAt != nil {
			continue
		}
		if _, configured := s.integrations.Lookup(stage.Integration); !configured {
			continue
		}
		stagesByRepository[stage.RepositoryName] = append(stagesByRepository[stage.RepositoryName], stage)
	}

	type scan struct {
		record      repository.Repository
		stages      []Stage
		fingerprint [sha256.Size]byte
	}
	now := s.currentTime()
	active := make(map[uuid.UUID]struct{}, len(stagesByRepository))
	scans := make([]scan, 0, len(stagesByRepository))
	for _, name := range slices.Sorted(maps.Keys(stagesByRepository)) {
		groupStages := stagesByRepository[name]
		record, ok := repositoriesByName[name]
		if !ok {
			continue
		}
		active[record.ID] = struct{}{}
		slices.SortFunc(groupStages, compareBranchDriftStages)
		fingerprint := branchDriftFingerprint(record, groupStages)
		cached, found := s.cacheEntry(record.ID)
		if found && cached.fingerprint == fingerprint {
			if cached.refreshing || now.Before(cached.scannedAt.Add(s.cachePeriod)) {
				continue
			}
			cached.refreshing = true
			s.storeCache(record.ID, cached)
		} else {
			s.storeCache(record.ID, pendingBranchDriftCacheEntry(fingerprint, groupStages, now))
		}
		scans = append(scans, scan{record: record, stages: groupStages, fingerprint: fingerprint})
	}
	s.pruneCache(active)

	group, groupContext := errgroup.WithContext(ctx)
	group.SetLimit(branchDriftScanConcurrency)
	for _, item := range scans {
		group.Go(func() error {
			entry, scanErr := s.scanRepository(groupContext, item.record, item.stages, item.fingerprint)
			if scanErr != nil {
				s.finishRefresh(item.record.ID, item.fingerprint)

				return scanErr
			}
			s.storeRefreshedCache(item.record.ID, entry)

			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return 0, err
	}

	return len(scans), nil
}

func (s *BranchDriftService) storeRefreshedCache(repositoryID uuid.UUID, entry branchDriftCacheEntry) {
	s.cacheMutex.Lock()
	defer s.cacheMutex.Unlock()

	cached, ok := s.cache[repositoryID]
	if !ok || cached.fingerprint != entry.fingerprint {
		return
	}
	s.cache[repositoryID] = entry
}

func (s *BranchDriftService) finishRefresh(repositoryID uuid.UUID, fingerprint [sha256.Size]byte) {
	s.cacheMutex.Lock()
	defer s.cacheMutex.Unlock()

	entry, ok := s.cache[repositoryID]
	if !ok || entry.fingerprint != fingerprint {
		return
	}
	entry.refreshing = false
	s.cache[repositoryID] = entry
}

func (s *BranchDriftService) readCache(
	record repository.Repository,
	stages []Stage,
) branchDriftCacheEntry {
	fingerprint := branchDriftFingerprint(record, stages)
	cached, ok := s.cacheEntry(record.ID)
	if !ok || cached.fingerprint != fingerprint {
		s.requestRefresh()
		scannedAt := s.currentTime()
		if ok {
			scannedAt = cached.scannedAt
		}

		return pendingBranchDriftCacheEntry(fingerprint, stages, scannedAt)
	}
	if !cached.refreshing && !s.currentTime().Before(cached.scannedAt.Add(s.cachePeriod)) {
		s.requestRefresh()
	}

	return cached
}

func (s *BranchDriftService) cacheEntry(repositoryID uuid.UUID) (branchDriftCacheEntry, bool) {
	s.cacheMutex.RLock()
	defer s.cacheMutex.RUnlock()

	entry, ok := s.cache[repositoryID]

	return entry, ok
}

func (s *BranchDriftService) storeCache(repositoryID uuid.UUID, entry branchDriftCacheEntry) {
	s.cacheMutex.Lock()
	defer s.cacheMutex.Unlock()

	s.cache[repositoryID] = entry
}

func (s *BranchDriftService) pruneCache(active map[uuid.UUID]struct{}) {
	s.cacheMutex.Lock()
	defer s.cacheMutex.Unlock()

	for repositoryID := range s.cache {
		if _, ok := active[repositoryID]; !ok {
			delete(s.cache, repositoryID)
		}
	}
}

func (s *BranchDriftService) requestRefresh() {
	select {
	case s.refresh <- struct{}{}:
	default:
	}
}

func (s *BranchDriftService) refreshAndLog(ctx context.Context) {
	count, err := s.Refresh(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.Error("refresh promotion branch drift", "error", err)
		}

		return
	}
	if count > 0 {
		s.logger.Debug("refreshed promotion branch drift", "repositories", count)
	}
}

func pendingBranchDriftCacheEntry(
	fingerprint [sha256.Size]byte,
	stages []Stage,
	scannedAt time.Time,
) branchDriftCacheEntry {
	entry := branchDriftCacheEntry{
		fingerprint: fingerprint,
		scannedAt:   scannedAt,
		stages:      make(map[uuid.UUID]branchDriftEvidence, len(stages)),
		refreshing:  true,
	}
	for _, stage := range stages {
		acknowledgement, err := validatedRenderAcknowledgement(stage.SuccessfulRenderAcknowledgement)
		message := "Branch drift refresh is pending."
		if err != nil {
			message = err.Error()
		}
		entry.stages[stage.ID] = branchDriftEvidence{
			status:          BranchDriftStatusUnknown,
			acknowledgement: acknowledgement,
			message:         message,
		}
	}

	return entry
}

func validatedRenderAcknowledgement(value *SuccessfulRenderAcknowledgement) (SuccessfulRenderAcknowledgement, error) {
	if value == nil {
		return SuccessfulRenderAcknowledgement{}, fmt.Errorf(
			"Stage has no successful-render acknowledgement",
		)
	}

	acknowledgement := *value
	for label, revision := range map[string]string{
		"source revision":        acknowledgement.SourceRevision,
		"render base revision":   acknowledgement.TargetRevision,
		"render result revision": acknowledgement.ResultRevision,
	} {
		if err := validateRevision(label, revision); err != nil {
			return SuccessfulRenderAcknowledgement{}, fmt.Errorf(
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
		SuccessfulRenderAcknowledgement *SuccessfulRenderAcknowledgement
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
			SuccessfulRenderAcknowledgement: stage.SuccessfulRenderAcknowledgement,
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
