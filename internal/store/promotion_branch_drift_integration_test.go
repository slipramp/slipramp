package store

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/repository"
)

type failedPromotionBranchScanner struct {
	requests []repository.BranchComparisonRequest
}

func (s *failedPromotionBranchScanner) ScanBranches(
	_ context.Context,
	_ repository.Repository,
	requests []repository.BranchComparisonRequest,
) ([]repository.BranchComparison, error) {
	s.requests = append(s.requests, requests...)
	comparisons := make([]repository.BranchComparison, len(requests))
	for index := range comparisons {
		comparisons[index] = repository.BranchComparison{
			SourceBranchCommit: strings.Repeat("d", 40),
			TargetBranchCommit: strings.Repeat("e", 40),
			SourceDrift:        true,
			TargetDrift:        true,
		}
	}

	return comparisons, nil
}

func TestPromotionBranchDriftRetainsLastSuccessAfterFailedPromotionIntegration(t *testing.T) {
	ctx, _, database := openIntegrationDatabase(t)
	defer database.Close()

	observedAt := time.Date(2026, time.October, 28, 12, 0, 0, 0, time.UTC)
	record := repository.Repository{
		ID:             uuid.New(),
		Name:           "artifact-test",
		CloneURL:       "https://github.com/example/artifact-test.git",
		Branch:         "main",
		CommitIndexing: repository.CommitIndexingObservedHeads,
		CreatedAt:      observedAt,
	}
	if _, err := database.CreateRepository(ctx, record); err != nil {
		t.Fatalf("CreateRepository() error = %v", err)
	}

	lastSuccess := &promotion.SuccessfulRenderAcknowledgement{
		SourceRevision: strings.Repeat("a", 40),
		TargetRevision: strings.Repeat("b", 40),
		ResultRevision: strings.Repeat("c", 40),
	}
	succeededStage := promotionTestStage("prod", "colo", "kargo-security", "falco-prod", observedAt)
	succeededStage.SuccessfulRenderAcknowledgement = lastSuccess
	if err := database.ReplacePromotionStageSnapshot(
		ctx,
		promotionTestSnapshot(uuid.New(), "prod", "colo", observedAt),
		[]promotion.Stage{succeededStage},
	); err != nil {
		t.Fatalf("store successful Promotion Stage observation: %v", err)
	}

	failedAt := observedAt.Add(time.Minute)
	failedStage := promotionTestStage("prod", "colo", "kargo-security", "falco-prod", failedAt)
	failedStage.ResourceVersion = "2"
	if err := database.ReplacePromotionStageSnapshot(
		ctx,
		promotionTestSnapshot(uuid.New(), "prod", "colo", failedAt),
		[]promotion.Stage{failedStage},
	); err != nil {
		t.Fatalf("store failed Promotion Stage observation: %v", err)
	}

	scanner := &failedPromotionBranchScanner{}
	service := promotion.NewBranchDriftService(
		promotion.IntegrationSet{"prod": {}},
		database,
		scanner,
		time.Minute,
		30*time.Second,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if _, err := service.Refresh(ctx); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	result, err := service.List(ctx, promotion.BranchDriftListFilter{
		Status: promotion.BranchDriftStatusDrifted,
		Limit:  promotion.DefaultListLimit,
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if result.Total != 1 || len(result.Items) != 1 || len(result.Items[0].Stages) != 1 ||
		result.Items[0].Stages[0].Stage.Name != "falco-prod" {
		t.Fatalf("drifted Stages = %#v", result)
	}
	if len(scanner.requests) != 1 ||
		scanner.requests[0].AcknowledgedSourceCommit != lastSuccess.SourceRevision ||
		scanner.requests[0].AcknowledgedTargetCommit != lastSuccess.ResultRevision {
		t.Errorf("branch comparison requests = %#v", scanner.requests)
	}
}
