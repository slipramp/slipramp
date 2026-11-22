package run

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/gate"
)

type GateStore interface {
	ListRunGateSummaries(context.Context, []uuid.UUID, uuid.UUID, time.Time) (map[uuid.UUID]gate.Summary, error)
}

func (s *Service) GateSummaries(
	ctx context.Context,
	records []Run,
	at time.Time,
) (map[uuid.UUID]gate.Summary, error) {
	ids := make([]uuid.UUID, 0, len(records))
	for _, record := range records {
		if record.Snapshot.Action.Intent != catalog.ActionIntentPreview || len(record.Snapshot.Action.Gates) > 0 {
			ids = append(ids, record.ID)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}

	summaries, err := s.gates.ListRunGateSummaries(ctx, ids, s.gateEvaluatorProcessID, at)
	if err != nil {
		return nil, fmt.Errorf("load Run Gate summaries: %w", err)
	}

	return summaries, nil
}
