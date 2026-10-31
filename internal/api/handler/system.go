package handler

import (
	"context"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/readiness"
)

const productAPIContract = 1

func (h *Handler) GetClientCompatibility(
	context.Context,
	api.GetClientCompatibilityRequestObject,
) (api.GetClientCompatibilityResponseObject, error) {
	return api.GetClientCompatibility200JSONResponse{
		ServerVersion:         h.serviceVersion,
		ProductAPIContract:    productAPIContract,
		AuthenticationMethods: []string{"oidc-native", "api-key"},
	}, nil
}

func (*Handler) GetHealth(context.Context, api.GetHealthRequestObject) (api.GetHealthResponseObject, error) {
	return api.GetHealth200JSONResponse{
		Status: api.Ok,
	}, nil
}

func (h *Handler) GetReadiness(
	ctx context.Context,
	_ api.GetReadinessRequestObject,
) (api.GetReadinessResponseObject, error) {
	report := readiness.Report{}
	if h.readiness != nil {
		report = h.readiness.Check(ctx)
	}

	response := api.ReadinessResponse{
		Status:          api.NotReady,
		Database:        readinessStatus(report.Database),
		ArtifactStorage: readinessStatus(report.ArtifactStorage),
	}
	if report.Ready() {
		response.Status = api.Ready

		return api.GetReadiness200JSONResponse(response), nil
	}

	return api.GetReadiness503JSONResponse(response), nil
}

func readinessStatus(status readiness.DependencyStatus) api.DependencyStatus {
	if status == readiness.DependencyAvailable {
		return api.Available
	}

	return api.Unavailable
}
