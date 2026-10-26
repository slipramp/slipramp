package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	EventTypeReviewRequested EventType   = "review.requested"
	SubjectTypePromotion     SubjectType = "promotion"
	SubjectTypeRun           SubjectType = "run"

	maximumDeduplicationKeyLength = 512
	maximumTitleLength            = 200
	maximumSummaryLength          = 500
	maximumFactCount              = 20
	maximumFactLabelLength        = 100
	maximumFactValueLength        = 500
	maximumActionLabelLength      = 100
	maximumActionPathLength       = 2_000
)

type EventType string

func (t EventType) Valid() bool {
	return t == EventTypeReviewRequested
}

type SubjectType string

func (t SubjectType) Valid() bool {
	return t == SubjectTypePromotion || t == SubjectTypeRun
}

type Subject struct {
	Type SubjectType `json:"type"`
	ID   uuid.UUID   `json:"id"`
}

type Fact struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type Action struct {
	Label string `json:"label"`
	Path  string `json:"path"`
}

type ReviewRequestedPayload struct {
	Type          EventType  `json:"type"`
	SchemaVersion int        `json:"schemaVersion"`
	Subject       Subject    `json:"subject"`
	Title         string     `json:"title"`
	Summary       string     `json:"summary"`
	Facts         []Fact     `json:"facts"`
	Action        Action     `json:"action"`
	ExpiresAt     *time.Time `json:"expiresAt,omitempty"`
}

type Event struct {
	ID               uuid.UUID
	Type             EventType
	SchemaVersion    int
	SubjectType      SubjectType
	SubjectID        uuid.UUID
	OccurredAt       time.Time
	DeduplicationKey string
	Payload          json.RawMessage
	CreatedAt        time.Time
}

type ReviewRequestedInput struct {
	ID               uuid.UUID
	SubjectType      SubjectType
	SubjectID        uuid.UUID
	OccurredAt       time.Time
	DeduplicationKey string
	Title            string
	Summary          string
	Facts            []Fact
	Action           Action
	ExpiresAt        *time.Time
	CreatedAt        time.Time
}

func NewReviewRequestedEvent(input ReviewRequestedInput) (Event, error) {
	payload := ReviewRequestedPayload{
		Type:          EventTypeReviewRequested,
		SchemaVersion: 1,
		Subject: Subject{
			Type: input.SubjectType,
			ID:   input.SubjectID,
		},
		Title:     input.Title,
		Summary:   input.Summary,
		Facts:     append([]Fact(nil), input.Facts...),
		Action:    input.Action,
		ExpiresAt: input.ExpiresAt,
	}
	if err := payload.Validate(); err != nil {
		return Event{}, err
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return Event{}, fmt.Errorf("encode review-requested notification payload: %w", err)
	}

	event := Event{
		ID:               input.ID,
		Type:             payload.Type,
		SchemaVersion:    payload.SchemaVersion,
		SubjectType:      payload.Subject.Type,
		SubjectID:        payload.Subject.ID,
		OccurredAt:       input.OccurredAt,
		DeduplicationKey: input.DeduplicationKey,
		Payload:          encoded,
		CreatedAt:        input.CreatedAt,
	}
	if err := event.Validate(); err != nil {
		return Event{}, err
	}

	return event, nil
}

func (p ReviewRequestedPayload) Validate() error {
	if p.Type != EventTypeReviewRequested || p.SchemaVersion != 1 {
		return errors.New("review-requested notification payload contract is invalid")
	}
	if !p.Subject.Type.Valid() || p.Subject.ID == uuid.Nil {
		return errors.New("review-requested notification subject is invalid")
	}
	if err := boundedRequired("notification title", p.Title, maximumTitleLength); err != nil {
		return err
	}
	if err := boundedRequired("notification summary", p.Summary, maximumSummaryLength); err != nil {
		return err
	}
	if len(p.Facts) > maximumFactCount {
		return fmt.Errorf("notification facts must contain at most %d entries", maximumFactCount)
	}
	for index, fact := range p.Facts {
		if err := boundedRequired("notification fact label", fact.Label, maximumFactLabelLength); err != nil {
			return fmt.Errorf("fact %d: %w", index, err)
		}
		if err := boundedRequired("notification fact value", fact.Value, maximumFactValueLength); err != nil {
			return fmt.Errorf("fact %d: %w", index, err)
		}
	}
	if err := boundedRequired("notification action label", p.Action.Label, maximumActionLabelLength); err != nil {
		return err
	}
	if err := validateActionPath(p.Action.Path); err != nil {
		return err
	}

	return nil
}

func (e Event) Validate() error {
	if e.ID == uuid.Nil {
		return errors.New("notification event ID is required")
	}
	if !e.Type.Valid() {
		return fmt.Errorf("notification event type %q is invalid", e.Type)
	}
	if e.SchemaVersion != 1 {
		return errors.New("notification event schema version must be 1")
	}
	if !e.SubjectType.Valid() || e.SubjectID == uuid.Nil {
		return errors.New("notification event subject is invalid")
	}
	if e.OccurredAt.IsZero() || e.CreatedAt.IsZero() {
		return errors.New("notification event occurrence and creation times are required")
	}
	if err := boundedRequired(
		"notification deduplication key",
		e.DeduplicationKey,
		maximumDeduplicationKeyLength,
	); err != nil {
		return err
	}
	if len(e.Payload) == 0 || !json.Valid(e.Payload) || bytes.Equal(bytes.TrimSpace(e.Payload), []byte("null")) {
		return errors.New("notification event payload must be valid JSON")
	}

	var payload ReviewRequestedPayload
	decoder := json.NewDecoder(bytes.NewReader(e.Payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return errors.New("notification event payload does not match its event type")
	}
	if err := payload.Validate(); err != nil {
		return err
	}
	if payload.Type != e.Type || payload.SchemaVersion != e.SchemaVersion ||
		payload.Subject.Type != e.SubjectType || payload.Subject.ID != e.SubjectID {
		return errors.New("notification event envelope does not match its payload")
	}

	return nil
}

func boundedRequired(name, value string, maximum int) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("%s is required", name)
	}
	if len(trimmed) > maximum {
		return fmt.Errorf("%s must contain at most %d bytes", name, maximum)
	}

	return nil
}

func validateActionPath(value string) error {
	if len(value) == 0 || len(value) > maximumActionPathLength || !strings.HasPrefix(value, "/") ||
		strings.HasPrefix(value, "//") {
		return errors.New("notification action path must be an application-relative absolute path")
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("notification action path must be an application-relative path without a query or fragment")
	}

	return nil
}

type DestinationSnapshot struct {
	Name string
	Type DestinationType
}

func (d DestinationSnapshot) Validate() error {
	if !destinationNamePattern.MatchString(d.Name) {
		return fmt.Errorf("notification destination name %q is invalid", d.Name)
	}
	if d.Type != DestinationTypeSlack {
		return fmt.Errorf("notification destination type %q is invalid", d.Type)
	}

	return nil
}

type DeliveryState string

const (
	DeliveryStatePending   DeliveryState = "pending"
	DeliveryStateDelivered DeliveryState = "delivered"
	DeliveryStateFailed    DeliveryState = "failed"
)

type DeliveryHistoryItem struct {
	ID               uuid.UUID
	EventID          uuid.UUID
	EventType        EventType
	SubjectType      SubjectType
	SubjectID        uuid.UUID
	OccurredAt       time.Time
	DestinationName  string
	DestinationType  DestinationType
	State            DeliveryState
	AttemptCount     int
	NextAttemptAt    *time.Time
	LastError        string
	FirstAttemptedAt *time.Time
	LastAttemptedAt  *time.Time
	DeliveredAt      *time.Time
	FailedAt         *time.Time
	CreatedAt        time.Time
}

type DeliveryHistoryPage struct {
	Items []DeliveryHistoryItem
	Total int
}

type DeliveryHistoryFilter struct {
	Limit  int
	Offset int
}

// EnqueuePlan is the immutable event and resolved destination set that a domain
// transaction must persist together. Routing is resolved before the transaction
// so later configuration changes cannot redirect an accepted transition.
type EnqueuePlan struct {
	Event        Event
	Destinations []DestinationSnapshot
}

func (p EnqueuePlan) Validate() error {
	if err := p.Event.Validate(); err != nil {
		return fmt.Errorf("validate notification event: %w", err)
	}
	if len(p.Destinations) == 0 {
		return errors.New("at least one notification destination is required")
	}

	seenDestinations := make(map[string]struct{}, len(p.Destinations))
	for _, destination := range p.Destinations {
		if err := destination.Validate(); err != nil {
			return err
		}
		if _, duplicate := seenDestinations[destination.Name]; duplicate {
			return fmt.Errorf("duplicate notification destination %q", destination.Name)
		}
		seenDestinations[destination.Name] = struct{}{}
	}

	return nil
}

type Store interface {
	EnqueueNotification(context.Context, Event, []DestinationSnapshot) (bool, error)
	ListNotificationDeliveries(context.Context, DeliveryHistoryFilter) (DeliveryHistoryPage, error)
}

type Service struct {
	config Config
	store  Store
}

func NewService(config Config, store Store) (*Service, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("validate notification configuration: %w", err)
	}
	if store == nil {
		return nil, errors.New("notification store is required")
	}

	return &Service{config: config, store: store}, nil
}

func (s *Service) Plan(event Event) (*EnqueuePlan, error) {
	if err := event.Validate(); err != nil {
		return nil, fmt.Errorf("validate notification event: %w", err)
	}
	destinations := s.config.DestinationsFor(event.Type)
	if len(destinations) == 0 {
		return nil, nil
	}

	plan := &EnqueuePlan{Event: event, Destinations: destinations}
	if err := plan.Validate(); err != nil {
		return nil, err
	}

	return plan, nil
}

func (s *Service) Enqueue(ctx context.Context, event Event) (bool, error) {
	plan, err := s.Plan(event)
	if err != nil {
		return false, err
	}
	if plan == nil {
		return false, nil
	}

	created, err := s.store.EnqueueNotification(ctx, plan.Event, plan.Destinations)
	if err != nil {
		return false, fmt.Errorf("enqueue notification: %w", err)
	}

	return created, nil
}

func (s *Service) ListDeliveries(
	ctx context.Context,
	filter DeliveryHistoryFilter,
) (DeliveryHistoryPage, error) {
	if filter.Limit < 1 || filter.Limit > 200 || filter.Offset < 0 || filter.Offset > 2_147_483_647 {
		return DeliveryHistoryPage{}, errors.New("invalid notification delivery list window")
	}

	return s.store.ListNotificationDeliveries(ctx, filter)
}
