package promotion

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type creationReconciliationTestStore struct {
	claim  CreationReconciliationClaim
	report CreationReconciliationReport
	record Promotion
	err    error
}

func (s *creationReconciliationTestStore) ClaimPromotionCreationReconciliation(
	_ context.Context,
	claim CreationReconciliationClaim,
) (Promotion, error) {
	s.claim = claim
	return s.record, s.err
}

func (s *creationReconciliationTestStore) ReportPromotionCreationReconciliation(
	_ context.Context,
	report CreationReconciliationReport,
) (Promotion, error) {
	s.report = report
	return s.record, s.err
}

func TestCreationReconciliationServiceBuildsCommandAndReport(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 23, 18, 0, 0, 123456789, time.UTC)
	promotionID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1941")
	stageSnapshot, err := json.Marshal(PromotionStageSnapshot{
		ClusterName: "colo",
		Project:     "kargo-core",
	})
	if err != nil {
		t.Fatal(err)
	}
	uid := "promotion-uid"
	store := &creationReconciliationTestStore{record: Promotion{
		ID:                  promotionID,
		Integration:         "prod",
		ExternalPromotionID: "external-dns-prod.01test.freight",
		StageSnapshot:       stageSnapshot,
		KargoPromotionUID:   &uid,
	}}
	service := NewCreationReconciliationService(store)
	service.now = func() time.Time { return now }

	command, err := service.Next(t.Context(), "colo")
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if command.PromotionID != promotionID || command.Integration != "prod" ||
		command.Project != "kargo-core" || command.KargoName != store.record.ExternalPromotionID ||
		command.KargoUID != uid {
		t.Errorf("command = %#v", command)
	}
	if store.claim.ClusterName != "colo" || store.claim.ClaimedAt.Nanosecond() != 123456000 ||
		store.claim.Interval != DefaultCreationReconciliationInterval {
		t.Errorf("claim = %#v", store.claim)
	}

	diagnostic := "Kargo Promotion completed before its callback."
	err = service.Report(t.Context(), "colo", CreationReconciliationReport{
		PromotionID: promotionID,
		KargoName:   command.KargoName,
		KargoUID:    uid,
		Phase:       CreationReconciliationSucceeded,
		Diagnostic:  diagnostic,
	})
	if err != nil {
		t.Fatalf("Report() error = %v", err)
	}
	if store.report.ClusterName != "colo" || !store.report.ReportedAt.Equal(now.Truncate(time.Microsecond)) ||
		store.report.Phase != CreationReconciliationSucceeded || store.report.Diagnostic != diagnostic {
		t.Errorf("report = %#v", store.report)
	}
}

func TestCreationReconciliationReportRequiresTerminalPhaseAndCanonicalIdentity(t *testing.T) {
	t.Parallel()

	base := CreationReconciliationReport{
		PromotionID: uuid.New(),
		ClusterName: "colo",
		KargoName:   "external-dns-prod.01test.freight",
		KargoUID:    "promotion-uid",
		Diagnostic:  "terminal before callback",
		ReportedAt:  time.Now().UTC(),
	}
	for _, phase := range []CreationReconciliationPhase{
		CreationReconciliationFailed,
		CreationReconciliationErrored,
		CreationReconciliationAborted,
		CreationReconciliationSucceeded,
	} {
		report := base
		report.Phase = phase
		if err := report.Validate(); err != nil {
			t.Errorf("Validate(%q) error = %v", phase, err)
		}
	}

	nonterminal := base
	nonterminal.Phase = "Running"
	if err := nonterminal.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nonterminal phase error = %v, want ErrInvalid", err)
	}
	missingUID := base
	missingUID.Phase = CreationReconciliationFailed
	missingUID.KargoUID = ""
	if err := missingUID.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing UID error = %v, want ErrInvalid", err)
	}
}
