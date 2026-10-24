package notification

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSlackSenderRendersReviewWithAuthenticatedActionLink(t *testing.T) {
	t.Parallel()

	var message slackMessage
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.Header.Get("Content-Type") != "application/json" {
			t.Errorf("request = %s with content type %q", request.Method, request.Header.Get("Content-Type"))
		}
		if err := json.NewDecoder(request.Body).Decode(&message); err != nil {
			t.Fatal(err)
		}

		return httpResponse(http.StatusOK, nil), nil
	})}
	sender := newSlackSender("https://slipramp.example.com/control/", client)
	destination := Destination{
		Type:       DestinationTypeSlack,
		webhookURL: "https://hooks.slack.com/services/secret-value",
	}
	input := validReviewRequestedInput()
	input.Summary = "candidate <production> & ready"
	input.Facts = []Fact{{Label: "Repository", Value: "ops <main>"}}
	event, err := NewReviewRequestedEvent(input)
	if err != nil {
		t.Fatal(err)
	}

	outcome := sender.Send(t.Context(), destination, event)
	if !outcome.succeeded() {
		t.Fatalf("Send() outcome = %#v", outcome)
	}
	if !strings.Contains(message.Text, "https://slipramp.example.com/control/promotions/") {
		t.Fatalf("fallback text = %q", message.Text)
	}
	if len(message.Blocks) != 3 || message.Blocks[0].Text == nil ||
		!strings.Contains(message.Blocks[0].Text.Text, "&lt;production&gt; &amp; ready") ||
		len(message.Blocks[1].Fields) != 1 ||
		!strings.Contains(message.Blocks[1].Fields[0].Text, "ops &lt;main&gt;") {
		t.Fatalf("Slack blocks = %#v", message.Blocks)
	}
	action := message.Blocks[2].Elements[0]
	if action.URL != "https://slipramp.example.com/control"+input.Action.Path {
		t.Errorf("action URL = %q", action.URL)
	}
}

func TestSlackSenderClassifiesProviderResponsesWithoutPersistingBodies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		status         int
		headers        http.Header
		requestError   error
		wantPermanent  bool
		wantRetryAfter time.Duration
		wantDiagnostic string
	}{
		{
			name:           "rate limited",
			status:         http.StatusTooManyRequests,
			headers:        http.Header{"Retry-After": []string{"45"}},
			wantRetryAfter: 45 * time.Second,
			wantDiagnostic: "rate limited",
		},
		{
			name:           "server failure",
			status:         http.StatusServiceUnavailable,
			wantDiagnostic: "temporarily rejected",
		},
		{
			name:           "invalid webhook",
			status:         http.StatusBadRequest,
			wantPermanent:  true,
			wantDiagnostic: "HTTP 400",
		},
		{
			name:           "connection failure",
			requestError:   errors.New("request https://hooks.slack.com/services/sensitive failed"),
			wantDiagnostic: "before a response",
		},
		{
			name:           "timeout",
			requestError:   context.DeadlineExceeded,
			wantDiagnostic: "before a response",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				if test.requestError != nil {
					return nil, test.requestError
				}

				return httpResponse(test.status, test.headers), nil
			})}
			sender := newSlackSender("https://slipramp.example.com", client)
			destination := Destination{
				Type:       DestinationTypeSlack,
				webhookURL: "https://hooks.slack.com/services/sensitive",
			}

			outcome := sender.Send(t.Context(), destination, validReviewRequestedEvent(t))
			if outcome.permanent != test.wantPermanent || outcome.retryAfter != test.wantRetryAfter ||
				!strings.Contains(outcome.diagnostic, test.wantDiagnostic) {
				t.Fatalf("Send() outcome = %#v", outcome)
			}
			if strings.Contains(outcome.diagnostic, "sensitive") ||
				strings.Contains(outcome.diagnostic, "provider body") {
				t.Fatalf("diagnostic contains provider or secret material: %q", outcome.diagnostic)
			}
		})
	}
}

func TestSlackSenderFailsMalformedEventWithoutContactingProvider(t *testing.T) {
	t.Parallel()

	sender := newSlackSender("https://slipramp.example.com", &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("malformed event contacted Slack")
			return nil, nil
		}),
	})
	destination := Destination{
		Type:       DestinationTypeSlack,
		webhookURL: "https://hooks.slack.com/services/sensitive",
	}
	event := validReviewRequestedEvent(t)
	event.Payload = []byte(`{}`)

	outcome := sender.Send(t.Context(), destination, event)
	if !outcome.permanent || outcome.diagnostic != "Notification event payload is invalid." {
		t.Fatalf("Send() outcome = %#v", outcome)
	}
}

func httpResponse(status int, headers http.Header) *http.Response {
	if headers == nil {
		headers = http.Header{}
	}

	return &http.Response{
		StatusCode: status,
		Header:     headers,
		Body:       io.NopCloser(strings.NewReader("provider body must not be persisted")),
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
