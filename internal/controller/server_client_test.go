package controller

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
)

type receivedPoll struct {
	method        string
	path          string
	timeout       string
	authorization string
}

type receivedEnrollment struct {
	method        string
	path          string
	authorization string
	body          string
}

type receivedLogChunk struct {
	method        string
	path          string
	offset        string
	contentType   string
	authorization string
	body          string
}

type httpDoerFunc func(request *http.Request) (*http.Response, error)

func (f httpDoerFunc) Do(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestServerClientEnrollsControllerCredential(t *testing.T) {
	t.Parallel()

	var enrollment receivedEnrollment
	cfg := validConfig()
	cfg.ServerURL = "https://slipramp.example.com/slipramp"
	client, err := newServerClientWithHTTPClient(
		cfg,
		httpDoerFunc(func(request *http.Request) (*http.Response, error) {
			body, readErr := io.ReadAll(request.Body)
			if readErr != nil {
				t.Fatalf("read request body: %v", readErr)
			}
			enrollment = receivedEnrollment{
				method:        request.Method,
				path:          request.URL.Path,
				authorization: request.Header.Get("Authorization"),
				body:          string(body),
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

	if err := client.Enroll(context.Background()); err != nil {
		t.Fatalf("Enroll() error = %v", err)
	}

	digest := sha256.Sum256([]byte(cfg.BearerToken))
	if enrollment.method != http.MethodPost ||
		enrollment.path != "/slipramp/controller/v1/enroll" ||
		enrollment.authorization != "Bearer enrollment-token" ||
		!strings.Contains(enrollment.body, `"clusterName":"colo"`) ||
		!strings.Contains(enrollment.body, `"credentialSha256":"`+fmt.Sprintf("%x", digest)+`"`) {
		t.Errorf("enrollment = %#v", enrollment)
	}
}

func TestServerClientPollSendsAuthenticatedRequest(t *testing.T) {
	t.Parallel()

	var poll receivedPoll

	cfg := validConfig()
	cfg.ServerURL = "https://slipramp.example.com/slipramp"
	client, err := newServerClientWithHTTPClient(
		cfg,
		httpDoerFunc(func(request *http.Request) (*http.Response, error) {
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

	assignment, err := client.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if assignment != nil {
		t.Errorf("Poll() assignment = %#v, want nil", assignment)
	}

	if poll.method != http.MethodPost {
		t.Errorf("method = %q, want %q", poll.method, http.MethodPost)
	}

	if poll.path != "/slipramp/controller/v1/poll" {
		t.Errorf("path = %q, want %q", poll.path, "/slipramp/controller/v1/poll")
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
		httpDoerFunc(func(_ *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		}),
	)
	if err != nil {
		t.Fatalf("newServerClientWithHTTPClient() error = %v", err)
	}

	_, err = client.Poll(context.Background())
	if !errors.Is(err, errControllerCredentialRejected) {
		t.Fatalf("Poll() error = %v, want %v", err, errControllerCredentialRejected)
	}
}

func TestServerClientSubmitsAuthenticatedKargoStageSnapshot(t *testing.T) {
	t.Parallel()

	var received receivedEnrollment
	cfg := validConfig()
	cfg.ServerURL = "https://slipramp.example.com/slipramp"
	client, err := newServerClientWithHTTPClient(
		cfg,
		httpDoerFunc(func(request *http.Request) (*http.Response, error) {
			body, readErr := io.ReadAll(request.Body)
			if readErr != nil {
				t.Fatalf("read request body: %v", readErr)
			}
			received = receivedEnrollment{
				method:        request.Method,
				path:          request.URL.Path,
				authorization: request.Header.Get("Authorization"),
				body:          string(body),
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

	snapshotID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1905")
	err = client.ReplaceKargoStageSnapshot(context.Background(), "prod", api.KargoStageSnapshot{
		SnapshotID: snapshotID,
		ObservedAt: time.Date(2026, time.October, 21, 12, 0, 0, 0, time.UTC),
		Stages:     []api.PromotionStageObservation{},
	})
	if err != nil {
		t.Fatalf("ReplaceKargoStageSnapshot() error = %v", err)
	}
	if received.method != http.MethodPut ||
		received.path != "/slipramp/controller/v1/kargo/prod/stages" ||
		received.authorization != "Bearer controller-token" ||
		!strings.Contains(received.body, `"snapshotId":"`+snapshotID.String()+`"`) {
		t.Errorf("snapshot request = %#v", received)
	}
}

func TestServerClientAppendsAuthenticatedRunLogChunk(t *testing.T) {
	t.Parallel()

	var chunk receivedLogChunk
	cfg := validConfig()
	cfg.ServerURL = "https://slipramp.example.com/slipramp"
	client, err := newServerClientWithHTTPClient(
		cfg,
		httpDoerFunc(func(request *http.Request) (*http.Response, error) {
			body, readErr := io.ReadAll(request.Body)
			if readErr != nil {
				t.Fatalf("read request body: %v", readErr)
			}
			chunk = receivedLogChunk{
				method:        request.Method,
				path:          request.URL.Path,
				offset:        request.URL.Query().Get("offsetBytes"),
				contentType:   request.Header.Get("Content-Type"),
				authorization: request.Header.Get("Authorization"),
				body:          string(body),
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

	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	if err := client.AppendLogChunk(context.Background(), runID, 12, []byte("hello logs\n")); err != nil {
		t.Fatalf("AppendLogChunk() error = %v", err)
	}
	if chunk.method != http.MethodPost ||
		chunk.path != "/slipramp/controller/v1/runs/"+
			runID.String()+"/log-chunks" ||
		chunk.offset != "12" ||
		chunk.contentType != "application/octet-stream" ||
		chunk.authorization != "Bearer controller-token" ||
		chunk.body != "hello logs\n" {
		t.Errorf("chunk = %#v", chunk)
	}
}

func TestServerClientReturnsSealedLogCursor(t *testing.T) {
	t.Parallel()

	digest := strings.Repeat("a", 64)
	client, err := newServerClientWithHTTPClient(
		validConfig(),
		httpDoerFunc(func(*http.Request) (*http.Response, error) {
			body := fmt.Sprintf(
				`{"nextOffsetBytes":12,"state":"sealed","sha256":%q}`,
				digest,
			)

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(body)),
			}, nil
		}),
	)
	if err != nil {
		t.Fatalf("newServerClientWithHTTPClient() error = %v", err)
	}

	cursor, err := client.GetLogCursor(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("GetLogCursor() error = %v", err)
	}
	if cursor.nextOffsetBytes != 12 || !cursor.sealed || cursor.sha256 != digest {
		t.Errorf("log cursor = %#v", cursor)
	}
}

func TestServerClientPreservesUnknownRunStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		call func(*serverClient) error
		want error
	}{
		{
			name: "log cursor",
			call: func(client *serverClient) error {
				_, err := client.GetLogCursor(context.Background(), uuid.New())

				return err
			},
			want: errRunLogRejected,
		},
		{
			name: "observation",
			call: func(client *serverClient) error {
				return client.ReportObservation(
					context.Background(),
					uuid.New(),
					api.RunObservation{Phase: api.RunObservationPhaseRunning},
				)
			},
			want: errRunObservationRejected,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			client, err := newServerClientWithHTTPClient(
				validConfig(),
				httpDoerFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusNotFound,
						Body:       io.NopCloser(strings.NewReader("")),
					}, nil
				}),
			)
			if err != nil {
				t.Fatalf("newServerClientWithHTTPClient() error = %v", err)
			}

			err = test.call(client)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
			statusCode, ok := serverResponseStatusCode(err)
			if !ok || statusCode != http.StatusNotFound {
				t.Errorf("HTTP status = %d, %t; want 404, true", statusCode, ok)
			}
		})
	}
}
