package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/slipramp/slipramp/internal/notification"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

var _ notification.Store = (*Database)(nil)
var _ notification.DeliveryStore = (*Database)(nil)

func (d *Database) EnqueueNotification(
	ctx context.Context,
	event notification.Event,
	destinations []notification.DestinationSnapshot,
) (bool, error) {
	plan := notification.EnqueuePlan{Event: event, Destinations: destinations}
	if err := plan.Validate(); err != nil {
		return false, err
	}

	transaction, err := d.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin notification enqueue: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()
	queries := d.queries.WithTx(transaction)
	created, err := enqueueNotification(ctx, queries, event, destinations)
	if err != nil {
		return false, err
	}
	if !created {
		return false, nil
	}

	if err := transaction.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit notification enqueue: %w", err)
	}

	return true, nil
}

func enqueueNotification(
	ctx context.Context,
	queries *storedb.Queries,
	event notification.Event,
	destinations []notification.DestinationSnapshot,
) (bool, error) {
	eventID, err := queries.CreateNotificationEvent(ctx, storedb.CreateNotificationEventParams{
		ID:               databaseUUID(event.ID),
		EventType:        string(event.Type),
		SchemaVersion:    int32(event.SchemaVersion),
		SubjectType:      string(event.SubjectType),
		SubjectID:        databaseUUID(event.SubjectID),
		OccurredAt:       databaseTime(event.OccurredAt),
		PayloadJson:      event.Payload,
		DeduplicationKey: event.DeduplicationKey,
		CreatedAt:        databaseTime(event.CreatedAt),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		if err := verifyNotificationDeduplication(ctx, queries, event); err != nil {
			return false, err
		}

		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("create notification event: %w", err)
	}

	for _, destination := range destinations {
		err := queries.CreateNotificationDelivery(ctx, storedb.CreateNotificationDeliveryParams{
			ID:              databaseUUID(uuid.New()),
			EventID:         eventID,
			DestinationName: destination.Name,
			DestinationType: string(destination.Type),
			CreatedAt:       databaseTime(event.CreatedAt),
		})
		if err != nil {
			return false, fmt.Errorf("create notification delivery for %q: %w", destination.Name, err)
		}
	}

	return true, nil
}

func verifyNotificationDeduplication(
	ctx context.Context,
	queries *storedb.Queries,
	event notification.Event,
) error {
	existing, err := queries.GetNotificationEventByDeduplicationKey(ctx, event.DeduplicationKey)
	if err != nil {
		return fmt.Errorf("get deduplicated notification event: %w", err)
	}
	if existing.EventType != string(event.Type) || existing.SchemaVersion != int32(event.SchemaVersion) ||
		existing.SubjectType != string(event.SubjectType) || uuid.UUID(existing.SubjectID.Bytes) != event.SubjectID ||
		!existing.OccurredAt.Time.Equal(event.OccurredAt.Truncate(time.Microsecond)) ||
		!notificationPayloadsEqual(existing.PayloadJson, event.Payload) {
		return errors.New("notification deduplication key belongs to another event")
	}

	return nil
}

func notificationPayloadsEqual(first, second []byte) bool {
	var firstPayload any
	if err := json.Unmarshal(first, &firstPayload); err != nil {
		return false
	}
	var secondPayload any
	if err := json.Unmarshal(second, &secondPayload); err != nil {
		return false
	}

	return reflect.DeepEqual(firstPayload, secondPayload)
}

func (d *Database) ClaimNextNotificationDelivery(
	ctx context.Context,
	claim notification.DeliveryClaim,
) (notification.DeliveryAttempt, error) {
	if claim.AttemptedAt.IsZero() || claim.MaximumBackoff <= 0 || claim.Jitter < 0.8 || claim.Jitter > 1.2 {
		return notification.DeliveryAttempt{}, errors.New("invalid notification delivery claim")
	}

	transaction, err := d.pool.Begin(ctx)
	if err != nil {
		return notification.DeliveryAttempt{}, fmt.Errorf("begin notification delivery claim: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()
	queries := d.queries.WithTx(transaction)
	row, err := queries.GetEligibleNotificationDeliveryForUpdate(ctx, databaseTime(claim.AttemptedAt))
	if errors.Is(err, pgx.ErrNoRows) {
		return notification.DeliveryAttempt{}, notification.ErrNoEligibleDelivery
	}
	if err != nil {
		return notification.DeliveryAttempt{}, fmt.Errorf("get eligible notification delivery: %w", err)
	}

	attemptCount := int(row.AttemptCount) + 1
	nextAttemptAt := claim.AttemptedAt.Add(notification.RetryBackoff(
		attemptCount,
		claim.MaximumBackoff,
		claim.Jitter,
	))
	updated, err := queries.ClaimNotificationDelivery(ctx, storedb.ClaimNotificationDeliveryParams{
		NextAttemptAt:        databaseTime(nextAttemptAt),
		AttemptedAt:          databaseTime(claim.AttemptedAt),
		ID:                   row.ID,
		PreviousAttemptCount: row.AttemptCount,
	})
	if err != nil {
		return notification.DeliveryAttempt{}, fmt.Errorf("advance notification delivery attempt: %w", err)
	}
	if updated != 1 {
		return notification.DeliveryAttempt{}, errors.New("eligible notification delivery changed while claimed")
	}
	if err := transaction.Commit(ctx); err != nil {
		return notification.DeliveryAttempt{}, fmt.Errorf("commit notification delivery claim: %w", err)
	}

	return notification.DeliveryAttempt{
		ID: uuid.UUID(row.ID.Bytes),
		Event: notification.Event{
			ID:               uuid.UUID(row.EventID.Bytes),
			Type:             notification.EventType(row.EventType),
			SchemaVersion:    int(row.SchemaVersion),
			SubjectType:      notification.SubjectType(row.SubjectType),
			SubjectID:        uuid.UUID(row.SubjectID.Bytes),
			OccurredAt:       row.OccurredAt.Time.UTC(),
			DeduplicationKey: row.DeduplicationKey,
			Payload:          row.PayloadJson,
			CreatedAt:        row.EventCreatedAt.Time.UTC(),
		},
		Destination: notification.DestinationSnapshot{
			Name: row.DestinationName,
			Type: notification.DestinationType(row.DestinationType),
		},
		AttemptCount:  attemptCount,
		NextAttemptAt: nextAttemptAt,
	}, nil
}

func (d *Database) MarkNotificationDeliveryDelivered(
	ctx context.Context,
	deliveryID uuid.UUID,
	attemptCount int,
	deliveredAt time.Time,
) (bool, error) {
	if deliveryID == uuid.Nil || attemptCount < 1 || deliveredAt.IsZero() {
		return false, errors.New("invalid delivered notification result")
	}
	updated, err := d.queries.MarkNotificationDeliveryDelivered(
		ctx,
		storedb.MarkNotificationDeliveryDeliveredParams{
			DeliveredAt:  databaseTime(deliveredAt),
			ID:           databaseUUID(deliveryID),
			AttemptCount: int32(attemptCount),
		},
	)
	if err != nil {
		return false, fmt.Errorf("mark notification delivery delivered: %w", err)
	}

	return updated == 1, nil
}

func (d *Database) RetryNotificationDelivery(
	ctx context.Context,
	deliveryID uuid.UUID,
	attemptCount int,
	nextAttemptAt time.Time,
	diagnostic string,
) (bool, error) {
	if deliveryID == uuid.Nil || attemptCount < 1 || nextAttemptAt.IsZero() {
		return false, errors.New("invalid retryable notification result")
	}
	if err := validateNotificationDiagnostic(diagnostic); err != nil {
		return false, err
	}
	updated, err := d.queries.RetryNotificationDelivery(ctx, storedb.RetryNotificationDeliveryParams{
		NextAttemptAt: databaseTime(nextAttemptAt),
		LastError:     &diagnostic,
		ID:            databaseUUID(deliveryID),
		AttemptCount:  int32(attemptCount),
	})
	if err != nil {
		return false, fmt.Errorf("retry notification delivery: %w", err)
	}

	return updated == 1, nil
}

func (d *Database) FailNotificationDelivery(
	ctx context.Context,
	deliveryID uuid.UUID,
	attemptCount int,
	failedAt time.Time,
	diagnostic string,
) (bool, error) {
	if deliveryID == uuid.Nil || attemptCount < 1 || failedAt.IsZero() {
		return false, errors.New("invalid failed notification result")
	}
	if err := validateNotificationDiagnostic(diagnostic); err != nil {
		return false, err
	}
	updated, err := d.queries.FailNotificationDelivery(ctx, storedb.FailNotificationDeliveryParams{
		LastError:    &diagnostic,
		FailedAt:     databaseTime(failedAt),
		ID:           databaseUUID(deliveryID),
		AttemptCount: int32(attemptCount),
	})
	if err != nil {
		return false, fmt.Errorf("fail notification delivery: %w", err)
	}

	return updated == 1, nil
}

func validateNotificationDiagnostic(diagnostic string) error {
	if strings.TrimSpace(diagnostic) == "" || len(diagnostic) > 512 {
		return errors.New("notification delivery diagnostic must contain between 1 and 512 bytes")
	}

	return nil
}

func (d *Database) ListNotificationDeliveries(
	ctx context.Context,
	filter notification.DeliveryHistoryFilter,
) (notification.DeliveryHistoryPage, error) {
	if filter.Limit < 1 || filter.Limit > 200 || filter.Offset < 0 || filter.Offset > 2_147_483_647 {
		return notification.DeliveryHistoryPage{}, errors.New("invalid notification delivery list window")
	}

	rows, err := d.queries.ListNotificationDeliveries(ctx, storedb.ListNotificationDeliveriesParams{
		ResultLimit:  int32(filter.Limit),
		ResultOffset: int32(filter.Offset),
	})
	if err != nil {
		return notification.DeliveryHistoryPage{}, fmt.Errorf("list notification deliveries: %w", err)
	}
	total, err := d.queries.CountNotificationDeliveries(ctx)
	if err != nil {
		return notification.DeliveryHistoryPage{}, fmt.Errorf("count notification deliveries: %w", err)
	}

	page := notification.DeliveryHistoryPage{
		Items: make([]notification.DeliveryHistoryItem, 0, len(rows)),
		Total: int(total),
	}
	for _, row := range rows {
		lastError := ""
		if row.LastError != nil {
			lastError = *row.LastError
		}
		page.Items = append(page.Items, notification.DeliveryHistoryItem{
			ID:               uuid.UUID(row.ID.Bytes),
			EventID:          uuid.UUID(row.EventID.Bytes),
			EventType:        notification.EventType(row.EventType),
			SubjectType:      notification.SubjectType(row.SubjectType),
			SubjectID:        uuid.UUID(row.SubjectID.Bytes),
			OccurredAt:       row.OccurredAt.Time.UTC(),
			DestinationName:  row.DestinationName,
			DestinationType:  notification.DestinationType(row.DestinationType),
			State:            notification.DeliveryState(row.State),
			AttemptCount:     int(row.AttemptCount),
			NextAttemptAt:    applicationTime(row.NextAttemptAt),
			LastError:        lastError,
			FirstAttemptedAt: applicationTime(row.FirstAttemptedAt),
			LastAttemptedAt:  applicationTime(row.LastAttemptedAt),
			DeliveredAt:      applicationTime(row.DeliveredAt),
			FailedAt:         applicationTime(row.FailedAt),
			CreatedAt:        row.CreatedAt.Time.UTC(),
		})
	}

	return page, nil
}

func (d *Database) DeleteTerminalNotificationHistoryBefore(ctx context.Context, before time.Time) (int64, error) {
	if before.IsZero() {
		return 0, errors.New("notification retention cutoff is required")
	}
	deleted, err := d.queries.DeleteTerminalNotificationEventsBefore(ctx, databaseTime(before))
	if err != nil {
		return 0, fmt.Errorf("delete terminal notification history: %w", err)
	}

	return deleted, nil
}
