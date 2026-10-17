package handler

import (
	"context"

	"github.com/slipramp/slipramp/internal/api"
)

type Handler struct{}

var _ api.StrictServerInterface = (*Handler)(nil)

func New() *Handler {
	return &Handler{}
}

func (*Handler) GetHealth(
	context.Context,
	api.GetHealthRequestObject,
) (api.GetHealthResponseObject, error) {
	return api.GetHealth200JSONResponse{
		Status: api.Ok,
	}, nil
}
