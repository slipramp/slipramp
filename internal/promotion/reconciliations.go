package promotion

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/cluster"
	"github.com/slipramp/slipramp/internal/kubevalidate"
)

const DefaultReconciliationInterval = 15 * time.Second

type ReconciliationClaim struct {
	ClusterName string
	ClaimedAt   time.Time
	Interval    time.Duration
}

type ReconciliationCommand struct {
	PromotionID uuid.UUID
	Source      PromotionSource
	Integration string
	Project     string
	Stage       string
	KargoName   string
	KargoUID    *string
}

type ReconciliationReport struct {
	PromotionID uuid.UUID
	ClusterName string
	KargoName   string
	KargoUID    string
	Phase       *KargoPhase
	Diagnostic  *string
	ObservedAt  time.Time
}

type ReconciliationStore interface {
	ClaimPromotionReconciliation(context.Context, ReconciliationClaim) (Promotion, error)
	ReportPromotionReconciliation(context.Context, ReconciliationReport) (Promotion, error)
}

type ReconciliationService struct {
	store    ReconciliationStore
	interval time.Duration
	now      func() time.Time
}

func NewReconciliationService(store ReconciliationStore) *ReconciliationService {
	return &ReconciliationService{
		store:    store,
		interval: DefaultReconciliationInterval,
		now:      time.Now,
	}
}

func (s *ReconciliationService) Next(
	ctx context.Context,
	clusterName string,
) (ReconciliationCommand, error) {
	if err := cluster.ValidateName(clusterName); err != nil {
		return ReconciliationCommand{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}

	record, err := s.store.ClaimPromotionReconciliation(ctx, ReconciliationClaim{
		ClusterName: clusterName,
		ClaimedAt:   s.now().UTC().Truncate(time.Microsecond),
		Interval:    s.interval,
	})
	if err != nil {
		return ReconciliationCommand{}, err
	}

	stage := record.StageSnapshot
	command := ReconciliationCommand{
		PromotionID: record.ID,
		Source:      record.Source,
		Integration: record.Integration,
		Project:     stage.Project,
		Stage:       stage.Name,
		KargoName:   record.ExternalPromotionID,
		KargoUID:    record.KargoPromotionUID,
	}
	if err := command.Validate(); err != nil {
		return ReconciliationCommand{}, err
	}
	if stage.ClusterName != clusterName {
		return ReconciliationCommand{}, fmt.Errorf(
			"%w: claimed Promotion belongs to another cluster",
			ErrInvalid,
		)
	}

	return command, nil
}

func (s *ReconciliationService) Report(
	ctx context.Context,
	clusterName string,
	report ReconciliationReport,
) error {
	report.ClusterName = clusterName
	report.ObservedAt = s.now().UTC().Truncate(time.Microsecond)
	if err := report.Validate(); err != nil {
		return err
	}

	_, err := s.store.ReportPromotionReconciliation(ctx, report)
	return err
}

func (c ReconciliationClaim) Validate() error {
	if err := cluster.ValidateName(c.ClusterName); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if c.ClaimedAt.IsZero() || c.Interval <= 0 {
		return fmt.Errorf("%w: reconciliation time and positive interval are required", ErrInvalid)
	}

	return nil
}

func (c ReconciliationCommand) Validate() error {
	if c.PromotionID == uuid.Nil {
		return fmt.Errorf("%w: Promotion ID is required", ErrInvalid)
	}
	if err := kubevalidate.DNSLabel("Kargo integration name", c.Integration); err != nil {
		return invalid(err)
	}
	if !c.Source.Valid() {
		return fmt.Errorf("%w: Promotion source is invalid", ErrInvalid)
	}
	if err := kubevalidate.DNSLabel("Kargo project", c.Project); err != nil {
		return invalid(err)
	}
	if err := kubevalidate.DNSSubdomain("Kargo Stage name", c.Stage); err != nil {
		return invalid(err)
	}
	if err := kubevalidate.DNSSubdomain("Kargo Promotion name", c.KargoName); err != nil {
		return invalid(err)
	}
	if c.KargoUID == nil {
		if c.Source != SourceKargo {
			return fmt.Errorf("%w: Slipramp-originated reconciliation requires a Kargo UID", ErrInvalid)
		}
		return nil
	}

	return validateText("Kargo Promotion UID", *c.KargoUID, maximumTextLength)
}

func (r ReconciliationReport) Validate() error {
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
	if r.ObservedAt.IsZero() {
		return fmt.Errorf("%w: reconciliation report time is required", ErrInvalid)
	}
	if r.Phase == nil {
		if r.Diagnostic != nil {
			return fmt.Errorf("%w: identity-only reconciliation cannot include a diagnostic", ErrInvalid)
		}
		return nil
	}
	if !r.Phase.Terminal() {
		return fmt.Errorf("%w: Kargo Promotion phase is not terminal", ErrInvalid)
	}
	if r.Diagnostic == nil {
		return nil
	}
	if strings.TrimSpace(*r.Diagnostic) != *r.Diagnostic {
		return fmt.Errorf("%w: reconciliation diagnostic must not have surrounding whitespace", ErrInvalid)
	}

	return validateText("reconciliation diagnostic", *r.Diagnostic, maximumMessageLength)
}
