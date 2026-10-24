package promotion

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/cluster"
	"github.com/slipramp/slipramp/internal/kubevalidate"
)

const DefaultCreationReconciliationInterval = 15 * time.Second

type CreationReconciliationPhase string

const (
	CreationReconciliationFailed    CreationReconciliationPhase = "Failed"
	CreationReconciliationErrored   CreationReconciliationPhase = "Errored"
	CreationReconciliationAborted   CreationReconciliationPhase = "Aborted"
	CreationReconciliationSucceeded CreationReconciliationPhase = "Succeeded"
)

type CreationReconciliationClaim struct {
	ClusterName string
	ClaimedAt   time.Time
	Interval    time.Duration
}

type CreationReconciliationCommand struct {
	PromotionID uuid.UUID
	Integration string
	Project     string
	KargoName   string
	KargoUID    string
}

type CreationReconciliationReport struct {
	PromotionID uuid.UUID
	ClusterName string
	KargoName   string
	KargoUID    string
	Phase       CreationReconciliationPhase
	Diagnostic  string
	ReportedAt  time.Time
}

type CreationReconciliationStore interface {
	ClaimPromotionCreationReconciliation(context.Context, CreationReconciliationClaim) (Promotion, error)
	ReportPromotionCreationReconciliation(context.Context, CreationReconciliationReport) (Promotion, error)
}

type CreationReconciliationService struct {
	store    CreationReconciliationStore
	interval time.Duration
	now      func() time.Time
}

func NewCreationReconciliationService(store CreationReconciliationStore) *CreationReconciliationService {
	return &CreationReconciliationService{
		store:    store,
		interval: DefaultCreationReconciliationInterval,
		now:      time.Now,
	}
}

func (s *CreationReconciliationService) Next(
	ctx context.Context,
	clusterName string,
) (CreationReconciliationCommand, error) {
	if err := cluster.ValidateName(clusterName); err != nil {
		return CreationReconciliationCommand{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}

	record, err := s.store.ClaimPromotionCreationReconciliation(ctx, CreationReconciliationClaim{
		ClusterName: clusterName,
		ClaimedAt:   s.now().UTC().Truncate(time.Microsecond),
		Interval:    s.interval,
	})
	if err != nil {
		return CreationReconciliationCommand{}, err
	}

	var stage PromotionStageSnapshot
	if err := json.Unmarshal(record.StageSnapshot, &stage); err != nil {
		return CreationReconciliationCommand{}, fmt.Errorf("decode Promotion reconciliation Stage snapshot: %w", err)
	}
	if record.KargoPromotionUID == nil {
		return CreationReconciliationCommand{}, fmt.Errorf("%w: claimed Promotion has no Kargo UID", ErrInvalid)
	}
	command := CreationReconciliationCommand{
		PromotionID: record.ID,
		Integration: record.Integration,
		Project:     stage.Project,
		KargoName:   record.ExternalPromotionID,
		KargoUID:    *record.KargoPromotionUID,
	}
	if err := command.Validate(); err != nil {
		return CreationReconciliationCommand{}, err
	}
	if stage.ClusterName != clusterName {
		return CreationReconciliationCommand{}, fmt.Errorf(
			"%w: claimed Promotion belongs to another cluster",
			ErrInvalid,
		)
	}

	return command, nil
}

func (s *CreationReconciliationService) Report(
	ctx context.Context,
	clusterName string,
	report CreationReconciliationReport,
) error {
	report.ClusterName = clusterName
	report.ReportedAt = s.now().UTC().Truncate(time.Microsecond)
	if err := report.Validate(); err != nil {
		return err
	}

	_, err := s.store.ReportPromotionCreationReconciliation(ctx, report)
	return err
}

func (c CreationReconciliationClaim) Validate() error {
	if err := cluster.ValidateName(c.ClusterName); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if c.ClaimedAt.IsZero() || c.Interval <= 0 {
		return fmt.Errorf("%w: reconciliation time and positive interval are required", ErrInvalid)
	}

	return nil
}

func (c CreationReconciliationCommand) Validate() error {
	if c.PromotionID == uuid.Nil {
		return fmt.Errorf("%w: Promotion ID is required", ErrInvalid)
	}
	if err := kubevalidate.DNSLabel("Kargo integration name", c.Integration); err != nil {
		return invalid(err)
	}
	if err := kubevalidate.DNSLabel("Kargo project", c.Project); err != nil {
		return invalid(err)
	}
	if err := kubevalidate.DNSSubdomain("Kargo Promotion name", c.KargoName); err != nil {
		return invalid(err)
	}

	return validateText("Kargo Promotion UID", c.KargoUID, maximumTextLength)
}

func (r CreationReconciliationReport) Validate() error {
	if r.PromotionID == uuid.Nil {
		return fmt.Errorf("%w: Promotion ID is required", ErrInvalid)
	}
	if err := cluster.ValidateName(r.ClusterName); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if err := kubevalidate.DNSSubdomain("Kargo Promotion name", r.KargoName); err != nil {
		return invalid(err)
	}
	if err := validateText("Kargo Promotion UID", r.KargoUID, maximumTextLength); err != nil {
		return err
	}
	if !r.Phase.Terminal() {
		return fmt.Errorf("%w: Kargo Promotion phase is not terminal", ErrInvalid)
	}
	if r.ReportedAt.IsZero() {
		return fmt.Errorf("%w: reconciliation report time is required", ErrInvalid)
	}
	if strings.TrimSpace(r.Diagnostic) != r.Diagnostic {
		return fmt.Errorf("%w: reconciliation diagnostic must not have surrounding whitespace", ErrInvalid)
	}

	return validateText("reconciliation diagnostic", r.Diagnostic, maximumMessageLength)
}

func (p CreationReconciliationPhase) Terminal() bool {
	switch p {
	case CreationReconciliationFailed,
		CreationReconciliationErrored,
		CreationReconciliationAborted,
		CreationReconciliationSucceeded:
		return true
	default:
		return false
	}
}
