package gate

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.yaml.in/yaml/v3"
)

func webhookDemand() EvaluationDemand {
	subject := EvaluationSubject{
		Type: RunSubject,
		ID:   uuid.New(),
		Run: &RunEvaluationContext{
			TargetID: uuid.New(), TargetName: "production", ActionName: "apply", Intent: "reconcile",
			CatalogRevisionID: uuid.New(), CommitSHA: strings.Repeat("c", 40),
		},
	}
	return EvaluationDemand{
		RepositoryID:      uuid.New(),
		RepositoryName:    "operations",
		CatalogRevisionID: uuid.New(),
		CommitSHA:         strings.Repeat("a", 40),
		Definition:        Definition{Evaluator: &WebhookDefinition{Source: "policy", Input: map[string]any{"policy": "deploy"}}},
		DefinitionDigest:  strings.Repeat("b", 64),
		Subject:           &subject,
	}
}

func TestWebhookCoordinatorRetriesAndStopsAfterSourceRemoval(t *testing.T) {
	t.Parallel()
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			http.Error(w, "upstream failure", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"apiVersion":%q,"decision":"passing","code":"ready","message":"Ready"}`, webhookVersion)
	}))
	defer server.Close()
	d := webhookDemand()
	d.GateName = "change"
	alias := d
	alias.GateName = "also-change"
	store := &recordingEvaluationStore{targets: []EvaluationDemand{d, alias}}
	sources := SourceSet{"policy": WebhookSource{URL: server.URL, EvaluationInterval: time.Minute, Timeout: time.Second, MaximumConcurrency: 1}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	coordinator, err := NewEvaluationCoordinator(store, sources, uuid.New(), logger, nil)
	if err != nil {
		t.Fatal(err)
	}
	source := coordinator.sources["policy"]
	defer source.evaluator.Close()
	now := time.Now().UTC()
	coordinator.now = func() time.Time { return now }
	poll := func() {
		t.Helper()
		coordinator.refreshDemand(t.Context())
		if err := coordinator.reconcileSource(t.Context(), source, <-source.demand); err != nil {
			t.Fatal(err)
		}
	}
	poll()
	poll()
	if attempts.Load() != 1 || len(store.observations) != 2 || store.observations[0].Observation.State != ObservationUnavailable {
		t.Fatal("expected one failed attempt for two attachments on the same subject")
	}
	now = now.Add(2 * time.Minute)
	poll()
	if attempts.Load() != 2 || len(store.observations) != 4 || store.observations[3].Observation.State != ObservationPassing {
		t.Fatal("expected one fresh evaluation after source recovery")
	}
	restarted, err := NewEvaluationCoordinator(store, nil, uuid.New(), logger, nil)
	if err != nil {
		t.Fatal(err)
	}
	restarted.refreshDemand(t.Context())
	if len(restarted.sources) != 0 || attempts.Load() != 2 {
		t.Fatal("evaluation continued on removed source")
	}
}

func TestWebhookPostsVersionedSubjectOverTLS(t *testing.T) {
	t.Parallel()
	d := webhookDemand()
	at := time.Now().UTC()
	var prior uuid.UUID
	remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.RequestURI() != "/policy" || r.Header.Get("Authorization") != "Bearer test-token" ||
			r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Accept") != "application/json" {
			t.Error("unexpected request")
		}
		var payload webhookRequest
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload.APIVersion != webhookVersion || payload.EvaluationID == uuid.Nil || prior == payload.EvaluationID ||
			payload.EvaluationKey != string(d.Subject.Type)+"/"+d.Subject.ID.String()+"/"+d.DefinitionDigest ||
			payload.RepositoryID != d.RepositoryID || payload.RepositoryName != d.RepositoryName || payload.CatalogRevisionID != d.CatalogRevisionID ||
			payload.CommitSHA != d.CommitSHA || payload.DefinitionDigest != d.DefinitionDigest || !payload.EvaluatedAt.Equal(at) ||
			payload.Input["policy"] != "deploy" || payload.Subject == nil || payload.Subject.ID != d.Subject.ID ||
			payload.Subject.Promotion != nil || payload.Subject.Run == nil || *payload.Subject.Run != *d.Subject.Run {
			t.Error("missing or incorrect webhook request facts")
		}
		prior = payload.EvaluationID
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = fmt.Fprintf(w, `{"apiVersion":%q,"decision":"passing","code":"approved","message":"Change approved"}`, webhookVersion)
	}))
	defer remote.Close()
	dir := t.TempDir()
	tokenPath, caPath := filepath.Join(dir, "token"), filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(tokenPath, []byte("test-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cert := remote.Certificate()
	if err := os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf(`apiVersion: slipramp.com/v1alpha1
kind: GateSourceConfig
sources:
  policy:
    type: webhook
    url: %s/policy
    bearerTokenFile: %s
    tls: {caFile: %s}
`, remote.URL, tokenPath, caPath)
	sources, err := DecodeSourceFile(strings.NewReader(config))
	if err != nil {
		t.Fatal(err)
	}
	evaluator, err := sources["policy"].Open()
	if err != nil {
		t.Fatal(err)
	}
	defer evaluator.Close()
	for range 2 {
		obs := evaluator.Evaluate(t.Context(), d, at)
		if obs.State != ObservationPassing || obs.Validate() != nil || obs.Evidence.Payload.(*WebhookEvidence).Code != "approved" {
			t.Fatal(obs)
		}
	}
	if _, err := DecodeSourceFile(strings.NewReader(config + "    publicUrl: https://example.com\n")); err == nil {
		t.Fatal("accepted viewer URL for webhook")
	}
}

func TestWebhookRequiresCompletePolicyContext(t *testing.T) {
	t.Parallel()
	endpoint := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("sent an incomplete evaluation request")
	}))
	defer endpoint.Close()
	e := webhookEvaluator{client: endpoint.Client(), endpoint: endpoint.URL}
	for _, tc := range []struct {
		name string
		edit func(*EvaluationDemand)
	}{
		{"missing repository", func(d *EvaluationDemand) { d.RepositoryID = uuid.Nil }},
		{"missing active commit", func(d *EvaluationDemand) { d.CommitSHA = "" }},
		{"missing frozen commit", func(d *EvaluationDemand) { d.Subject.Run.CommitSHA = "" }},
		{"missing intent", func(d *EvaluationDemand) { d.Subject.Run.Intent = "" }},
		{"missing Stage project", func(d *EvaluationDemand) {
			d.Subject = &EvaluationSubject{
				Type: PromotionSubject, ID: uuid.New(),
				Promotion: &PromotionEvaluationContext{
					StageID: uuid.New(), Integration: "kargo", Cluster: "colo", StageName: "production",
				},
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := webhookDemand()
			tc.edit(&d)
			o := e.Evaluate(t.Context(), d, time.Now())
			if o.State != ObservationUnavailable || o.FailureClass != "context" || o.Evidence != nil {
				t.Fatalf("unexpected observation %#v", o)
			}
		})
	}
}

func TestWebhookDecisionsAndFailures(t *testing.T) {
	t.Parallel()
	valid := `{"apiVersion":"slipramp.com/gate-evaluation/v1alpha1","decision":"blocking","code":"change","message":"Change pending"}`
	for _, tc := range []struct {
		name, body, media string
		status            int
		class             string
	}{
		{"blocking", valid, "application/json", 200, ""},
		{"unavailable", strings.Replace(valid, "blocking", "unavailable", 1), "application/json", 200, "remote-unavailable"},
		{"non-2xx", "private response", "application/json", 500, "http-status"},
		{"wrong media", valid, "text/plain", 200, "media-type"},
		{"too large", strings.Repeat(" ", 4097), "application/json", 200, "size"},
		{"malformed", "{", "application/json", 200, "invalid-response"},
		{"invalid decision", strings.Replace(valid, "blocking", "approved", 1), "application/json", 200, "invalid-response"},
		{"wrong version", strings.Replace(valid, "v1alpha1", "v1", 1), "application/json", 200, "invalid-response"},
		{"trailing", valid + " {}", "application/json", 200, "invalid-response"},
		{"unknown", strings.Replace(valid, "{", `{"token":"private",`, 1), "application/json", 200, "invalid-response"},
		{"duplicate", strings.Replace(valid, "{", `{"decision":"passing",`, 1), "application/json", 200, "invalid-response"},
		{"case mismatch", strings.Replace(valid, "decision", "Decision", 1), "application/json", 200, "invalid-response"},
		{"control character", strings.Replace(valid, "Change pending", `private\u0000`, 1), "application/json", 200, "invalid-response"},
		{"code length", strings.Replace(valid, `"change"`, `"`+strings.Repeat("c", 65)+`"`, 1), "application/json", 200, "invalid-response"},
		{"message length", strings.Replace(valid, "Change pending", strings.Repeat("m", 257), 1), "application/json", 200, "invalid-response"},
		{"echoed credential", strings.Replace(valid, "Change pending", "exposed private-token", 1), "application/json", 200, "invalid-response"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tc.media)
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			e := webhookEvaluator{client: server.Client(), endpoint: server.URL, token: "private-token"}
			obs := e.Evaluate(t.Context(), webhookDemand(), time.Now())
			if obs.Validate() != nil || obs.FailureClass != tc.class || strings.Contains(obs.Diagnostic, "private") {
				t.Fatal(obs)
			}
			if tc.class == "" && obs.State != ObservationBlocking {
				t.Fatal(obs)
			}
			if tc.class != "" && obs.State != ObservationUnavailable {
				t.Fatal(obs)
			}
			if tc.class == "remote-unavailable" && obs.Diagnostic != "change: Change pending" {
				t.Fatal(obs)
			}
		})
	}
}

func TestWebhookRejectsRedirectAndTimesOut(t *testing.T) {
	t.Parallel()
	var redirected atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected.Store(true) }))
	defer destination.Close()
	timedOut := false
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/timeout" {
			_, _ = io.Copy(io.Discard, r.Body)
			<-r.Context().Done()
			return
		}
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer endpoint.Close()
	for _, path := range []string{"/redirect", "/timeout"} {
		source := WebhookSource{HTTPSource{URL: endpoint.URL + path, EvaluationInterval: time.Second, Timeout: 100 * time.Millisecond, MaximumConcurrency: 1}}
		e, err := source.Open()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		obs := e.Evaluate(ctx, webhookDemand(), time.Now())
		cancel()
		e.Close()
		if obs.State != ObservationUnavailable {
			t.Fatal(obs)
		}
		timedOut = obs.FailureClass == "timeout"
	}
	if redirected.Load() || !timedOut {
		t.Fatalf("redirect followed %v, timed out %v", redirected.Load(), timedOut)
	}
}

func TestWebhookDefinitionBoundsAndDigest(t *testing.T) {
	t.Parallel()
	decode := func(t *testing.T, input string) (Definition, error) {
		t.Helper()
		var def Definition
		if err := yaml.Unmarshal([]byte("evaluator: {type: webhook, source: approval, input: "+input+"}"), &def); err != nil {
			return def, err
		}
		return def, def.Validate()
	}
	for _, input := range []string{"null", "[1]", "{v: .inf}", "{v: 9007199254740993}",
		"{v: " + strings.Repeat("x", 1025) + "}", "{" + strings.Repeat("x", 65) + ": true}",
		"{v: " + strings.Repeat("[", 8) + "1" + strings.Repeat("]", 8) + "}",
		"{v: [" + strings.Repeat(strings.Repeat("x", 1000)+",", 9) + "]}",
	} {
		if _, err := decode(t, input); err == nil {
			t.Fatal("accepted out-of-bounds input")
		}
	}
	many := map[string]any{}
	for i := range 65 {
		many[fmt.Sprint(i)] = true
	}
	if err := validateWebhookInput(many); err == nil {
		t.Fatal("accepted excessive property count")
	}
	digest := func(input string) string {
		def, err := decode(t, input)
		if err != nil {
			t.Fatal(err)
		}
		hash, err := def.Digest()
		if err != nil {
			t.Fatal(err)
		}
		return hash
	}
	if digest("{a: [null, true, 1.5], b: deploy}") != digest("{b: deploy, a: [null, true, 1.5]}") ||
		digest("{mode: apply}") == digest("{mode: preview}") {
		t.Fatal("webhook digest did not capture normalized input")
	}
}
