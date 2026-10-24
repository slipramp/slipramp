package promotion

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/cluster"
	"github.com/slipramp/slipramp/internal/kubevalidate"
)

const exhaustedCreationDispatchMessage = "Kargo Promotion creation failed after the dispatch retry budget was exhausted."

var defaultCreationDispatchRetryDelays = []time.Duration{
	5 * time.Second,
	30 * time.Second,
	2 * time.Minute,
	10 * time.Minute,
	30 * time.Minute,
}

type CreationDispatchResult string

const (
	CreationDispatchCreated   CreationDispatchResult = "created"
	CreationDispatchRetryable CreationDispatchResult = "retryable"
	CreationDispatchFailed    CreationDispatchResult = "failed"
)

type CreationDispatchClaim struct {
	ClusterName      string
	ClaimedAt        time.Time
	RetryDelays      []time.Duration
	ExhaustedMessage string
}

type CreationCommand struct {
	PromotionID  uuid.UUID
	Attempt      int
	ExternalName string
	Integration  string
	Project      string
	Stage        string
	StageUID     string
	Freight      string
}

type CreationDispatchReport struct {
	PromotionID uuid.UUID
	ClusterName string
	Attempt     int
	Result      CreationDispatchResult
	KargoName   *string
	KargoUID    *string
	Diagnostic  *string
	ReportedAt  time.Time
}

type CreationDispatchStore interface {
	ClaimPromotionCreation(context.Context, CreationDispatchClaim) (Promotion, error)
	ReportPromotionCreation(context.Context, CreationDispatchReport) (Promotion, error)
}

type CreationDispatchService struct {
	store       CreationDispatchStore
	retryDelays []time.Duration
	now         func() time.Time
}

func NewCreationDispatchService(store CreationDispatchStore) *CreationDispatchService {
	return &CreationDispatchService{
		store:       store,
		retryDelays: slices.Clone(defaultCreationDispatchRetryDelays),
		now:         time.Now,
	}
}

func (s *CreationDispatchService) Next(ctx context.Context, clusterName string) (CreationCommand, error) {
	if err := cluster.ValidateName(clusterName); err != nil {
		return CreationCommand{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}

	record, err := s.store.ClaimPromotionCreation(ctx, CreationDispatchClaim{
		ClusterName:      clusterName,
		ClaimedAt:        s.now().UTC().Truncate(time.Microsecond),
		RetryDelays:      slices.Clone(s.retryDelays),
		ExhaustedMessage: exhaustedCreationDispatchMessage,
	})
	if err != nil {
		return CreationCommand{}, err
	}

	stage := record.StageSnapshot
	freight := record.SelectedFreightSnapshot
	if freight == nil {
		return CreationCommand{}, fmt.Errorf("%w: claimed Promotion has no selected Freight", ErrInvalid)
	}
	command := CreationCommand{
		PromotionID:  record.ID,
		Attempt:      record.DispatchAttemptCount,
		ExternalName: record.ExternalPromotionID,
		Integration:  record.Integration,
		Project:      stage.Project,
		Stage:        stage.Name,
		StageUID:     stage.KubernetesUID,
		Freight:      freight.Name,
	}
	if err := command.Validate(); err != nil {
		return CreationCommand{}, err
	}
	if stage.ClusterName != clusterName {
		return CreationCommand{}, fmt.Errorf("%w: claimed Promotion belongs to another cluster", ErrInvalid)
	}

	return command, nil
}

func (s *CreationDispatchService) Report(
	ctx context.Context,
	clusterName string,
	report CreationDispatchReport,
) error {
	report.ClusterName = clusterName
	report.ReportedAt = s.now().UTC().Truncate(time.Microsecond)
	if err := report.Validate(); err != nil {
		return err
	}

	_, err := s.store.ReportPromotionCreation(ctx, report)
	return err
}

func (c CreationDispatchClaim) Validate() error {
	if err := cluster.ValidateName(c.ClusterName); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if c.ClaimedAt.IsZero() || len(c.RetryDelays) == 0 {
		return fmt.Errorf("%w: dispatch time and retry delays are required", ErrInvalid)
	}
	for _, delay := range c.RetryDelays {
		if delay <= 0 {
			return fmt.Errorf("%w: dispatch retry delays must be positive", ErrInvalid)
		}
	}
	return validateText("exhausted dispatch diagnostic", c.ExhaustedMessage, maximumMessageLength)
}

func (c CreationCommand) Validate() error {
	if c.PromotionID == uuid.Nil || c.Attempt < 1 {
		return fmt.Errorf("%w: Promotion ID and positive dispatch attempt are required", ErrInvalid)
	}
	if err := kubevalidate.DNSSubdomain("external Kargo Promotion name", c.ExternalName); err != nil {
		return invalid(err)
	}
	if err := kubevalidate.DNSLabel("Kargo integration name", c.Integration); err != nil {
		return invalid(err)
	}
	if err := kubevalidate.DNSLabel("Kargo project", c.Project); err != nil {
		return invalid(err)
	}
	if err := kubevalidate.DNSSubdomain("Kargo Stage name", c.Stage); err != nil {
		return invalid(err)
	}
	if err := validateText("Kargo Stage UID", c.StageUID, maximumTextLength); err != nil {
		return err
	}
	if err := kubevalidate.DNSSubdomain("Kargo Freight name", c.Freight); err != nil {
		return invalid(err)
	}

	return nil
}

func (r CreationDispatchReport) Validate() error {
	if r.PromotionID == uuid.Nil || r.Attempt < 1 {
		return fmt.Errorf("%w: Promotion ID and positive dispatch attempt are required", ErrInvalid)
	}
	if err := cluster.ValidateName(r.ClusterName); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if r.ReportedAt.IsZero() {
		return fmt.Errorf("%w: dispatch report time is required", ErrInvalid)
	}
	switch r.Result {
	case CreationDispatchCreated:
		if r.KargoName == nil || r.KargoUID == nil || r.Diagnostic != nil {
			return fmt.Errorf("%w: created dispatch requires only the Kargo name and UID", ErrInvalid)
		}
		if err := kubevalidate.DNSSubdomain("Kargo Promotion name", *r.KargoName); err != nil {
			return invalid(err)
		}
		return validateText("Kargo Promotion UID", *r.KargoUID, maximumTextLength)
	case CreationDispatchRetryable, CreationDispatchFailed:
		if r.KargoName != nil || r.KargoUID != nil || r.Diagnostic == nil {
			return fmt.Errorf("%w: failed dispatch requires only a diagnostic", ErrInvalid)
		}
		if strings.TrimSpace(*r.Diagnostic) != *r.Diagnostic {
			return fmt.Errorf("%w: dispatch diagnostic must not have surrounding whitespace", ErrInvalid)
		}
		return validateText("dispatch diagnostic", *r.Diagnostic, maximumMessageLength)
	default:
		return fmt.Errorf("%w: dispatch result is invalid", ErrInvalid)
	}
}
