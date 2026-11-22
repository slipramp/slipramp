package gate

import (
	"bytes"
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"mime"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

const webhookVersion = "slipramp.com/gate-evaluation/v1alpha1"

// EvaluationSubject contains only the frozen facts an external policy may read.
type EvaluationSubject struct {
	Type      SubjectType                 `json:"type"`
	ID        uuid.UUID                   `json:"id"`
	Run       *RunEvaluationContext       `json:"run,omitempty"`
	Promotion *PromotionEvaluationContext `json:"promotion,omitempty"`
}

func (s *EvaluationSubject) valid() bool {
	if s == nil || s.ID == uuid.Nil {
		return false
	}
	switch s.Type {
	case RunSubject:
		r := s.Run
		return r != nil && s.Promotion == nil && r.TargetID != uuid.Nil && r.TargetName != "" &&
			r.ActionName != "" && r.Intent != "" && r.CatalogRevisionID != uuid.Nil && r.CommitSHA != ""
	case PromotionSubject:
		p := s.Promotion
		return p != nil && s.Run == nil && p.StageID != uuid.Nil && p.Integration != "" &&
			p.Cluster != "" && p.Project != "" && p.StageName != ""
	default:
		return false
	}
}

type RunEvaluationContext struct {
	TargetID          uuid.UUID `json:"targetId"`
	TargetName        string    `json:"targetName"`
	ActionName        string    `json:"actionName"`
	Intent            string    `json:"intent"`
	CatalogRevisionID uuid.UUID `json:"catalogRevisionId"`
	CommitSHA         string    `json:"commitSha"`
}

type PromotionEvaluationContext struct {
	StageID     uuid.UUID `json:"stageId"`
	Integration string    `json:"integration"`
	Cluster     string    `json:"cluster"`
	Project     string    `json:"project"`
	StageName   string    `json:"stageName"`
}

type webhookRequest struct {
	APIVersion        string             `json:"apiVersion"`
	EvaluationID      uuid.UUID          `json:"evaluationId"`
	EvaluationKey     string             `json:"evaluationKey"`
	EvaluatedAt       time.Time          `json:"evaluatedAt"`
	Subject           *EvaluationSubject `json:"subject"`
	RepositoryID      uuid.UUID          `json:"repositoryId"`
	RepositoryName    string             `json:"repositoryName"`
	CatalogRevisionID uuid.UUID          `json:"catalogRevisionId"`
	CommitSHA         string             `json:"commitSha"`
	DefinitionDigest  string             `json:"definitionDigest"`
	Input             map[string]any     `json:"input"`
}

type webhookEvaluator struct {
	endpoint string
	client   *http.Client
	token    string
}

type WebhookSource struct{ HTTPSource }

func (WebhookSource) Type() string      { return WebhookType }
func (s WebhookSource) Validate() error { return s.validate(WebhookType) }

func (s WebhookSource) Open() (Evaluator, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	transport, token, err := sourceTransport(s.HTTPSource)
	if err != nil {
		return nil, err
	}
	transport.MaxResponseHeaderBytes = 8 << 10
	client := &http.Client{
		Timeout:       s.Timeout,
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return &webhookEvaluator{endpoint: s.URL, client: client, token: token}, nil
}

func (e *webhookEvaluator) Close() { e.client.CloseIdleConnections() }

func (e *webhookEvaluator) Evaluate(ctx context.Context, d EvaluationDemand, at time.Time) Observation {
	policy, ok := d.Definition.Evaluator.(*WebhookDefinition)
	if !ok || policy == nil || d.Definition.Validate() != nil || !d.Subject.valid() || d.RepositoryID == uuid.Nil ||
		d.RepositoryName == "" || d.CatalogRevisionID == uuid.Nil || d.CommitSHA == "" ||
		validateDefinitionDigest(d.DefinitionDigest) != nil || at.IsZero() {
		return webhookFailure("context", "Webhook evaluation context is invalid.")
	}
	payload, err := json.Marshal(webhookRequest{
		APIVersion:        webhookVersion,
		EvaluationID:      uuid.New(),
		EvaluationKey:     d.evaluationKey(),
		EvaluatedAt:       at.UTC(),
		Subject:           d.Subject,
		RepositoryID:      d.RepositoryID,
		RepositoryName:    d.RepositoryName,
		CatalogRevisionID: d.CatalogRevisionID,
		CommitSHA:         d.CommitSHA,
		DefinitionDigest:  d.DefinitionDigest,
		Input:             policy.Input,
	})
	if err != nil || len(payload) > 16384 {
		return webhookFailure("context", "Webhook request is invalid.")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(payload))
	if err != nil {
		return webhookFailure("transport", "Webhook request could not be created.")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if e.token != "" {
		req.Header.Set("Authorization", "Bearer "+e.token)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return webhookTransportFailure(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return webhookFailure("http-status", "Webhook returned an unsuccessful HTTP status.")
	}
	media, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return webhookFailure("media-type", "Webhook response must be application/json.")
	}
	body, oversized, err := readBounded(resp.Body, 4096)
	if err != nil {
		return webhookTransportFailure(err)
	}
	if oversized {
		return webhookFailure("size", "Webhook response exceeded the size limit.")
	}
	return e.responseObservation(body)
}

func (e *webhookEvaluator) responseObservation(body []byte) Observation {
	var response struct {
		APIVersion string           `json:"apiVersion"`
		Decision   ObservationState `json:"decision"`
		WebhookEvidence
	}
	if err := jsonv2.Unmarshal(body, &response, jsonv2.RejectUnknownMembers(true)); err != nil ||
		response.APIVersion != webhookVersion || response.Validate() != nil ||
		(e.token != "" && (strings.Contains(response.Code, e.token) || strings.Contains(response.Message, e.token))) {
		return webhookFailure("invalid-response", "Webhook response was invalid.")
	}
	switch response.Decision {
	case ObservationPassing, ObservationBlocking:
		return Observation{State: response.Decision, Evidence: &Evidence{Payload: &response.WebhookEvidence}}
	case ObservationUnavailable:
		return webhookFailure("remote-unavailable", response.Code+": "+response.Message)
	default:
		return webhookFailure("invalid-response", "Webhook decision was invalid.")
	}
}

func webhookTransportFailure(err error) Observation {
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return webhookFailure("timeout", "Webhook request timed out.")
	}
	return webhookFailure("transport", "Webhook request failed.")
}

func webhookFailure(class, diagnostic string) Observation {
	return Observation{State: ObservationUnavailable, Diagnostic: diagnostic, FailureClass: class}
}
