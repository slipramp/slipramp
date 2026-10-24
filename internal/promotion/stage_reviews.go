package promotion

import "context"

type StageReviewRecord struct {
	Stage                 Stage
	Policy                *StagePolicy
	PendingPromotionCount int
}

type StageReviewPage struct {
	Items []StageReviewRecord
	Total int
}

type StageReviewStore interface {
	ListPromotionStageReviews(context.Context, StageListFilter) (StageReviewPage, error)
}

func (s *ReviewService) ListStages(ctx context.Context, filter StageListFilter) (StageReviewList, error) {
	if err := validateListWindow(filter.Limit, filter.Offset); err != nil {
		return StageReviewList{}, err
	}
	now := s.currentTime()
	if err := s.ExpirePendingAt(ctx, now); err != nil {
		return StageReviewList{}, err
	}

	page, err := s.store.ListPromotionStageReviews(ctx, filter)
	if err != nil {
		return StageReviewList{}, err
	}

	items := make([]StageReview, 0, len(page.Items))
	for _, record := range page.Items {
		items = append(items, StageReview{
			Stage:                 record.Stage,
			Policy:                policyView(record.Policy, now),
			PendingPromotionCount: record.PendingPromotionCount,
		})
	}

	return StageReviewList{Items: items, Total: page.Total, Limit: filter.Limit, Offset: filter.Offset}, nil
}
