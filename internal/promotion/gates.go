package promotion

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/gate"
)

func (s *ReviewService) GateSummaries(
	ctx context.Context,
	records []Promotion,
	at time.Time,
) (map[uuid.UUID]gate.Summary, error) {
	ids := make([]uuid.UUID, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.ID)
	}
	if len(ids) == 0 {
		return nil, nil
	}

	summaries, err := s.store.ListPromotionGateSummaries(ctx, ids, s.gateEvaluatorProcessID, at)
	if err != nil {
		return nil, fmt.Errorf("load Promotion Gate summaries: %w", err)
	}

	return summaries, nil
}
