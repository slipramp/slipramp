package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

type serverFailureKind string

const (
	serverFailureRetryable       serverFailureKind = "retryable"
	serverFailureAuthentication  serverFailureKind = "authentication"
	serverFailureNotFound        serverFailureKind = "not-found"
	serverFailureConflict        serverFailureKind = "conflict"
	serverFailureInvalidRequest  serverFailureKind = "invalid-request"
	serverFailureInvalidResponse serverFailureKind = "invalid-response"
	serverFailureLogFailed       serverFailureKind = "log-failed"
)

var errInvalidServerResponse = errors.New("invalid Server response")

type serverResponseError struct {
	operation  string
	statusCode int
	kind       serverFailureKind
}

func (e *serverResponseError) Error() string {
	return fmt.Sprintf("%s: %s; Server returned HTTP %d", e.operation, e.kind, e.statusCode)
}

func (e *serverResponseError) Unwrap() error {
	if e.kind == serverFailureAuthentication {
		return errControllerCredentialRejected
	}

	return nil
}

func newServerResponseError(operation string, statusCode int) error {
	kind := serverFailureInvalidResponse
	switch {
	case statusCode == http.StatusUnauthorized:
		kind = serverFailureAuthentication
	case statusCode == http.StatusRequestTimeout || statusCode == http.StatusTooManyRequests ||
		statusCode >= 500 && statusCode <= 599:
		kind = serverFailureRetryable
	case statusCode == http.StatusNotFound:
		kind = serverFailureNotFound
	case statusCode == http.StatusConflict:
		kind = serverFailureConflict
	case statusCode >= 400 && statusCode <= 499:
		kind = serverFailureInvalidRequest
	}

	return &serverResponseError{operation: operation, statusCode: statusCode, kind: kind}
}

func invalidServerResponse(operation, message string) error {
	return fmt.Errorf("%w: %s: %s", errInvalidServerResponse, operation, message)
}

func serverRequestError(operation string, err error) error {
	var syntaxError *json.SyntaxError
	var typeError *json.UnmarshalTypeError
	if errors.As(err, &syntaxError) || errors.As(err, &typeError) {
		return invalidServerResponse(operation, "response JSON does not match the API contract")
	}

	return fmt.Errorf("%s: %w", operation, err)
}

func permanentServerError(err error) bool {
	if errors.Is(err, errControllerCredentialRejected) || errors.Is(err, errInvalidServerResponse) {
		return true
	}

	var responseError *serverResponseError
	return errors.As(err, &responseError) && responseError.kind != serverFailureRetryable
}
