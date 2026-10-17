package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/cluster"
)

func TestListClustersReturnsObservedHealth(t *testing.T) {
	t.Parallel()

	firstSeenAt := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	lastSeenAt := firstSeenAt.Add(time.Hour)
	controllers := &recordingClusterService{
		clusters: []cluster.Status{
			{
				Name:            "colo",
				FirstSeenAt:     firstSeenAt,
				LastSeenAt:      lastSeenAt,
				ConnectionState: cluster.ConnectionStateConnected,
			},
		},
	}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"/api/v1/clusters",
		nil,
	)
	response := httptest.NewRecorder()

	newHandler(controllers, nil, nil, nil).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}

	var body api.ClusterList
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(body.Items) != 1 {
		t.Fatalf("cluster count = %d, want 1", len(body.Items))
	}

	item := body.Items[0]
	if item.Name != "colo" {
		t.Errorf("cluster name = %q, want %q", item.Name, "colo")
	}

	if item.ConnectionState != api.Connected {
		t.Errorf(
			"connection state = %q, want %q",
			item.ConnectionState,
			api.Connected,
		)
	}

	if !item.FirstSeenAt.Equal(firstSeenAt) || !item.LastSeenAt.Equal(lastSeenAt) {
		t.Errorf(
			"observation = (%s, %s), want (%s, %s)",
			item.FirstSeenAt,
			item.LastSeenAt,
			firstSeenAt,
			lastSeenAt,
		)
	}
}

func TestListClustersReturnsProblemWhenServiceFails(t *testing.T) {
	t.Parallel()

	controllers := &recordingClusterService{
		listError: errors.New("database unavailable"),
	}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"/api/v1/clusters",
		nil,
	)
	response := httptest.NewRecorder()

	newHandler(controllers, nil, nil, nil).ServeHTTP(response, request)

	assertProblem(t, response, http.StatusInternalServerError)
	if strings.Contains(response.Body.String(), "database unavailable") {
		t.Error("internal error details were exposed in the problem response")
	}
}

func TestControllerPollAuthenticatesAndDelegates(t *testing.T) {
	t.Parallel()

	controllers := &recordingClusterService{}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/api/v1/controllers/colo/poll?timeoutSeconds=0",
		nil,
	)
	request.Header.Set("Authorization", "Bearer controller-token")
	response := httptest.NewRecorder()

	newHandler(
		controllers,
		nil,
		nil,
		map[string]string{"colo": "controller-token"},
	).ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Errorf(
			"status = %d, want %d",
			response.Code,
			http.StatusNoContent,
		)
	}

	if controllers.clusterName != "colo" {
		t.Errorf("cluster name = %q, want %q", controllers.clusterName, "colo")
	}

	if controllers.wait != 0 {
		t.Errorf("wait = %s, want 0", controllers.wait)
	}
}

func TestControllerPollUsesDefaultWait(t *testing.T) {
	t.Parallel()

	controllers := &recordingClusterService{}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/api/v1/controllers/colo/poll",
		nil,
	)
	request.Header.Set("Authorization", "Bearer controller-token")
	response := httptest.NewRecorder()

	newHandler(
		controllers,
		nil,
		nil,
		map[string]string{"colo": "controller-token"},
	).ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Errorf(
			"status = %d, want %d",
			response.Code,
			http.StatusNoContent,
		)
	}

	if controllers.wait != 25*time.Second {
		t.Errorf("wait = %s, want %s", controllers.wait, 25*time.Second)
	}
}

func TestControllerPollRejectsInvalidCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		clusterName    string
		authorizations []string
	}{
		{
			name:        "missing credential",
			clusterName: "colo",
		},
		{
			name:           "incorrect credential",
			clusterName:    "colo",
			authorizations: []string{"Bearer incorrect-token"},
		},
		{
			name:           "unknown cluster",
			clusterName:    "unknown",
			authorizations: []string{"Bearer controller-token"},
		},
		{
			name:           "incorrect scheme",
			clusterName:    "colo",
			authorizations: []string{"Basic controller-token"},
		},
		{
			name:        "multiple credentials",
			clusterName: "colo",
			authorizations: []string{
				"Bearer controller-token",
				"Bearer controller-token",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			controllers := &recordingClusterService{}
			request := httptest.NewRequestWithContext(
				context.Background(),
				http.MethodPost,
				"/api/v1/controllers/"+test.clusterName+
					"/poll?timeoutSeconds=0",
				nil,
			)
			for _, authorization := range test.authorizations {
				request.Header.Add("Authorization", authorization)
			}

			response := httptest.NewRecorder()
			newHandler(
				controllers,
				nil,
				nil,
				map[string]string{"colo": "controller-token"},
			).ServeHTTP(response, request)

			assertProblem(t, response, http.StatusUnauthorized)
			if challenge := response.Header().Get("WWW-Authenticate"); challenge != "Bearer" {
				t.Errorf(
					"WWW-Authenticate = %q, want %q",
					challenge,
					"Bearer",
				)
			}

			if controllers.calls != 0 {
				t.Errorf("work wait calls = %d, want 0", controllers.calls)
			}
		})
	}
}

func TestControllerPollRejectsInvalidTimeout(t *testing.T) {
	t.Parallel()

	controllers := &recordingClusterService{}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/api/v1/controllers/colo/poll?timeoutSeconds=31",
		nil,
	)
	request.Header.Set("Authorization", "Bearer controller-token")
	response := httptest.NewRecorder()

	newHandler(
		controllers,
		nil,
		nil,
		map[string]string{"colo": "controller-token"},
	).ServeHTTP(response, request)

	assertProblem(t, response, http.StatusBadRequest)

	if controllers.calls != 0 {
		t.Errorf("work wait calls = %d, want 0", controllers.calls)
	}
}

func TestControllerPollReturnsProblemWhenServiceFails(t *testing.T) {
	t.Parallel()

	controllers := &recordingClusterService{
		err: errors.New("database unavailable"),
	}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/api/v1/controllers/colo/poll?timeoutSeconds=0",
		nil,
	)
	request.Header.Set("Authorization", "Bearer controller-token")
	response := httptest.NewRecorder()

	newHandler(
		controllers,
		nil,
		nil,
		map[string]string{"colo": "controller-token"},
	).ServeHTTP(response, request)

	assertProblem(t, response, http.StatusInternalServerError)

	if strings.Contains(response.Body.String(), "database unavailable") {
		t.Error("internal error details were exposed in the problem response")
	}
}

type recordingClusterService struct {
	calls       int
	clusterName string
	wait        time.Duration
	err         error
	clusters    []cluster.Status
	listError   error
}

func (r *recordingClusterService) WaitForControllerWork(
	_ context.Context,
	clusterName string,
	wait time.Duration,
) error {
	r.calls++
	r.clusterName = clusterName
	r.wait = wait

	return r.err
}

func (r *recordingClusterService) ListClusters(
	context.Context,
) ([]cluster.Status, error) {
	return r.clusters, r.listError
}
