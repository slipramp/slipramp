package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultSlackRequestTimeout = 15 * time.Second
	maximumSlackFieldsPerBlock = 10
	maximumSlackButtonRunes    = 75
)

type slackSender struct {
	publicURL string
	client    *http.Client
}

type slackMessage struct {
	Text   string       `json:"text"`
	Blocks []slackBlock `json:"blocks"`
}

type slackBlock struct {
	Type     string         `json:"type"`
	Text     *slackText     `json:"text,omitempty"`
	Fields   []slackText    `json:"fields,omitempty"`
	Elements []slackElement `json:"elements,omitempty"`
}

type slackText struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Emoji bool   `json:"emoji,omitempty"`
}

type slackElement struct {
	Type string     `json:"type"`
	Text *slackText `json:"text,omitempty"`
	URL  string     `json:"url,omitempty"`
}

func newSlackSender(publicURL string, client *http.Client) *slackSender {
	if client == nil {
		client = &http.Client{Timeout: defaultSlackRequestTimeout}
	}

	return &slackSender{publicURL: strings.TrimRight(publicURL, "/"), client: client}
}

func (s *slackSender) Send(ctx context.Context, destination Destination, event Event) deliveryOutcome {
	webhookURL, ok := destination.SlackWebhookURL()
	if !ok {
		return deliveryOutcome{
			diagnostic: "Slack destination credentials are unavailable.",
			permanent:  true,
		}
	}
	message, outcome := s.render(event)
	if !outcome.succeeded() {
		return outcome
	}

	body, err := json.Marshal(message)
	if err != nil {
		return deliveryOutcome{
			diagnostic: "Slack message could not be encoded.",
			permanent:  true,
		}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return deliveryOutcome{
			diagnostic: "Slack request could not be created.",
			permanent:  true,
		}
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := s.client.Do(request)
	if err != nil {
		return deliveryOutcome{diagnostic: "Slack request failed before a response was received."}
	}
	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
		return deliveryOutcome{}
	}

	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return deliveryOutcome{
			diagnostic: "Slack rate limited the delivery.",
			retryAfter: retryAfterDuration(response.Header.Get("Retry-After"), time.Now()),
		}
	case response.StatusCode == http.StatusRequestTimeout,
		response.StatusCode == http.StatusTooEarly,
		response.StatusCode >= http.StatusInternalServerError:
		return deliveryOutcome{
			diagnostic: "Slack temporarily rejected the delivery (HTTP " + strconv.Itoa(response.StatusCode) + ").",
		}
	default:
		return deliveryOutcome{
			diagnostic: "Slack rejected the delivery (HTTP " + strconv.Itoa(response.StatusCode) + ").",
			permanent:  true,
		}
	}
}

func (s *slackSender) render(event Event) (slackMessage, deliveryOutcome) {
	if err := event.Validate(); err != nil {
		return slackMessage{}, deliveryOutcome{
			diagnostic: "Notification event payload is invalid.",
			permanent:  true,
		}
	}

	var payload ReviewRequestedPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return slackMessage{}, deliveryOutcome{
			diagnostic: "Notification event payload is invalid.",
			permanent:  true,
		}
	}
	actionURL := s.publicURL + payload.Action.Path
	parsedActionURL, err := url.Parse(actionURL)
	if err != nil || (parsedActionURL.Scheme != "http" && parsedActionURL.Scheme != "https") ||
		parsedActionURL.Host == "" {
		return slackMessage{}, deliveryOutcome{
			diagnostic: "Notification public URL is unavailable.",
			permanent:  true,
		}
	}

	blocks := []slackBlock{{
		Type: "section",
		Text: &slackText{
			Type: "mrkdwn",
			Text: "*" + escapeSlack(payload.Title) + "*\n" + escapeSlack(payload.Summary),
		},
	}}
	for start := 0; start < len(payload.Facts); start += maximumSlackFieldsPerBlock {
		end := min(start+maximumSlackFieldsPerBlock, len(payload.Facts))
		fields := make([]slackText, 0, end-start)
		for _, fact := range payload.Facts[start:end] {
			fields = append(fields, slackText{
				Type: "mrkdwn",
				Text: "*" + escapeSlack(fact.Label) + "*\n" + escapeSlack(fact.Value),
			})
		}
		blocks = append(blocks, slackBlock{Type: "section", Fields: fields})
	}
	blocks = append(blocks, slackBlock{
		Type: "actions",
		Elements: []slackElement{{
			Type: "button",
			Text: &slackText{
				Type:  "plain_text",
				Text:  truncateRunes(payload.Action.Label, maximumSlackButtonRunes),
				Emoji: true,
			},
			URL: actionURL,
		}},
	})

	return slackMessage{
		Text:   escapeSlack(payload.Title) + ": " + escapeSlack(payload.Summary) + " — " + actionURL,
		Blocks: blocks,
	}, deliveryOutcome{}
}

func escapeSlack(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return replacer.Replace(value)
}

func truncateRunes(value string, maximum int) string {
	runes := []rune(value)
	if len(runes) <= maximum {
		return value
	}

	return string(runes[:maximum])
}

func retryAfterDuration(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds > 0 {
		if seconds > int64((1<<63-1)/time.Second) {
			return time.Duration(1<<63 - 1)
		}

		return time.Duration(seconds) * time.Second
	}
	if retryAt, err := http.ParseTime(value); err == nil && retryAt.After(now) {
		return retryAt.Sub(now)
	}

	return 0
}
