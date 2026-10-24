package notification

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	DefaultMaximumAttempts = 5
	DefaultMaximumBackoff  = time.Hour
	DefaultRetentionPeriod = 30 * 24 * time.Hour
)

var destinationNamePattern = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`)

type DestinationType string

const DestinationTypeSlack DestinationType = "slack"

type secretValue string

func (secretValue) String() string {
	return "[REDACTED]"
}

func (secretValue) GoString() string {
	return "[REDACTED]"
}

func (secretValue) LogValue() slog.Value {
	return slog.StringValue("[REDACTED]")
}

type Destination struct {
	Type           DestinationType
	webhookURLFile string
	webhookURL     secretValue
}

func (d Destination) String() string {
	return fmt.Sprintf("%s destination", d.Type)
}

func (d Destination) GoString() string {
	return fmt.Sprintf(
		"notification.Destination{Type:%q, WebhookURLFile:[CONFIGURED], webhookURL:[REDACTED]}",
		d.Type,
	)
}

func (d Destination) LogValue() slog.Value {
	return slog.GroupValue(slog.String("type", string(d.Type)))
}

func NewSlackDestination(webhookURLFile, webhookURL string) (Destination, error) {
	destination := Destination{
		Type:           DestinationTypeSlack,
		webhookURLFile: webhookURLFile,
		webhookURL:     secretValue(strings.TrimSpace(webhookURL)),
	}
	if err := destination.validate(); err != nil {
		return Destination{}, err
	}

	return destination, nil
}

func (d Destination) SlackWebhookURL() (string, bool) {
	if d.Type != DestinationTypeSlack || d.webhookURL == "" {
		return "", false
	}

	return string(d.webhookURL), true
}

func (d Destination) validate() error {
	if d.Type != DestinationTypeSlack {
		return fmt.Errorf("unknown destination type %q", d.Type)
	}
	if !filepath.IsAbs(d.webhookURLFile) {
		return errors.New("slack webhook URL file must be an absolute path")
	}
	webhookURL, err := url.Parse(string(d.webhookURL))
	if err != nil || webhookURL.Scheme != "https" || webhookURL.Host == "" {
		return errors.New("slack webhook URL file must contain an absolute HTTPS URL")
	}
	if webhookURL.User != nil || webhookURL.Fragment != "" {
		return errors.New("slack webhook URL must not contain user information or a fragment")
	}

	return nil
}

type Route struct {
	Events       []EventType
	Destinations []string
}

type RetryPolicy struct {
	MaximumAttempts int
	MaximumBackoff  time.Duration
}

type Config struct {
	PublicURL       string
	Destinations    map[string]Destination
	Routes          []Route
	Retry           RetryPolicy
	RetentionPeriod time.Duration
}

func DefaultConfig() Config {
	return Config{
		Destinations: map[string]Destination{},
		Routes:       []Route{},
		Retry: RetryPolicy{
			MaximumAttempts: DefaultMaximumAttempts,
			MaximumBackoff:  DefaultMaximumBackoff,
		},
		RetentionPeriod: DefaultRetentionPeriod,
	}
}

func (c Config) Validate() error {
	if c.Retry.MaximumAttempts < 1 || c.Retry.MaximumAttempts > 100 {
		return errors.New("notification maximum attempts must be between 1 and 100")
	}
	if c.Retry.MaximumBackoff <= 0 {
		return errors.New("notification maximum backoff must be positive")
	}
	if c.RetentionPeriod <= 0 {
		return errors.New("notification retention period must be positive")
	}

	if len(c.Destinations) == 0 && len(c.Routes) == 0 {
		if strings.TrimSpace(c.PublicURL) != "" {
			return errors.New("notification public URL requires at least one destination and route")
		}

		return nil
	}
	if len(c.Destinations) == 0 {
		return errors.New("notification routes require at least one destination")
	}
	if len(c.Routes) == 0 {
		return errors.New("notification destinations require at least one route")
	}
	if err := validatePublicURL(c.PublicURL); err != nil {
		return err
	}

	for name, destination := range c.Destinations {
		if !destinationNamePattern.MatchString(name) {
			return fmt.Errorf("notification destination name %q is invalid", name)
		}
		if err := destination.validate(); err != nil {
			return fmt.Errorf("validate notification destination %q: %w", name, err)
		}
	}

	for index, route := range c.Routes {
		if len(route.Events) == 0 {
			return fmt.Errorf("notification route %d must select at least one event", index)
		}
		if len(route.Destinations) == 0 {
			return fmt.Errorf("notification route %d must select at least one destination", index)
		}
		seenEvents := map[EventType]struct{}{}
		for _, eventType := range route.Events {
			if !eventType.Valid() {
				return fmt.Errorf("notification route %d contains unknown event type %q", index, eventType)
			}
			if _, duplicate := seenEvents[eventType]; duplicate {
				return fmt.Errorf("notification route %d contains duplicate event type %q", index, eventType)
			}
			seenEvents[eventType] = struct{}{}
		}
		seenDestinations := map[string]struct{}{}
		for _, destinationName := range route.Destinations {
			if _, ok := c.Destinations[destinationName]; !ok {
				return fmt.Errorf(
					"notification route %d selects unknown destination %q",
					index,
					destinationName,
				)
			}
			if _, duplicate := seenDestinations[destinationName]; duplicate {
				return fmt.Errorf("notification route %d contains duplicate destination %q", index, destinationName)
			}
			seenDestinations[destinationName] = struct{}{}
		}
	}

	return nil
}

func validatePublicURL(value string) error {
	publicURL, err := url.Parse(strings.TrimSpace(value))
	if err != nil || (publicURL.Scheme != "http" && publicURL.Scheme != "https") || publicURL.Host == "" {
		return errors.New("notification public URL must be an absolute HTTP or HTTPS URL")
	}
	if publicURL.User != nil || publicURL.RawQuery != "" || publicURL.Fragment != "" {
		return errors.New("notification public URL must not contain user information, a query, or a fragment")
	}

	return nil
}

func (c Config) DestinationsFor(eventType EventType) []DestinationSnapshot {
	selected := map[string]struct{}{}
	for _, route := range c.Routes {
		if !slices.Contains(route.Events, eventType) {
			continue
		}
		for _, destinationName := range route.Destinations {
			selected[destinationName] = struct{}{}
		}
	}

	names := make([]string, 0, len(selected))
	for name := range selected {
		names = append(names, name)
	}
	slices.Sort(names)

	destinations := make([]DestinationSnapshot, 0, len(names))
	for _, name := range names {
		destination, ok := c.Destinations[name]
		if !ok {
			continue
		}
		destinations = append(destinations, DestinationSnapshot{Name: name, Type: destination.Type})
	}

	return destinations
}
