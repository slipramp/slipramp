package promotion

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

type creationDispatchTestStore struct {
	claim  CreationDispatchClaim
	report CreationDispatchReport
	record Promotion
	err    error
}

func (s *creationDispatchTestStore) ClaimPromotionCreation(
	_ context.Context,
	claim CreationDispatchClaim,
) (Promotion, error) {
	s.claim = claim
	return s.record, s.err
}

func (s *creationDispatchTestStore) ReportPromotionCreation(
	_ context.Context,
	report CreationDispatchReport,
) (Promotion, error) {
	s.report = report
	return s.record, s.err
}

func TestCreationDispatchServiceBuildsCommandAndReport(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 23, 18, 0, 0, 123456789, time.UTC)
	promotionID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1940")
	stageSnapshot, err := json.Marshal(PromotionStageSnapshot{
		ClusterName:   "colo",
		Project:       "kargo-core",
		Name:          "external-dns-prod",
		KubernetesUID: "stage-uid",
	})
	if err != nil {
		t.Fatal(err)
	}
	freightSnapshot, err := json.Marshal(FreightItem{
		Origin: "Warehouse/external-dns",
		Name:   "freight-42",
	})
	if err != nil {
		t.Fatal(err)
	}
	store := &creationDispatchTestStore{record: Promotion{
		ID:                      promotionID,
		Integration:             "prod",
		ExternalPromotionID:     "slipramp-" + promotionID.String(),
		StageSnapshot:           stageSnapshot,
		SelectedFreightSnapshot: freightSnapshot,
		DispatchAttemptCount:    2,
	}}
	service := NewCreationDispatchService(store)
	service.now = func() time.Time { return now }

	command, err := service.Next(t.Context(), "colo")
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if command.PromotionID != promotionID || command.Attempt != 2 ||
		command.Project != "kargo-core" || command.Stage != "external-dns-prod" ||
		command.StageUID != "stage-uid" || command.Freight != "freight-42" {
		t.Errorf("command = %#v", command)
	}
	if store.claim.ClusterName != "colo" || store.claim.ClaimedAt.Nanosecond() != 123456000 ||
		len(store.claim.RetryDelays) != len(defaultCreationDispatchRetryDelays) {
		t.Errorf("claim = %#v", store.claim)
	}

	uid := "promotion-uid"
	err = service.Report(t.Context(), "colo", CreationDispatchReport{
		PromotionID: promotionID,
		Attempt:     2,
		Result:      CreationDispatchCreated,
		KargoUID:    &uid,
	})
	if err != nil {
		t.Fatalf("Report() error = %v", err)
	}
	if store.report.ClusterName != "colo" || !store.report.ReportedAt.Equal(now.Truncate(time.Microsecond)) ||
		store.report.KargoUID == nil || *store.report.KargoUID != uid {
		t.Errorf("report = %#v", store.report)
	}
}

func TestCreationDispatchReportValidation(t *testing.T) {
	t.Parallel()

	base := CreationDispatchReport{
		PromotionID: uuid.New(),
		ClusterName: "colo",
		Attempt:     1,
		ReportedAt:  time.Now().UTC(),
	}
	uid := "promotion-uid"
	diagnostic := "admission denied"

	created := base
	created.Result = CreationDispatchCreated
	created.KargoUID = &uid
	if err := created.Validate(); err != nil {
		t.Fatalf("created report error = %v", err)
	}

	failed := base
	failed.Result = CreationDispatchFailed
	failed.Diagnostic = &diagnostic
	if err := failed.Validate(); err != nil {
		t.Fatalf("failed report error = %v", err)
	}

	invalid := base
	invalid.Result = CreationDispatchRetryable
	if err := invalid.Validate(); err == nil {
		t.Fatal("retryable report without diagnostic was accepted")
	}
}
