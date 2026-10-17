package controller

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type receivedPoll struct {
	method        string
	path          string
	timeout       string
	authorization string
}

type httpDoerFunc func(request *http.Request) (*http.Response, error)

func (f httpDoerFunc) Do(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestServerClientPollSendsAuthenticatedRequest(t *testing.T) {
	t.Parallel()

	var poll receivedPoll

	cfg := validConfig()
	cfg.ServerURL = "https://slipramp.example.com/slipramp"
	client, err := newServerClientWithHTTPClient(
		cfg,
		httpDoerFunc(func(
			request *http.Request,
		) (*http.Response, error) {
			poll = receivedPoll{
				method:        request.Method,
				path:          request.URL.Path,
				timeout:       request.URL.Query().Get("timeoutSeconds"),
				authorization: request.Header.Get("Authorization"),
			}

			return &http.Response{
				StatusCode: http.StatusNoContent,
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		}),
	)
	if err != nil {
		t.Fatalf("newServerClientWithHTTPClient() error = %v", err)
	}

	if err := client.Poll(context.Background()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}

	if poll.method != http.MethodPost {
		t.Errorf("method = %q, want %q", poll.method, http.MethodPost)
	}

	if poll.path != "/slipramp/api/v1/controllers/colo/poll" {
		t.Errorf(
			"path = %q, want %q",
			poll.path,
			"/slipramp/api/v1/controllers/colo/poll",
		)
	}

	if poll.timeout != "" {
		t.Errorf("timeoutSeconds = %q, want omitted", poll.timeout)
	}

	if poll.authorization != "Bearer controller-token" {
		t.Error("Authorization header does not contain the controller token")
	}
}

func TestServerClientPollRejectsUnauthorizedResponse(t *testing.T) {
	t.Parallel()

	client, err := newServerClientWithHTTPClient(
		validConfig(),
		httpDoerFunc(func(
			_ *http.Request,
		) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		}),
	)
	if err != nil {
		t.Fatalf("newServerClientWithHTTPClient() error = %v", err)
	}

	err = client.Poll(context.Background())
	if !errors.Is(err, errControllerCredentialRejected) {
		t.Fatalf(
			"Poll() error = %v, want %v",
			err,
			errControllerCredentialRejected,
		)
	}
}
