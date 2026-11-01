package promotion

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type reconciliationTestStore struct {
	claim  ReconciliationClaim
	report ReconciliationReport
	record Promotion
	err    error
}

func (s *reconciliationTestStore) ClaimPromotionReconciliation(
	_ context.Context,
	claim ReconciliationClaim,
) (Promotion, error) {
	s.claim = claim
	return s.record, s.err
}

func (s *reconciliationTestStore) ReportPromotionReconciliation(
	_ context.Context,
	report ReconciliationReport,
) (Promotion, error) {
	s.report = report
	return s.record, s.err
}

func TestReconciliationServiceBuildsCommandAndReport(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 23, 18, 0, 0, 123456789, time.UTC)
	promotionID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1941")
	stageSnapshot := PromotionStageSnapshot{ClusterName: "colo", Project: "kargo-core", Name: "prod"}
	uid := "promotion-uid"
	store := &reconciliationTestStore{record: Promotion{
		ID:                  promotionID,
		Source:              SourceSlipramp,
		Integration:         "prod",
		ExternalPromotionID: "external-dns-prod.01test.freight",
		StageSnapshot:       stageSnapshot,
		KargoPromotionUID:   &uid,
	}}
	service := NewReconciliationService(store)
	service.now = func() time.Time { return now }

	command, err := service.Next(t.Context(), "colo")
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if command.PromotionID != promotionID || command.Source != SourceSlipramp || command.Integration != "prod" ||
		command.Project != "kargo-core" || command.Stage != "prod" ||
		command.KargoName != store.record.ExternalPromotionID || command.KargoUID == nil || *command.KargoUID != uid {
		t.Errorf("command = %#v", command)
	}
	if store.claim.ClusterName != "colo" || store.claim.ClaimedAt.Nanosecond() != 123456000 ||
		store.claim.Interval != DefaultReconciliationInterval {
		t.Errorf("claim = %#v", store.claim)
	}

	phase := KargoPhaseSucceeded
	diagnostic := "Kargo completed the Promotion."
	err = service.Report(t.Context(), "colo", ReconciliationReport{
		PromotionID: promotionID,
		KargoName:   command.KargoName,
		KargoUID:    uid,
		Phase:       &phase,
		Diagnostic:  &diagnostic,
	})
	if err != nil {
		t.Fatalf("Report() error = %v", err)
	}
	if store.report.ClusterName != "colo" || !store.report.ObservedAt.Equal(now.Truncate(time.Microsecond)) ||
		store.report.Phase == nil || *store.report.Phase != KargoPhaseSucceeded ||
		store.report.Diagnostic == nil || *store.report.Diagnostic != diagnostic {
		t.Errorf("report = %#v", store.report)
	}
}

func TestReconciliationCommandAllowsUnboundKargoOriginatedIdentity(t *testing.T) {
	t.Parallel()

	command := ReconciliationCommand{
		PromotionID: uuid.New(),
		Source:      SourceKargo,
		Integration: "prod",
		Project:     "kargo-core",
		Stage:       "prod",
		KargoName:   "external-dns-prod.01test.freight",
	}
	if err := command.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	command.Source = SourceSlipramp
	if err := command.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing Slipramp-originated UID error = %v, want ErrInvalid", err)
	}
}

func TestReconciliationReportAcceptsIdentityAndEveryTerminalPhase(t *testing.T) {
	t.Parallel()

	base := ReconciliationReport{
		PromotionID: uuid.New(),
		ClusterName: "colo",
		KargoName:   "external-dns-prod.01test.freight",
		KargoUID:    "promotion-uid",
		ObservedAt:  time.Now().UTC(),
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("identity-only Validate() error = %v", err)
	}

	for _, phase := range []KargoPhase{
		KargoPhaseFailed,
		KargoPhaseErrored,
		KargoPhaseAborted,
		KargoPhaseSucceeded,
	} {
		report := base
		report.Phase = &phase
		if err := report.Validate(); err != nil {
			t.Errorf("Validate(%q) error = %v", phase, err)
		}
	}

	nonterminal := KargoPhase("Running")
	base.Phase = &nonterminal
	if err := base.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nonterminal phase error = %v, want ErrInvalid", err)
	}
}
