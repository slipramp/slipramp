package gate_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/slipramp/slipramp/internal/gate"
)

func TestPrometheusEvaluator(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		statusCode     int
		response       string
		wantState      gate.ObservationState
		wantValue      *float64
		wantDiagnostic string
	}{
		{
			name:      "passing scalar",
			response:  `{"status":"success","data":{"resultType":"scalar","result":[1730000000,"0"]}}`,
			wantState: gate.ObservationPassing,
			wantValue: new(0.0),
		},
		{
			name:      "blocking vector",
			response:  `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"secret":"not exposed"},"value":[1730000000,"2"]}]}}`,
			wantState: gate.ObservationBlocking,
			wantValue: new(2.0),
		},
		{
			name:           "empty vector",
			response:       `{"status":"success","data":{"resultType":"vector","result":[]}}`,
			wantState:      gate.ObservationUnavailable,
			wantDiagnostic: "exactly one sample",
		},
		{
			name:           "multiple vector samples",
			response:       `{"status":"success","data":{"resultType":"vector","result":[{"value":[1,"0"]},{"value":[1,"0"]}]}}`,
			wantState:      gate.ObservationUnavailable,
			wantDiagnostic: "exactly one sample",
		},
		{
			name:           "matrix",
			response:       `{"status":"success","data":{"resultType":"matrix","result":[]}}`,
			wantState:      gate.ObservationUnavailable,
			wantDiagnostic: "unsupported",
		},
		{
			name:           "null sample timestamp",
			response:       `{"status":"success","data":{"resultType":"scalar","result":[null,"0"]}}`,
			wantState:      gate.ObservationUnavailable,
			wantDiagnostic: "scalar result was invalid",
		},
		{
			name:           "oversized response",
			response:       strings.Repeat("x", gate.MaximumPrometheusResponseSize+1),
			wantState:      gate.ObservationUnavailable,
			wantDiagnostic: "size limit",
		},
		{
			name:           "non-finite value",
			response:       `{"status":"success","data":{"resultType":"scalar","result":[1730000000,"NaN"]}}`,
			wantState:      gate.ObservationUnavailable,
			wantDiagnostic: "scalar result was invalid",
		},
		{
			name:           "unsuccessful response",
			response:       `{"status":"error","errorType":"bad_data","error":"repository-controlled text"}`,
			wantState:      gate.ObservationUnavailable,
			wantDiagnostic: "query was unsuccessful",
		},
		{
			name:           "malformed response",
			response:       `{`,
			wantState:      gate.ObservationUnavailable,
			wantDiagnostic: "response was malformed",
		},
		{
			name:           "source response",
			statusCode:     http.StatusServiceUnavailable,
			response:       "repository-controlled text",
			wantState:      gate.ObservationUnavailable,
			wantDiagnostic: "HTTP 503",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			evaluationTime := time.Date(2026, time.November, 19, 12, 34, 56, 123, time.FixedZone("offset", -5*60*60))
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.Method != http.MethodGet || request.URL.Path != "/api/v1/query" {
					t.Errorf("request = %s %s", request.Method, request.URL.Path)
				}
				if got := request.URL.Query().Get("query"); got != "active_sessions" {
					t.Errorf("query = %q", got)
				}
				if got := request.URL.Query().Get("time"); got != evaluationTime.UTC().Format(time.RFC3339Nano) {
					t.Errorf("time = %q", got)
				}
				statusCode := test.statusCode
				if statusCode == 0 {
					statusCode = http.StatusOK
				}
				writer.WriteHeader(statusCode)
				_, _ = writer.Write([]byte(test.response))
			}))
			defer server.Close()

			evaluator := newTestPrometheusEvaluator(t, server.URL)
			observation := evaluator.Evaluate(context.Background(), validDefinition(), evaluationTime)
			if observation.State != test.wantState {
				t.Errorf("State = %q, want %q", observation.State, test.wantState)
			}
			if test.wantValue == nil {
				if observation.Value != nil {
					t.Errorf("Value = %v, want nil", *observation.Value)
				}
			} else if observation.Value == nil || *observation.Value != *test.wantValue {
				t.Errorf("Value = %v, want %v", observation.Value, *test.wantValue)
			}
			if !strings.Contains(observation.Diagnostic, test.wantDiagnostic) {
				t.Errorf("Diagnostic = %q, want phrase %q", observation.Diagnostic, test.wantDiagnostic)
			}
			if strings.Contains(observation.Diagnostic, "repository-controlled") ||
				strings.Contains(observation.Diagnostic, "secret") {
				t.Errorf("Diagnostic exposes upstream contents: %q", observation.Diagnostic)
			}
		})
	}
}

func TestPrometheusEvaluatorUsesConfiguredPathAndBearerToken(t *testing.T) {
	t.Parallel()

	tokenPath := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenPath, []byte("prometheus-token\n"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/prometheus/api/v1/query" {
			t.Errorf("path = %q", request.URL.Path)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer prometheus-token" {
			t.Errorf("Authorization = %q", got)
		}
		_, _ = writer.Write([]byte(
			`{"status":"success","data":{"resultType":"scalar","result":[1730000000,"0"]}}`,
		))
	}))
	defer server.Close()

	source := validPrometheusSource(server.URL + "/prometheus")
	source.BearerTokenFile = tokenPath
	evaluator, err := gate.NewPrometheusEvaluator(source)
	if err != nil {
		t.Fatalf("NewPrometheusEvaluator() error = %v", err)
	}
	observation := evaluator.Evaluate(context.Background(), validDefinition(), time.Now())
	if observation.State != gate.ObservationPassing {
		t.Errorf("observation = %#v", observation)
	}
}

func TestPrometheusEvaluatorRejectsInvalidBearerTokenFile(t *testing.T) {
	t.Parallel()

	tokenPath := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenPath, []byte("secret token contents"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	source := validPrometheusSource("https://prometheus.example.com")
	source.BearerTokenFile = tokenPath
	_, err := gate.NewPrometheusEvaluator(source)
	if err == nil || !strings.Contains(err.Error(), "invalid token") || strings.Contains(err.Error(), "secret token contents") {
		t.Fatalf("NewPrometheusEvaluator() error = %v", err)
	}
}

func newTestPrometheusEvaluator(t *testing.T, sourceURL string) *gate.PrometheusEvaluator {
	t.Helper()

	evaluator, err := gate.NewPrometheusEvaluator(validPrometheusSource(sourceURL))
	if err != nil {
		t.Fatalf("NewPrometheusEvaluator() error = %v", err)
	}

	return evaluator
}

func validPrometheusSource(sourceURL string) gate.PrometheusSource {
	return gate.PrometheusSource{
		URL:                sourceURL,
		EvaluationInterval: time.Minute,
		Timeout:            5 * time.Second,
		MaximumConcurrency: 1,
	}
}

func TestPrometheusQueryURL(t *testing.T) {
	t.Parallel()

	sources := gate.SourceSet{
		"prod": {Prometheus: gate.PrometheusSource{
			URL: "http://internal:9090", PublicURL: "https://metrics.example.com/tenant/prod/",
		}},
		"internal": {Prometheus: gate.PrometheusSource{URL: "http://internal:9090"}},
	}
	const expr = `sum(rate(http_requests_total{job=~"api|web",route="/a&b"}[5m])) + 1`
	def := &gate.PrometheusDefinition{Source: "prod", Query: expr}
	at := time.Date(2026, 11, 20, 10, 30, 12, 345678000, time.FixedZone("MST", -7*60*60))
	const utc = "2026-11-20T17:30:12.345Z"
	override := gate.Requirement{
		State: gate.RequirementReleased, Prometheus: def, DefinitionDigest: "release",
		Overrides: []gate.Override{{DefinitionDigest: "release", EvaluatedAt: &at}},
	}
	otherDigest := override
	otherDigest.DefinitionDigest = "other"
	for _, tc := range []struct {
		name       string
		req        gate.Requirement
		wantMoment string
		wantLink   bool
	}{
		{"observation", gate.Requirement{State: gate.RequirementBlocking, Prometheus: def, EvaluatedAt: &at}, utc, true},
		{"waiting for observation", gate.Requirement{State: gate.RequirementUnevaluated, Prometheus: def}, "", true},
		{"released", gate.Requirement{State: gate.RequirementReleased, Prometheus: def, EvaluatedAt: &at}, utc, true},
		{"release by override", override, utc, true},
		{"different override", otherDigest, "", true},
		{"stopped", gate.Requirement{State: gate.RequirementStopped, Prometheus: def}, "", false},
		{"missing definition", gate.Requirement{}, "", false},
		{"missing source", gate.Requirement{Prometheus: &gate.PrometheusDefinition{Source: "removed", Query: expr}}, "", false},
		{"private source", gate.Requirement{Prometheus: &gate.PrometheusDefinition{Source: "internal", Query: expr}}, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			link := sources.QueryURL(tc.req)
			if !tc.wantLink {
				if link != nil {
					t.Errorf("unexpected viewer link %q", *link)
				}
				return
			}
			if link == nil {
				t.Fatal("missing viewer link")
			}
			viewer, err := url.Parse(*link)
			if err != nil {
				t.Fatal(err)
			}
			if viewer.Scheme != "https" || viewer.Host != "metrics.example.com" || viewer.Path != "/tenant/prod/query" {
				t.Errorf("viewer location = %v", viewer)
			}
			params := viewer.Query()
			if params.Get("g0.expr") != expr || params.Get("g0.tab") != "table" ||
				params.Get("g0.moment_input") != tc.wantMoment || params.Has("g0.moment_input") != (tc.wantMoment != "") {
				t.Errorf("viewer parameters = %v", params)
			}
		})
	}
}
