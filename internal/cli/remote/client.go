package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"unicode"

	"github.com/slipramp/slipramp/internal/api"
)

// Client owns the generated API client and the credential-scrubbing boundary
// for errors returned to terminal users.
type Client struct {
	API    *api.ClientWithResponses
	apiKey string
}

func New(serverURL, apiKey string, httpClient *http.Client) (*Client, error) {
	serverURL = strings.TrimSpace(serverURL)
	if serverURL == "" {
		return nil, errors.New("server URL is required; use --server or set SLIPRAMP_URL")
	}
	parsed, err := url.Parse(serverURL)
	if err != nil || parsed.Host == "" || parsed.User != nil ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("server URL must be an absolute http:// or https:// URL without credentials, a query, or a fragment")
	}

	if apiKey == "" {
		return nil, errors.New("API key is required in SLIPRAMP_API_KEY")
	}
	if strings.TrimSpace(apiKey) != apiKey || strings.IndexFunc(apiKey, unicode.IsControl) >= 0 {
		return nil, errors.New("SLIPRAMP_API_KEY contains invalid whitespace or control characters")
	}

	options := []api.ClientOption{
		api.WithRequestEditorFn(func(_ context.Context, request *http.Request) error {
			request.Header.Set("Authorization", "Bearer "+apiKey)

			return nil
		}),
	}
	if httpClient != nil {
		options = append(options, api.WithHTTPClient(httpClient))
	}
	generated, err := api.NewClientWithResponses(parsed.String(), options...)
	if err != nil {
		return nil, fmt.Errorf("create Server client: %w", err)
	}

	return &Client{API: generated, apiKey: apiKey}, nil
}

func (c *Client) NetworkError(operation string, err error) error {
	if err == nil {
		return nil
	}
	detail := c.scrub(err.Error())

	return fmt.Errorf("%s: network failure: %s", operation, detail)
}

func (c *Client) ResponseError(operation string, status int, body []byte) error {
	category := responseCategory(status)
	detail := http.StatusText(status)
	var problem api.Problem
	if err := json.Unmarshal(body, &problem); err == nil {
		switch {
		case problem.Detail != nil && strings.TrimSpace(*problem.Detail) != "":
			detail = strings.TrimSpace(*problem.Detail)
		case strings.TrimSpace(problem.Title) != "":
			detail = strings.TrimSpace(problem.Title)
		}
	}
	if detail == "" {
		detail = "unexpected response"
	}

	return fmt.Errorf("%s: %s (HTTP %d): %s", operation, category, status, c.scrub(detail))
}

func (c *Client) InvalidResponse(operation string, err error) error {
	return fmt.Errorf("%s: invalid Server response: %s", operation, c.scrub(err.Error()))
}

func (c *Client) scrub(value string) string {
	if c.apiKey == "" {
		return value
	}

	return strings.ReplaceAll(value, c.apiKey, "[REDACTED]")
}

func responseCategory(status int) string {
	switch status {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return "validation failed"
	case http.StatusUnauthorized:
		return "authentication failed"
	case http.StatusForbidden:
		return "authorization failed"
	case http.StatusNotFound:
		return "not found"
	case http.StatusConflict:
		return "conflict"
	default:
		if status >= 500 {
			return "server failure"
		}

		return "request failed"
	}
}
