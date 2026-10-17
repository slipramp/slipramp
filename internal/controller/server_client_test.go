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

type receivedReport struct {
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
		t.Errorf(
			"path = %q, want %q",
			poll.path,
			"/slipramp/controller/v1/poll",
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

	_, err = client.Poll(context.Background())
	if !errors.Is(err, errControllerCredentialRejected) {
		t.Fatalf(
			"Poll() error = %v, want %v",
			err,
			errControllerCredentialRejected,
		)
	}
}

func TestServerClientPollParsesRunAssignment(t *testing.T) {
	t.Parallel()

	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	body := `{
		"type":"run-assignment",
		"assignment":{
		"id":"018f90ff-9fd8-7b5f-b7e1-f32ff54c1200",
		"catalogRevisionId":"018f90ff-9fd8-7b5f-b7e1-f32ff54c1201",
		"commitSha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"parameters":{},
		"repository":{"name":"ops","cloneUrl":"https://example.com/ops.git","branch":"main"},
		"target":{"name":"network"},
		"action":{"name":"sync","command":["terraform","apply"],"runtime":"terraform","placement":"admin"},
		"runtime":{"name":"terraform","image":"terraform:1"},
		"placement":{"name":"admin","cluster":"colo","jobNamespace":"slipramp-runs"}}
	}`
	client, err := newServerClientWithHTTPClient(
		validConfig(),
		httpDoerFunc(func(
			_ *http.Request,
		) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body: io.NopCloser(strings.NewReader(body)),
			}, nil
		}),
	)
	if err != nil {
		t.Fatalf("newServerClientWithHTTPClient() error = %v", err)
	}

	command, err := client.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if command == nil || command.Assignment == nil ||
		command.Assignment.ID != runID ||
		command.Assignment.Action.Name != "sync" {
		t.Fatalf("Poll() command = %#v", command)
	}
}

func TestServerClientPollParsesCancelRun(t *testing.T) {
	t.Parallel()

	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	body := `{
		"type":"cancel-run",
		"cancel":{
			"id":"018f90ff-9fd8-7b5f-b7e1-f32ff54c1200",
			"jobNamespace":"slipramp-runs",
			"jobUid":"job-uid"
		}
	}`
	client, err := newServerClientWithHTTPClient(
		validConfig(),
		httpDoerFunc(func(_ *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body: io.NopCloser(strings.NewReader(body)),
			}, nil
		}),
	)
	if err != nil {
		t.Fatalf("newServerClientWithHTTPClient() error = %v", err)
	}

	command, err := client.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if command == nil || command.Cancel == nil ||
		command.Cancel.ID != runID ||
		command.Cancel.JobNamespace != "slipramp-runs" ||
		command.Cancel.JobUID == nil ||
		*command.Cancel.JobUID != "job-uid" {
		t.Fatalf("Poll() command = %#v", command)
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
	if err := client.AppendLogChunk(
		context.Background(),
		runID,
		12,
		[]byte("hello logs\n"),
	); err != nil {
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

func TestServerClientReportsAuthenticatedRunObservation(t *testing.T) {
	t.Parallel()

	var report receivedReport
	cfg := validConfig()
	cfg.ServerURL = "https://slipramp.example.com/slipramp"
	client, err := newServerClientWithHTTPClient(
		cfg,
		httpDoerFunc(func(request *http.Request) (*http.Response, error) {
			body, readErr := io.ReadAll(request.Body)
			if readErr != nil {
				t.Fatalf("read request body: %v", readErr)
			}
			report = receivedReport{
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

	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	if err := client.ReportObservation(
		context.Background(),
		runID,
		api.RunObservation{Phase: api.RunObservationPhaseRunning},
	); err != nil {
		t.Fatalf("ReportObservation() error = %v", err)
	}
	if report.method != http.MethodPost ||
		report.path != "/slipramp/controller/v1/runs/"+
			runID.String()+"/observations" ||
		report.authorization != "Bearer controller-token" ||
		!strings.Contains(report.body, `"phase":"running"`) {
		t.Errorf("report = %#v", report)
	}
}
