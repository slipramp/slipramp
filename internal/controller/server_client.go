package controller

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
)

const pollRequestTimeout = 35 * time.Second

type serverResponseError struct {
	operation  string
	statusCode int
	err        error
}

func (e *serverResponseError) Error() string {
	if e.err == nil {
		return fmt.Sprintf("%s returned HTTP %d", e.operation, e.statusCode)
	}

	return fmt.Sprintf("%s: %v; Server returned HTTP %d", e.operation, e.err, e.statusCode)
}

func (e *serverResponseError) Unwrap() error { return e.err }

func serverResponseStatusCode(err error) (int, bool) {
	var responseErr *serverResponseError
	if !errors.As(err, &responseErr) {
		return 0, false
	}

	return responseErr.statusCode, true
}

func rejectedServerResponse(operation string, statusCode int, err error) error {
	return &serverResponseError{
		operation:  operation,
		statusCode: statusCode,
		err:        err,
	}
}

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
	ReplaceKargoStageSnapshotWithResponse(
		ctx context.Context,
		integration api.CatalogName,
		body api.ReplaceKargoStageSnapshotJSONRequestBody,
		reqEditors ...api.RequestEditorFn,
	) (*api.ReplaceKargoStageSnapshotResponse, error)
}

func (c *serverClient) GetLogCursor(ctx context.Context, runID uuid.UUID) (logCursor, error) {
	response, err := c.api.GetRunLogCursorWithResponse(ctx, runID, controllerAuthorization(c.bearerToken))
	if err != nil {
		return logCursor{}, fmt.Errorf("get Run log cursor: %w", err)
	}

	switch response.StatusCode() {
	case http.StatusOK:
		if response.JSON200 == nil {
			return logCursor{}, errors.New("Run log cursor response is empty")
		}
		return logCursor{
			nextOffsetBytes: response.JSON200.NextOffsetBytes,
			sealed:          response.JSON200.State == api.LogCursorStateSealed,
			sha256:          valueOrEmpty(response.JSON200.Sha256),
		}, nil
	case http.StatusUnauthorized:
		return logCursor{}, errControllerCredentialRejected
	case http.StatusNotFound:
		return logCursor{}, rejectedServerResponse("get Run log cursor", response.StatusCode(), errRunLogRejected)
	default:
		return logCursor{}, rejectedServerResponse("get Run log cursor", response.StatusCode(), nil)
	}
}

func (c *serverClient) SealLog(ctx context.Context, runID uuid.UUID, sizeBytes int64, digest string) error {
	response, err := c.api.SealRunLogWithResponse(
		ctx,
		runID,
		api.LogSeal{SizeBytes: sizeBytes, Sha256: digest},
		controllerAuthorization(c.bearerToken),
	)
	if err != nil {
		return fmt.Errorf("seal Run log: %w", err)
	}

	switch response.StatusCode() {
	case http.StatusNoContent:
		return nil
	case http.StatusUnauthorized:
		return errControllerCredentialRejected
	case http.StatusBadRequest, http.StatusNotFound, http.StatusConflict:
		return rejectedServerResponse("seal Run log", response.StatusCode(), errRunLogRejected)
	default:
		return rejectedServerResponse("seal Run log", response.StatusCode(), nil)
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
		return fmt.Errorf("append Run log chunk: %w", err)
	}

	switch response.StatusCode() {
	case http.StatusNoContent:
		return nil
	case http.StatusUnauthorized:
		return errControllerCredentialRejected
	case http.StatusBadRequest,
		http.StatusNotFound,
		http.StatusConflict,
		http.StatusRequestEntityTooLarge:
		return rejectedServerResponse("append Run log chunk", response.StatusCode(), errRunLogRejected)
	default:
		return rejectedServerResponse("append Run log chunk", response.StatusCode(), nil)
	}
}

type serverClient struct {
	clusterName     string
	bearerToken     string
	enrollmentToken string
	api             controllerAPI
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
		return fmt.Errorf("enroll controller with Server: %w", err)
	}

	switch response.StatusCode() {
	case http.StatusNoContent:
		return nil
	case http.StatusUnauthorized:
		return errControllerEnrollmentCredentialRejected
	case http.StatusConflict:
		return errControllerEnrollmentConflict
	default:
		return fmt.Errorf("controller enrollment returned HTTP %d", response.StatusCode())
	}
}

func (c *serverClient) Poll(ctx context.Context) (*api.ControllerCommand, error) {
	response, err := c.api.PollForControllerCommandWithResponse(ctx, nil, controllerAuthorization(c.bearerToken))
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
		return nil, fmt.Errorf("controller poll returned HTTP %d", response.StatusCode())
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
		return fmt.Errorf("report Run observation to Server: %w", err)
	}

	switch response.StatusCode() {
	case http.StatusNoContent:
		return nil
	case http.StatusUnauthorized:
		return errControllerCredentialRejected
	case http.StatusNotFound, http.StatusConflict:
		return rejectedServerResponse("report Run observation", response.StatusCode(), errRunObservationRejected)
	default:
		return rejectedServerResponse("report Run observation", response.StatusCode(), nil)
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
		return fmt.Errorf("report Kargo Promotion dispatch: %w", err)
	}

	switch response.StatusCode() {
	case http.StatusNoContent:
		return nil
	case http.StatusUnauthorized:
		return errControllerCredentialRejected
	case http.StatusBadRequest, http.StatusNotFound, http.StatusConflict:
		return rejectedServerResponse(
			"report Kargo Promotion dispatch",
			response.StatusCode(),
			errKargoPromotionDispatchRejected,
		)
	default:
		return rejectedServerResponse("report Kargo Promotion dispatch", response.StatusCode(), nil)
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
		return fmt.Errorf("replace Kargo Stage snapshot: %w", err)
	}

	switch response.StatusCode() {
	case http.StatusNoContent:
		return nil
	case http.StatusUnauthorized:
		return errControllerCredentialRejected
	case http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound, http.StatusConflict:
		return rejectedServerResponse("replace Kargo Stage snapshot", response.StatusCode(), nil)
	default:
		return rejectedServerResponse("replace Kargo Stage snapshot", response.StatusCode(), nil)
	}
}

func controllerAuthorization(bearerToken string) api.RequestEditorFn {
	return func(_ context.Context, request *http.Request) error {
		request.Header.Set("Authorization", "Bearer "+bearerToken)

		return nil
	}
}
