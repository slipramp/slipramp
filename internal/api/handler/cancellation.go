package handler

import (
	"time"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/cancellation"
	"github.com/slipramp/slipramp/internal/productidentity"
)

func cancellationResponse(state cancellation.State, at *time.Time, actor *productidentity.ActorSnapshot) api.Cancellation {
	return api.Cancellation{State: api.CancellationState(state), RequestedAt: at, RequestedBy: apiActorSnapshot(actor)}
}

func apiActorSnapshot(value *productidentity.ActorSnapshot) *api.ActorSnapshot {
	if value == nil {
		return nil
	}

	return new(api.NewActorSnapshot(*value))
}
