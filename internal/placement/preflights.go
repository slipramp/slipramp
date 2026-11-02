package placement

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"

	"github.com/slipramp/slipramp/internal/cluster"
	"github.com/slipramp/slipramp/internal/productidentity"
)

const (
	DefaultPreflightListLimit   = 10
	MaximumPreflightListLimit   = 100
	DefaultPreflightTimeout     = 2 * time.Minute
	PreflightDispatchRetryDelay = 30 * time.Second

	PreflightStateQueued    PreflightState = "queued"
	PreflightStateRunning   PreflightState = "running"
	PreflightStateSucceeded PreflightState = "succeeded"
	PreflightStateFailed    PreflightState = "failed"

	PreflightCleanupPending     PreflightCleanupState = "pending"
	PreflightCleanupNotRequired PreflightCleanupState = "not-required"
	PreflightCleanupSucceeded   PreflightCleanupState = "succeeded"
	PreflightCleanupFailed      PreflightCleanupState = "failed"
)

var (
	ErrPreflightInvalid               = errors.New("invalid Placement preflight")
	ErrPreflightNotFound              = errors.New("placement preflight not found")
	ErrPreflightPlacementNotFound     = errors.New("placement not found")
	ErrPreflightControllerUnreachable = errors.New("placement Controller is unreachable")
	ErrPreflightActive                = errors.New("placement already has an active preflight")
	ErrPreflightConflict              = errors.New("placement preflight report conflicts with durable state")
	ErrNoPreflightCommand             = errors.New("no Placement preflight command available")
)

type PreflightState string
type PreflightCleanupState string

type PreflightDiagnostic struct {
	Reason          string
	Message         string
	Count           int32
	FirstObservedAt *time.Time
	LastObservedAt  *time.Time
}

type PreflightSnapshot struct {
	PlacementName         string                  `json:"placementName"`
	ConfigurationChecksum string                  `json:"configurationChecksum"`
	ClusterName           string                  `json:"cluster"`
	JobNamespace          string                  `json:"jobNamespace"`
	ServiceAccountName    string                  `json:"serviceAccountName,omitempty"`
	Resources             ResourceRequirements    `json:"resources"`
	NodeSelector          map[string]string       `json:"nodeSelector,omitempty"`
	PodTemplate           *corev1.PodTemplateSpec `json:"podTemplate,omitempty"`
}

type Preflight struct {
	ID               uuid.UUID
	Snapshot         PreflightSnapshot
	State            PreflightState
	CleanupState     PreflightCleanupState
	RequestedBy      productidentity.ActorSnapshot
	CreatedAt        time.Time
	StartedAt        *time.Time
	FinishedAt       *time.Time
	LastDispatchedAt *time.Time
	JobUID           *string
	Reason           *string
	Message          *string
	Diagnostics      []PreflightDiagnostic
}

type PreflightList struct {
	Items []Preflight
	Total int64
}

type PreflightListFilter struct {
	PlacementName string
	Limit         int32
	Offset        int32
}

type PreflightReport struct {
	ID           uuid.UUID
	ClusterName  string
	State        PreflightState
	CleanupState PreflightCleanupState
	JobUID       *string
	Reason       *string
	Message      *string
	Diagnostics  []PreflightDiagnostic
	ObservedAt   time.Time
}

type PreflightStore interface {
	CreatePlacementPreflight(context.Context, Preflight) (Preflight, error)
	ListPlacementPreflights(context.Context, PreflightListFilter) (PreflightList, error)
	ClaimPlacementPreflight(context.Context, string, time.Time, time.Time) (Preflight, error)
	RecordPlacementPreflightReport(context.Context, PreflightReport) (Preflight, error)
}

type PreflightService struct {
	placements Set
	clusters   ClusterInventory
	store      PreflightStore
	now        func() time.Time
}

func NewPreflightService(placements Set, clusters ClusterInventory, store PreflightStore) *PreflightService {
	return &PreflightService{placements: placements, clusters: clusters, store: store, now: time.Now}
}

func (s *PreflightService) Create(
	ctx context.Context,
	placementName string,
	principal *productidentity.Principal,
) (Preflight, error) {
	cfg, found := s.placements.Lookup(placementName)
	if !found {
		return Preflight{}, ErrPreflightPlacementNotFound
	}
	if principal == nil {
		return Preflight{}, fmt.Errorf("%w: requesting principal is required", ErrPreflightInvalid)
	}
	actor, err := principal.ActorSnapshot()
	if err != nil {
		return Preflight{}, fmt.Errorf("%w: %w", ErrPreflightInvalid, err)
	}

	statuses, err := s.clusters.ListClusters(ctx)
	if err != nil {
		return Preflight{}, err
	}
	connected := false
	for _, status := range statuses {
		if status.Name == cfg.ClusterName && status.ConnectionState == cluster.ConnectionStateConnected {
			connected = true
			break
		}
	}
	if !connected {
		return Preflight{}, ErrPreflightControllerUnreachable
	}

	createdAt := s.now().UTC()
	record := Preflight{
		ID: uuid.New(),
		Snapshot: PreflightSnapshot{
			PlacementName:         placementName,
			ConfigurationChecksum: s.placements.Checksum(),
			ClusterName:           cfg.ClusterName,
			JobNamespace:          cfg.JobNamespace,
			ServiceAccountName:    cfg.ServiceAccountName,
			Resources:             cfg.Resources,
			NodeSelector:          cfg.NodeSelector,
			PodTemplate:           cfg.PodTemplate,
		},
		State:        PreflightStateQueued,
		CleanupState: PreflightCleanupPending,
		RequestedBy:  actor,
		CreatedAt:    createdAt,
	}

	return s.store.CreatePlacementPreflight(ctx, record)
}

func (s *PreflightService) List(ctx context.Context, filter PreflightListFilter) (PreflightList, error) {
	if _, found := s.placements.Lookup(filter.PlacementName); !found {
		return PreflightList{}, ErrPreflightPlacementNotFound
	}
	if filter.Limit < 1 || filter.Limit > MaximumPreflightListLimit || filter.Offset < 0 {
		return PreflightList{}, fmt.Errorf("%w: invalid list window", ErrPreflightInvalid)
	}

	return s.store.ListPlacementPreflights(ctx, filter)
}

func (s *PreflightService) Next(ctx context.Context, clusterName string) (Preflight, error) {
	now := s.now().UTC()
	return s.store.ClaimPlacementPreflight(ctx, clusterName, now.Add(-PreflightDispatchRetryDelay), now)
}

func (s *PreflightService) Report(ctx context.Context, report PreflightReport) (Preflight, error) {
	if err := validatePreflightReport(report); err != nil {
		return Preflight{}, err
	}
	return s.store.RecordPlacementPreflightReport(ctx, report)
}

func validatePreflightReport(report PreflightReport) error {
	if report.ID == uuid.Nil || report.ClusterName == "" || report.ObservedAt.IsZero() {
		return fmt.Errorf("%w: identity and observation time are required", ErrPreflightInvalid)
	}
	if report.State != PreflightStateRunning && report.State != PreflightStateSucceeded &&
		report.State != PreflightStateFailed {
		return fmt.Errorf("%w: state is invalid", ErrPreflightInvalid)
	}
	if report.State == PreflightStateRunning {
		if report.CleanupState != PreflightCleanupPending || report.JobUID == nil ||
			report.Reason != nil || report.Message != nil {
			return fmt.Errorf("%w: running reports require a Job and pending cleanup", ErrPreflightInvalid)
		}
	} else if report.CleanupState == PreflightCleanupPending {
		return fmt.Errorf("%w: terminal reports require terminal cleanup state", ErrPreflightInvalid)
	} else if report.Reason == nil || report.Message == nil {
		return fmt.Errorf("%w: terminal reports require a reason and message", ErrPreflightInvalid)
	} else if report.State == PreflightStateSucceeded && report.CleanupState != PreflightCleanupSucceeded {
		return fmt.Errorf("%w: successful reports require successful cleanup", ErrPreflightInvalid)
	} else if report.CleanupState != PreflightCleanupNotRequired && report.JobUID == nil {
		return fmt.Errorf("%w: reports with Job cleanup require a Job UID", ErrPreflightInvalid)
	}
	if len(report.Diagnostics) > 20 {
		return fmt.Errorf("%w: too many diagnostics", ErrPreflightInvalid)
	}
	if report.JobUID != nil && (len(*report.JobUID) < 1 || len(*report.JobUID) > 200) {
		return fmt.Errorf("%w: Job UID is invalid", ErrPreflightInvalid)
	}
	if report.Reason != nil && (len(*report.Reason) < 1 || len(*report.Reason) > 200) {
		return fmt.Errorf("%w: reason is invalid", ErrPreflightInvalid)
	}
	if report.Message != nil && (len(*report.Message) < 1 || len(*report.Message) > 4096) {
		return fmt.Errorf("%w: message is invalid", ErrPreflightInvalid)
	}
	for _, diagnostic := range report.Diagnostics {
		if len(diagnostic.Reason) < 1 || len(diagnostic.Reason) > 200 ||
			len(diagnostic.Message) < 1 || len(diagnostic.Message) > 1024 || diagnostic.Count < 1 {
			return fmt.Errorf("%w: diagnostic is invalid", ErrPreflightInvalid)
		}
	}

	return nil
}

// ValidateReportTransition preserves the Job identity and immutable result
// established by earlier reports.
func ValidateReportTransition(current Preflight, report PreflightReport) error {
	if current.State == PreflightStateSucceeded || current.State == PreflightStateFailed {
		if report.State != current.State || report.CleanupState != current.CleanupState ||
			!sameOptionalString(report.JobUID, current.JobUID) ||
			!sameOptionalString(report.Reason, current.Reason) ||
			!sameOptionalString(report.Message, current.Message) {
			return ErrPreflightConflict
		}

		return nil
	}

	if current.JobUID != nil && !sameOptionalString(report.JobUID, current.JobUID) {
		return ErrPreflightConflict
	}
	if current.State == PreflightStateRunning && report.CleanupState == PreflightCleanupNotRequired {
		return ErrPreflightConflict
	}

	return nil
}

func sameOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}

	return *left == *right
}
