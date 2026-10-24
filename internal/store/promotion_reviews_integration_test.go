package store

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/repository"
)

func TestPromotionReviewProjectionAndPendingCountsIntegration(t *testing.T) {
	createdAt := time.Date(2026, time.October, 24, 12, 0, 0, 0, time.UTC)
	fixture := newPromotionIntegrationFixture(t, createdAt)
	accept := func(externalID string, at time.Time) promotion.Promotion {
		t.Helper()
		record, err := fixture.database.AcceptPromotionRequest(
			fixture.ctx, fixture.acceptance(t, fixture.request(externalID), at),
		)
		if err != nil {
			t.Fatal(err)
		}

		return record
	}

	automatic := accept("automatic", createdAt.Add(4*time.Minute))
	first := preparePromotionForReview(t, fixture, "pending-first", createdAt.Add(2*time.Minute))
	second := preparePromotionForReview(t, fixture, "pending-second", createdAt.Add(2*time.Minute))
	preparing := accept("preparing", createdAt.Add(3*time.Minute))
	tiedIDs := []uuid.UUID{first.ID, second.ID}
	slices.SortFunc(tiedIDs, func(left, right uuid.UUID) int {
		return strings.Compare(right.String(), left.String())
	})
	baseIDs := []uuid.UUID{tiedIDs[0], tiedIDs[1], automatic.ID, preparing.ID}

	otherRepository, err := fixture.database.CreateRepository(fixture.ctx, repository.Repository{
		ID: uuid.New(), Name: "other", CloneURL: "https://github.com/example/other.git", Branch: "main",
		CommitIndexing: repository.CommitIndexingObservedHeads, CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	edgeStage := promotionTestStage("edge", "edge-cluster", "kargo-edge", "edge-prod", createdAt.Add(time.Minute))
	edgeStage.RepositoryName = otherRepository.Name
	edgeStage.RepositoryURL = otherRepository.CloneURL
	unusedStage := promotionTestStage("edge", "edge-cluster", "kargo-edge", "unused-prod", edgeStage.LastObservedAt)
	if err := fixture.database.ReplacePromotionStageSnapshot(
		fixture.ctx,
		promotionTestSnapshot(uuid.New(), "edge", "edge-cluster", edgeStage.LastObservedAt),
		[]promotion.Stage{edgeStage, unusedStage},
	); err != nil {
		t.Fatal(err)
	}
	edgeFixture := fixture
	edgeFixture.stage = edgeStage
	edge := preparePromotionForReview(t, edgeFixture, "edge-pending", createdAt.Add(150*time.Second))
	allIDs := append([]uuid.UUID{edge.ID}, baseIDs...)

	// Historical filters use the accepted snapshot even after current inventory moves and archives.
	if _, err := fixture.database.pool.Exec(fixture.ctx, `
UPDATE promotion_stages
SET cluster_name = 'moved-cluster', project = 'moved-project', application = 'moved-app', archived_at = $2
WHERE id = $1`, fixture.stage.ID, createdAt.Add(6*time.Minute)); err != nil {
		t.Fatal(err)
	}

	baseCommit := fixture.request("unused").BaseRevision
	candidateCommit := fixture.request("unused").CandidateRevision
	for _, test := range []struct {
		name   string
		filter promotion.PromotionListFilter
		total  int
		ids    []uuid.UUID
	}{
		{
			name: "pending first then newest and UUID", total: 5, ids: allIDs,
			filter: promotion.PromotionListFilter{Limit: 10},
		},
		{name: "ordered page", filter: promotion.PromotionListFilter{Limit: 2, Offset: 1}, total: 5, ids: tiedIDs},
		{name: "empty page retains total", filter: promotion.PromotionListFilter{Limit: 2, Offset: 1 << 31}, total: 5},
		{
			name: "pending Stage", total: 2, ids: tiedIDs,
			filter: promotion.PromotionListFilter{Status: promotion.StatusPending, StageID: &fixture.stage.ID, Limit: 10},
		},
		{
			name: "historical Stage facts", total: 4, ids: baseIDs,
			filter: promotion.PromotionListFilter{
				Integration: "prod", Cluster: "colo", Project: "kargo-core", Application: "external-dns", Limit: 10,
			},
		},
		{
			name: "current Stage facts do not replace history",
			filter: promotion.PromotionListFilter{
				Cluster: "moved-cluster", Project: "moved-project", Application: "moved-app", Limit: 10,
			},
		},
		{
			name: "integration", total: 1, ids: []uuid.UUID{edge.ID},
			filter: promotion.PromotionListFilter{Integration: "edge", Limit: 10},
		},
		{
			name: "cluster", total: 1, ids: []uuid.UUID{edge.ID},
			filter: promotion.PromotionListFilter{Cluster: "edge-cluster", Limit: 10},
		},
		{
			name: "project", total: 1, ids: []uuid.UUID{edge.ID},
			filter: promotion.PromotionListFilter{Project: "kargo-edge", Limit: 10},
		},
		{
			name: "application", total: 1, ids: []uuid.UUID{edge.ID},
			filter: promotion.PromotionListFilter{Application: "edge", Limit: 10},
		},
		{
			name:   "mismatched scope",
			filter: promotion.PromotionListFilter{Integration: "prod", Cluster: "edge-cluster", Limit: 10},
		},
		{
			name: "repository", total: 4, ids: baseIDs,
			filter: promotion.PromotionListFilter{RepositoryID: &fixture.repositoryID, Limit: 10},
		},
		{
			name: "base revision in repository", total: 4, ids: baseIDs,
			filter: promotion.PromotionListFilter{RepositoryID: &fixture.repositoryID, CommitSHA: baseCommit, Limit: 10},
		},
		{
			name: "candidate revision in repository", total: 4, ids: baseIDs,
			filter: promotion.PromotionListFilter{RepositoryID: &fixture.repositoryID, CommitSHA: candidateCommit, Limit: 10},
		},
		{
			name: "same revision in another repository", total: 1, ids: []uuid.UUID{edge.ID},
			filter: promotion.PromotionListFilter{RepositoryID: &otherRepository.ID, CommitSHA: candidateCommit, Limit: 10},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			page, err := fixture.database.ListPromotionReviews(fixture.ctx, test.filter)
			if err != nil {
				t.Fatal(err)
			}
			ids := make([]uuid.UUID, 0, len(page.Items))
			for _, item := range page.Items {
				ids = append(ids, item.ID)
			}
			if page.Total != test.total || !slices.Equal(ids, test.ids) {
				t.Errorf("Promotion page = %v, total %d; want %v, total %d", ids, page.Total, test.ids, test.total)
			}
		})
	}

	counts, err := fixture.database.CountPendingPromotionsByStage(
		fixture.ctx, []uuid.UUID{fixture.stage.ID, edgeStage.ID, unusedStage.ID, fixture.stage.ID, uuid.New()},
	)
	if err != nil || len(counts) != 2 || counts[fixture.stage.ID] != 2 || counts[edgeStage.ID] != 1 {
		t.Errorf("grouped Stage pending counts = %v, %v", counts, err)
	}
	counts, err = fixture.database.CountPendingPromotionsByStage(fixture.ctx, []uuid.UUID{fixture.stage.ID})
	if err != nil || len(counts) != 1 || counts[fixture.stage.ID] != 2 {
		t.Errorf("selected Stage pending counts = %v, %v", counts, err)
	}
	counts, err = fixture.database.CountPendingPromotionsByStage(fixture.ctx, nil)
	if err != nil || counts == nil || len(counts) != 0 {
		t.Errorf("empty Stage selection pending counts = %v, %v", counts, err)
	}
}
