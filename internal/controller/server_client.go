package controller

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
)

const pollRequestTimeout = 35 * time.Second

type controllerAPI interface {
	EnrollControllerWithResponse(
		ctx context.Context,
		body api.EnrollControllerJSONRequestBody,
		reqEditors ...api.RequestEditorFn,
	) (*api.EnrollControllerResponse, error)
	PollForControllerCommandWithResponse(
		ctx context.Context,
		params *api.PollForControllerCommandParams,
		reqEditors ...api.RequestEditorFn,
	) (*api.PollForControllerCommandResponse, error)
	AppendRunLogChunkWithBodyWithResponse(
		ctx context.Context,
		runID uuid.UUID,
		params *api.AppendRunLogChunkParams,
		contentType string,
		body io.Reader,
		reqEditors ...api.RequestEditorFn,
	) (*api.AppendRunLogChunkResponse, error)
	GetRunLogCursorWithResponse(
		ctx context.Context,
		runID uuid.UUID,
		reqEditors ...api.RequestEditorFn,
	) (*api.GetRunLogCursorResponse, error)
	FailRunLogWithResponse(
		ctx context.Context,
		runID uuid.UUID,
		reqEditors ...api.RequestEditorFn,
	) (*api.FailRunLogResponse, error)
	SealRunLogWithResponse(
		ctx context.Context,
		runID uuid.UUID,
		body api.SealRunLogJSONRequestBody,
		reqEditors ...api.RequestEditorFn,
	) (*api.SealRunLogResponse, error)
	ReportRunObservationWithResponse(
		ctx context.Context,
		runID uuid.UUID,
		body api.ReportRunObservationJSONRequestBody,
		reqEditors ...api.RequestEditorFn,
	) (*api.ReportRunObservationResponse, error)
	ReportKargoPromotionDispatchWithResponse(
		ctx context.Context,
		promotionID uuid.UUID,
		body api.ReportKargoPromotionDispatchJSONRequestBody,
		reqEditors ...api.RequestEditorFn,
	) (*api.ReportKargoPromotionDispatchResponse, error)
	ReportKargoPromotionReconciliationWithResponse(
		ctx context.Context,
		promotionID uuid.UUID,
		body api.ReportKargoPromotionReconciliationJSONRequestBody,
		reqEditors ...api.RequestEditorFn,
	) (*api.ReportKargoPromotionReconciliationResponse, error)
	ReplaceKargoStageSnapshotWithResponse(
		ctx context.Context,
		integration api.CatalogName,
		body api.ReplaceKargoStageSnapshotJSONRequestBody,
		reqEditors ...api.RequestEditorFn,
	) (*api.ReplaceKargoStageSnapshotResponse, error)
}

type serverClient struct {
	clusterName     string
	bearerToken     string
	enrollmentToken string
	api             controllerAPI
}

func (c *serverClient) GetLogCursor(ctx context.Context, runID uuid.UUID) (logCursor, error) {
	response, err := c.api.GetRunLogCursorWithResponse(ctx, runID, controllerAuthorization(c.bearerToken))
	if err != nil {
		return logCursor{}, serverRequestError("get Run log cursor", err)
	}

	switch response.StatusCode() {
	case http.StatusOK:
		if response.JSON200 == nil {
			return logCursor{}, invalidServerResponse("get Run log cursor", "response is empty")
		}
		cursor := response.JSON200
		if cursor.NextOffsetBytes < 0 || !cursor.State.Valid() ||
			(cursor.State == api.LogCursorStateSealed) != (valueOrEmpty(cursor.Sha256) != "") {
			return logCursor{}, invalidServerResponse("get Run log cursor", "cursor state is invalid")
		}
		return logCursor{
			nextOffsetBytes: response.JSON200.NextOffsetBytes,
			sealed:          response.JSON200.State == api.LogCursorStateSealed,
			failed:          response.JSON200.State == api.LogCursorStateFailed,
			sha256:          valueOrEmpty(response.JSON200.Sha256),
		}, nil
	default:
		return logCursor{}, newServerResponseError("get Run log cursor", response.StatusCode())
	}
}

func (c *serverClient) FailLog(ctx context.Context, runID uuid.UUID) error {
	response, err := c.api.FailRunLogWithResponse(ctx, runID, controllerAuthorization(c.bearerToken))
	if err != nil {
		return serverRequestError("fail Run log", err)
	}
	if response.StatusCode() != http.StatusNoContent {
		return newServerResponseError("fail Run log", response.StatusCode())
	}

	return nil
}

func (c *serverClient) SealLog(ctx context.Context, runID uuid.UUID, sizeBytes int64, digest string) error {
	response, err := c.api.SealRunLogWithResponse(
		ctx,
		runID,
		api.LogSeal{SizeBytes: sizeBytes, Sha256: digest},
		controllerAuthorization(c.bearerToken),
	)
	if err != nil {
		return serverRequestError("seal Run log", err)
	}

	switch response.StatusCode() {
	case http.StatusNoContent:
		return nil
	default:
		return newServerResponseError("seal Run log", response.StatusCode())
	}
}

func (c *serverClient) AppendLogChunk(ctx context.Context, runID uuid.UUID, offsetBytes int64, contents []byte) error {
	response, err := c.api.AppendRunLogChunkWithBodyWithResponse(
		ctx,
		runID,
		&api.AppendRunLogChunkParams{OffsetBytes: offsetBytes},
		"application/octet-stream",
		bytes.NewReader(contents),
		controllerAuthorization(c.bearerToken),
	)
	if err != nil {
		return serverRequestError("append Run log chunk", err)
	}

	switch response.StatusCode() {
	case http.StatusNoContent:
		return nil
	default:
		return newServerResponseError("append Run log chunk", response.StatusCode())
	}
}

func newServerClient(cfg Config) (*serverClient, error) {
	httpClient := &http.Client{Timeout: pollRequestTimeout}

	return newServerClientWithHTTPClient(cfg, httpClient)
}

func newServerClientWithHTTPClient(cfg Config, httpClient api.HttpRequestDoer) (*serverClient, error) {
	client, err := api.NewClientWithResponses(cfg.ServerURL, api.WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("create Server API client: %w", err)
	}

	return &serverClient{
		clusterName:     cfg.ClusterName,
		bearerToken:     cfg.BearerToken,
		enrollmentToken: cfg.EnrollmentToken,
		api:             client,
	}, nil
}

func (c *serverClient) Enroll(ctx context.Context) error {
	digest := sha256.Sum256([]byte(c.bearerToken))
	response, err := c.api.EnrollControllerWithResponse(
		ctx,
		api.ControllerEnrollment{
			ClusterName:      c.clusterName,
			CredentialSha256: hex.EncodeToString(digest[:]),
		},
		controllerAuthorization(c.enrollmentToken),
	)
	if err != nil {
		return serverRequestError("enroll Controller", err)
	}

	switch response.StatusCode() {
	case http.StatusNoContent:
		return nil
	case http.StatusUnauthorized:
		return errControllerEnrollmentCredentialRejected
	case http.StatusConflict:
		return errControllerEnrollmentConflict
	default:
		return newServerResponseError("enroll Controller", response.StatusCode())
	}
}

func (c *serverClient) Poll(ctx context.Context) (*api.ControllerCommand, error) {
	response, err := c.api.PollForControllerCommandWithResponse(ctx, nil, controllerAuthorization(c.bearerToken))
	if err != nil {
		return nil, serverRequestError("poll Controller command", err)
	}

	switch response.StatusCode() {
	case http.StatusOK:
		if response.JSON200 == nil {
			return nil, invalidServerResponse("poll Controller command", "response is empty")
		}
		return response.JSON200, nil
	case http.StatusNoContent:
		return nil, nil
	default:
		return nil, newServerResponseError("poll Controller command", response.StatusCode())
	}
}

func (c *serverClient) ReportObservation(ctx context.Context, runID uuid.UUID, observation api.RunObservation) error {
	response, err := c.api.ReportRunObservationWithResponse(
		ctx,
		runID,
		observation,
		controllerAuthorization(c.bearerToken),
	)
	if err != nil {
		return serverRequestError("report Run observation", err)
	}

	switch response.StatusCode() {
	case http.StatusNoContent:
		return nil
	default:
		return newServerResponseError("report Run observation", response.StatusCode())
	}
}

func (c *serverClient) ReportKargoPromotionDispatch(
	ctx context.Context,
	promotionID uuid.UUID,
	report api.KargoPromotionDispatchReport,
) error {
	response, err := c.api.ReportKargoPromotionDispatchWithResponse(
		ctx,
		promotionID,
		report,
		controllerAuthorization(c.bearerToken),
	)
	if err != nil {
		return serverRequestError("report Kargo Promotion dispatch", err)
	}

	switch response.StatusCode() {
	case http.StatusNoContent:
		return nil
	default:
		return newServerResponseError("report Kargo Promotion dispatch", response.StatusCode())
	}
}

func (c *serverClient) ReportKargoPromotionReconciliation(
	ctx context.Context,
	promotionID uuid.UUID,
	report api.KargoPromotionReconciliationReport,
) error {
	response, err := c.api.ReportKargoPromotionReconciliationWithResponse(
		ctx,
		promotionID,
		report,
		controllerAuthorization(c.bearerToken),
	)
	if err != nil {
		return serverRequestError("report Kargo Promotion reconciliation", err)
	}

	switch response.StatusCode() {
	case http.StatusNoContent:
		return nil
	default:
		return newServerResponseError("report Kargo Promotion reconciliation", response.StatusCode())
	}
}

func (c *serverClient) ReplaceKargoStageSnapshot(
	ctx context.Context,
	integration string,
	snapshot api.KargoStageSnapshot,
) error {
	response, err := c.api.ReplaceKargoStageSnapshotWithResponse(
		ctx,
		api.CatalogName(integration),
		snapshot,
		controllerAuthorization(c.bearerToken),
	)
	if err != nil {
		return serverRequestError("replace Kargo Stage snapshot", err)
	}

	switch response.StatusCode() {
	case http.StatusNoContent:
		return nil
	default:
		return newServerResponseError("replace Kargo Stage snapshot", response.StatusCode())
	}
}

func controllerAuthorization(bearerToken string) api.RequestEditorFn {
	return func(_ context.Context, request *http.Request) error {
		request.Header.Set("Authorization", "Bearer "+bearerToken)

		return nil
	}
}
