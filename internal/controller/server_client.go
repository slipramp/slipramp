package controller

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/slipramp/slipramp/internal/api"
)

const pollRequestTimeout = 35 * time.Second

type controllerAPI interface {
	PollForRunAssignmentWithResponse(
		ctx context.Context,
		clusterName string,
		params *api.PollForRunAssignmentParams,
		reqEditors ...api.RequestEditorFn,
	) (*api.PollForRunAssignmentResponse, error)
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

func (c *serverClient) Poll(ctx context.Context) (*api.RunAssignment, error) {
	response, err := c.api.PollForRunAssignmentWithResponse(
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
			return nil, fmt.Errorf("controller poll returned an empty assignment response")
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

func controllerAuthorization(bearerToken string) api.RequestEditorFn {
	return func(_ context.Context, request *http.Request) error {
		request.Header.Set("Authorization", "Bearer "+bearerToken)

		return nil
	}
}
