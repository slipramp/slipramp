package controller

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
)

const pollRequestTimeout = 35 * time.Second

type controllerAPI interface {
	PollForControllerCommandWithResponse(
		ctx context.Context,
		clusterName string,
		params *api.PollForControllerCommandParams,
		reqEditors ...api.RequestEditorFn,
	) (*api.PollForControllerCommandResponse, error)
	ReportRunObservationWithResponse(
		ctx context.Context,
		clusterName string,
		runID uuid.UUID,
		body api.ReportRunObservationJSONRequestBody,
		reqEditors ...api.RequestEditorFn,
	) (*api.ReportRunObservationResponse, error)
}

type serverClient struct {
	clusterName string
	api         controllerAPI
}

func newServerClient(cfg Config) (*serverClient, error) {
	httpClient := &http.Client{Timeout: pollRequestTimeout}

	return newServerClientWithHTTPClient(cfg, httpClient)
}

func newServerClientWithHTTPClient(
	cfg Config,
	httpClient api.HttpRequestDoer,
) (*serverClient, error) {
	client, err := api.NewClientWithResponses(
		cfg.ServerURL,
		api.WithHTTPClient(httpClient),
		api.WithRequestEditorFn(controllerAuthorization(cfg.BearerToken)),
	)
	if err != nil {
		return nil, fmt.Errorf("create Server API client: %w", err)
	}

	return &serverClient{
		clusterName: cfg.ClusterName,
		api:         client,
	}, nil
}

func (c *serverClient) Poll(ctx context.Context) (*api.ControllerCommand, error) {
	response, err := c.api.PollForControllerCommandWithResponse(
		ctx,
		c.clusterName,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("poll Server for run assignment: %w", err)
	}

	switch response.StatusCode() {
	case http.StatusOK:
		if response.JSON200 == nil {
			return nil, fmt.Errorf("controller poll returned an empty command response")
		}

		return response.JSON200, nil
	case http.StatusNoContent:
		return nil, nil
	case http.StatusUnauthorized:
		return nil, errControllerCredentialRejected
	default:
		return nil, fmt.Errorf(
			"controller poll returned HTTP %d",
			response.StatusCode(),
		)
	}
}

func (c *serverClient) ReportObservation(
	ctx context.Context,
	runID uuid.UUID,
	observation api.RunObservation,
) error {
	response, err := c.api.ReportRunObservationWithResponse(
		ctx,
		c.clusterName,
		runID,
		observation,
	)
	if err != nil {
		return fmt.Errorf("report Run observation to Server: %w", err)
	}

	switch response.StatusCode() {
	case http.StatusNoContent:
		return nil
	case http.StatusUnauthorized:
		return errControllerCredentialRejected
	case http.StatusNotFound, http.StatusConflict:
		return fmt.Errorf(
			"%w: Server returned HTTP %d",
			errRunObservationRejected,
			response.StatusCode(),
		)
	default:
		return fmt.Errorf(
			"report Run observation returned HTTP %d",
			response.StatusCode(),
		)
	}
}

func controllerAuthorization(bearerToken string) api.RequestEditorFn {
	return func(_ context.Context, request *http.Request) error {
		request.Header.Set("Authorization", "Bearer "+bearerToken)

		return nil
	}
}
