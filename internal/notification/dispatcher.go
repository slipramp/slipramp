package notification

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	DefaultDispatchInterval = time.Second
	InitialRetryBackoff     = 5 * time.Second

	maximumDeliveryDiagnosticLength = 512
)

var ErrNoEligibleDelivery = errors.New("no eligible notification delivery")

type DeliveryClaim struct {
	AttemptedAt    time.Time
	MaximumBackoff time.Duration
	Jitter         float64
}

type DeliveryAttempt struct {
	ID            uuid.UUID
	Event         Event
	Destination   DestinationSnapshot
	AttemptCount  int
	NextAttemptAt time.Time
}

type DeliveryStore interface {
	ClaimNextNotificationDelivery(context.Context, DeliveryClaim) (DeliveryAttempt, error)
	MarkNotificationDeliveryDelivered(context.Context, uuid.UUID, int, time.Time) (bool, error)
	RetryNotificationDelivery(context.Context, uuid.UUID, int, time.Time, string) (bool, error)
	FailNotificationDelivery(context.Context, uuid.UUID, int, time.Time, string) (bool, error)
}

type deliveryOutcome struct {
	diagnostic string
	permanent  bool
	retryAfter time.Duration
}

func (o deliveryOutcome) succeeded() bool {
	return o.diagnostic == ""
}

type deliverySender interface {
	Send(context.Context, Destination, Event) deliveryOutcome
}

type Dispatcher struct {
	config     Config
	deliveries DeliveryStore
	sender     deliverySender
	interval   time.Duration
	logger     *slog.Logger
	now        func() time.Time
	jitter     func() float64
}

func NewDispatcher(config Config, deliveries DeliveryStore, interval time.Duration, logger *slog.Logger) *Dispatcher {
	return &Dispatcher{
		config:     config,
		deliveries: deliveries,
		sender:     newSlackSender(config.PublicURL, nil),
		interval:   interval,
		logger:     logger.With("component", "notification_dispatcher"),
		now:        time.Now,
		jitter: func() float64 {
			return 0.8 + rand.Float64()*0.4
		},
	}
}

func (d *Dispatcher) Run(ctx context.Context) {
	d.drain(ctx)

	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.drain(ctx)
		}
	}
}

func (d *Dispatcher) drain(ctx context.Context) {
	for ctx.Err() == nil {
		processed, err := d.ProcessNext(ctx)
		if err != nil {
			if ctx.Err() == nil {
				d.logger.Warn("dispatch notification delivery", "error", err)
			}

			return
		}
		if !processed {
			return
		}
	}
}

// ProcessNext handles the oldest eligible delivery, if one exists.
func (d *Dispatcher) ProcessNext(ctx context.Context) (bool, error) {
	attemptedAt := d.now().UTC()
	attempt, err := d.deliveries.ClaimNextNotificationDelivery(ctx, DeliveryClaim{
		AttemptedAt:    attemptedAt,
		MaximumBackoff: d.config.Retry.MaximumBackoff,
		Jitter:         d.jitter(),
	})
	if errors.Is(err, ErrNoEligibleDelivery) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim eligible notification delivery: %w", err)
	}

	destination, configured := d.config.Destinations[attempt.Destination.Name]
	var outcome deliveryOutcome
	if !configured || destination.Type != attempt.Destination.Type {
		outcome = deliveryOutcome{
			diagnostic: "Notification destination is no longer configured.",
		}
	} else {
		outcome = d.sender.Send(ctx, destination, attempt.Event)
	}

	if ctx.Err() != nil {
		return true, nil
	}

	resolvedAt := d.now().UTC()
	if outcome.succeeded() {
		updated, updateErr := d.deliveries.MarkNotificationDeliveryDelivered(
			ctx,
			attempt.ID,
			attempt.AttemptCount,
			resolvedAt,
		)
		if updateErr != nil {
			return true, fmt.Errorf("mark notification delivery %s delivered: %w", attempt.ID, updateErr)
		}
		if updated {
			d.logger.Info(
				"delivered notification",
				"delivery", attempt.ID,
				"event", attempt.Event.ID,
				"destination", attempt.Destination.Name,
				"attempt", attempt.AttemptCount,
			)
		}

		return true, nil
	}

	diagnostic := d.safeDiagnostic(outcome.diagnostic)
	terminal := outcome.permanent || attempt.AttemptCount >= d.config.Retry.MaximumAttempts
	if terminal {
		updated, updateErr := d.deliveries.FailNotificationDelivery(
			ctx,
			attempt.ID,
			attempt.AttemptCount,
			resolvedAt,
			diagnostic,
		)
		if updateErr != nil {
			return true, fmt.Errorf("fail notification delivery %s: %w", attempt.ID, updateErr)
		}
		if updated {
			d.logger.Warn(
				"notification delivery failed",
				"delivery", attempt.ID,
				"event", attempt.Event.ID,
				"destination", attempt.Destination.Name,
				"attempt", attempt.AttemptCount,
				"diagnostic", diagnostic,
			)
		}

		return true, nil
	}

	retryDelay := max(attempt.NextAttemptAt.Sub(attemptedAt), 0)
	nextAttemptAt := resolvedAt.Add(retryDelay)
	if outcome.retryAfter > 0 {
		retryAfter := min(outcome.retryAfter, d.config.Retry.MaximumBackoff)
		providerAttemptAt := resolvedAt.Add(retryAfter)
		if providerAttemptAt.After(nextAttemptAt) {
			nextAttemptAt = providerAttemptAt
		}
	}
	updated, updateErr := d.deliveries.RetryNotificationDelivery(
		ctx,
		attempt.ID,
		attempt.AttemptCount,
		nextAttemptAt,
		diagnostic,
	)
	if updateErr != nil {
		return true, fmt.Errorf("reschedule notification delivery %s: %w", attempt.ID, updateErr)
	}
	if updated {
		d.logger.Warn(
			"notification delivery will retry",
			"delivery", attempt.ID,
			"event", attempt.Event.ID,
			"destination", attempt.Destination.Name,
			"attempt", attempt.AttemptCount,
			"next_attempt_at", nextAttemptAt,
			"diagnostic", diagnostic,
		)
	}

	return true, nil
}

func RetryBackoff(attempt int, maximum time.Duration, jitter float64) time.Duration {
	if attempt < 1 || maximum <= 0 {
		return 0
	}
	if jitter < 0.8 {
		jitter = 0.8
	}
	if jitter > 1.2 {
		jitter = 1.2
	}

	delay := InitialRetryBackoff
	for range attempt - 1 {
		if delay >= maximum/2 {
			delay = maximum
			break
		}
		delay *= 2
	}
	if delay > maximum {
		delay = maximum
	}

	jittered := time.Duration(float64(delay) * jitter)
	return min(jittered, maximum)
}

func (d *Dispatcher) safeDiagnostic(value string) string {
	value = strings.TrimSpace(value)
	for _, destination := range d.config.Destinations {
		if secret, ok := destination.SlackWebhookURL(); ok {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
	}
	if value == "" {
		value = "Notification delivery failed."
	}
	if len(value) <= maximumDeliveryDiagnosticLength {
		return value
	}

	value = value[:maximumDeliveryDiagnosticLength]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}

	return value
}
