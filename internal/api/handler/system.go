package handler

import (
	"context"

	"github.com/slipramp/slipramp/internal/api"
)

func (*Handler) GetHealth(
	context.Context,
	api.GetHealthRequestObject,
) (api.GetHealthResponseObject, error) {
	return api.GetHealth200JSONResponse{
		Status: api.Ok,
	}, nil
}
