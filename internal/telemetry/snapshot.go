package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	dbconv "go.opentelemetry.io/otel/semconv/v1.43.0/dbconv"

	"github.com/slipramp/slipramp/internal/notification"
	"github.com/slipramp/slipramp/internal/repository"
	"github.com/slipramp/slipramp/internal/run"
	"github.com/slipramp/slipramp/internal/store"
)

const (
	defaultSnapshotInterval = 15 * time.Second
	defaultSnapshotTimeout  = 2 * time.Second
)

type publishedSnapshot struct {
	state       store.MetricsSnapshot
	pool        store.DatabasePoolMetrics
	success     bool
	lastSuccess *time.Time
}

type snapshotPublisher struct {
	store        SnapshotStore
	logger       *slog.Logger
	interval     time.Duration
	timeout      time.Duration
	now          func() time.Time
	published    atomic.Pointer[publishedSnapshot]
	registration metric.Registration
}

type snapshotInstruments struct {
	activeRuns                   metric.Int64ObservableGauge
	pendingRunReviews            metric.Int64ObservableGauge
	oldestEligibleRunAge         metric.Float64ObservableGauge
	connectedControllers         metric.Int64ObservableGauge
	controllerLastSeenTime       metric.Float64ObservableGauge
	repositoryRefreshState       metric.Int64ObservableGauge
	notificationDeliveryState    metric.Int64ObservableGauge
	snapshotSuccess              metric.Int64ObservableGauge
	snapshotLastSuccessTime      metric.Float64ObservableGauge
	databaseConnectionCount      dbconv.ClientConnectionCountObservable
	databaseConnectionMax        dbconv.ClientConnectionMaxObservable
	databasePoolAcquireWaits     metric.Int64ObservableCounter
	databasePoolCanceledAcquires metric.Int64ObservableCounter
	databasePoolAcquireDuration  metric.Float64ObservableCounter
}

func newSnapshotPublisher(
	meter metric.Meter,
	snapshotStore SnapshotStore,
	logger *slog.Logger,
) (*snapshotPublisher, error) {
	instruments, err := newSnapshotInstruments(meter)
	if err != nil {
		return nil, err
	}

	publisher := &snapshotPublisher{
		store:    snapshotStore,
		logger:   logger.With("component", "metrics_snapshot"),
		interval: defaultSnapshotInterval,
		timeout:  defaultSnapshotTimeout,
		now:      time.Now,
	}
	publisher.published.Store(&publishedSnapshot{
		state: store.MetricsSnapshot{
			ActiveRuns:                 map[string]int64{},
			RepositoryRefreshStates:    map[string]int64{},
			NotificationDeliveryStates: map[string]int64{},
		},
	})

	registration, err := meter.RegisterCallback(
		publisher.observe(instruments),
		instruments.activeRuns,
		instruments.pendingRunReviews,
		instruments.oldestEligibleRunAge,
		instruments.connectedControllers,
		instruments.controllerLastSeenTime,
		instruments.repositoryRefreshState,
		instruments.notificationDeliveryState,
		instruments.snapshotSuccess,
		instruments.snapshotLastSuccessTime,
		instruments.databaseConnectionCount.Inst(),
		instruments.databaseConnectionMax.Inst(),
		instruments.databasePoolAcquireWaits,
		instruments.databasePoolCanceledAcquires,
		instruments.databasePoolAcquireDuration,
	)
	if err != nil {
		return nil, fmt.Errorf("register current-state metric callback: %w", err)
	}
	publisher.registration = registration

	return publisher, nil
}

func newSnapshotInstruments(meter metric.Meter) (snapshotInstruments, error) {
	var instruments snapshotInstruments
	var err error

	instruments.activeRuns, err = meter.Int64ObservableGauge(
		"slipramp.run.active",
		metric.WithUnit("{run}"),
		metric.WithDescription("Active Runs by lifecycle phase."),
	)
	if err != nil {
		return snapshotInstruments{}, fmt.Errorf("create active Runs metric: %w", err)
	}
	instruments.pendingRunReviews, err = meter.Int64ObservableGauge(
		"slipramp.run.review.pending",
		metric.WithUnit("{run}"),
		metric.WithDescription("Queued Runs awaiting review."),
	)
	if err != nil {
		return snapshotInstruments{}, fmt.Errorf("create pending Run reviews metric: %w", err)
	}
	instruments.oldestEligibleRunAge, err = meter.Float64ObservableGauge(
		"slipramp.run.queue.oldest_eligible.age",
		metric.WithUnit("s"),
		metric.WithDescription("Age of the oldest dispatch-eligible queued Run."),
	)
	if err != nil {
		return snapshotInstruments{}, fmt.Errorf("create oldest eligible Run age metric: %w", err)
	}
	instruments.connectedControllers, err = meter.Int64ObservableGauge(
		"slipramp.controller.connected",
		metric.WithUnit("{controller}"),
		metric.WithDescription("Controllers whose most recent contact is still fresh."),
	)
	if err != nil {
		return snapshotInstruments{}, fmt.Errorf("create connected Controllers metric: %w", err)
	}
	instruments.controllerLastSeenTime, err = meter.Float64ObservableGauge(
		"slipramp.controller.last_seen.time",
		metric.WithUnit("s"),
		metric.WithDescription("Unix time of the most recent Controller contact."),
	)
	if err != nil {
		return snapshotInstruments{}, fmt.Errorf("create Controller last-seen metric: %w", err)
	}
	instruments.repositoryRefreshState, err = meter.Int64ObservableGauge(
		"slipramp.repository.refresh.state",
		metric.WithUnit("{repository}"),
		metric.WithDescription("Active Repositories by refresh state."),
	)
	if err != nil {
		return snapshotInstruments{}, fmt.Errorf("create Repository refresh state metric: %w", err)
	}
	instruments.notificationDeliveryState, err = meter.Int64ObservableGauge(
		"slipramp.notification.delivery.state",
		metric.WithUnit("{delivery}"),
		metric.WithDescription("Retained notification deliveries by state."),
	)
	if err != nil {
		return snapshotInstruments{}, fmt.Errorf("create notification delivery state metric: %w", err)
	}
	instruments.snapshotSuccess, err = meter.Int64ObservableGauge(
		"slipramp.metrics.snapshot.success",
		metric.WithUnit("1"),
		metric.WithDescription("Whether the most recent metrics snapshot refresh succeeded."),
	)
	if err != nil {
		return snapshotInstruments{}, fmt.Errorf("create snapshot success metric: %w", err)
	}
	instruments.snapshotLastSuccessTime, err = meter.Float64ObservableGauge(
		"slipramp.metrics.snapshot.last_success.time",
		metric.WithUnit("s"),
		metric.WithDescription("Unix time of the most recent successful metrics snapshot refresh."),
	)
	if err != nil {
		return snapshotInstruments{}, fmt.Errorf("create snapshot last-success metric: %w", err)
	}
	instruments.databaseConnectionCount, err = dbconv.NewClientConnectionCountObservable(meter)
	if err != nil {
		return snapshotInstruments{}, fmt.Errorf("create database connection count metric: %w", err)
	}
	instruments.databaseConnectionMax, err = dbconv.NewClientConnectionMaxObservable(meter)
	if err != nil {
		return snapshotInstruments{}, fmt.Errorf("create database connection maximum metric: %w", err)
	}
	instruments.databasePoolAcquireWaits, err = meter.Int64ObservableCounter(
		"slipramp.database.pool.acquire.waits",
		metric.WithUnit("{request}"),
		metric.WithDescription("PostgreSQL pool acquires that waited for a connection."),
	)
	if err != nil {
		return snapshotInstruments{}, fmt.Errorf("create database pool acquire waits metric: %w", err)
	}
	instruments.databasePoolCanceledAcquires, err = meter.Int64ObservableCounter(
		"slipramp.database.pool.acquire.canceled",
		metric.WithUnit("{request}"),
		metric.WithDescription("Canceled PostgreSQL pool acquire requests."),
	)
	if err != nil {
		return snapshotInstruments{}, fmt.Errorf("create database pool canceled acquires metric: %w", err)
	}
	instruments.databasePoolAcquireDuration, err = meter.Float64ObservableCounter(
		"slipramp.database.pool.acquire.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Cumulative duration spent acquiring PostgreSQL pool connections."),
	)
	if err != nil {
		return snapshotInstruments{}, fmt.Errorf("create database pool acquire duration metric: %w", err)
	}

	return instruments, nil
}

func (p *snapshotPublisher) Run(ctx context.Context) {
	p.refresh(ctx)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.refresh(ctx)
		}
	}
}

func (p *snapshotPublisher) refresh(ctx context.Context) {
	at := p.now().UTC()
	refreshContext, cancel := context.WithTimeout(ctx, p.timeout)
	snapshot, err := p.store.LoadMetricsSnapshot(refreshContext, at)
	cancel()
	if err != nil {
		if ctx.Err() == nil {
			previous := p.published.Load()
			failed := *previous
			failed.success = false
			p.published.Store(&failed)
			p.logger.Warn("refresh metrics snapshot", "error", err)
		}

		return
	}

	succeededAt := p.now().UTC()
	p.published.Store(&publishedSnapshot{
		state:       snapshot,
		pool:        p.store.DatabasePoolMetrics(),
		success:     true,
		lastSuccess: &succeededAt,
	})
}

func (p *snapshotPublisher) observe(instruments snapshotInstruments) metric.Callback {
	return func(_ context.Context, observer metric.Observer) error {
		snapshot := p.published.Load()
		for _, phase := range []run.Phase{run.PhaseAssigned, run.PhasePreparing, run.PhaseRunning} {
			observer.ObserveInt64(
				instruments.activeRuns,
				snapshot.state.ActiveRuns[string(phase)],
				metric.WithAttributes(attribute.String("phase", string(phase))),
			)
		}
		observer.ObserveInt64(instruments.pendingRunReviews, snapshot.state.PendingRunReviews)
		if snapshot.state.OldestEligibleRunCreatedAt != nil {
			age := max(p.now().UTC().Sub(*snapshot.state.OldestEligibleRunCreatedAt).Seconds(), 0)
			observer.ObserveFloat64(instruments.oldestEligibleRunAge, age)
		}
		observer.ObserveInt64(instruments.connectedControllers, snapshot.state.ConnectedControllers)
		if snapshot.state.ControllerLastSeenAt != nil {
			observer.ObserveFloat64(
				instruments.controllerLastSeenTime,
				float64(snapshot.state.ControllerLastSeenAt.UnixNano())/float64(time.Second),
			)
		}
		for _, state := range []repository.RefreshStatus{
			repository.RefreshStatusPending,
			repository.RefreshStatusSucceeded,
			repository.RefreshStatusFailed,
		} {
			observer.ObserveInt64(
				instruments.repositoryRefreshState,
				snapshot.state.RepositoryRefreshStates[string(state)],
				metric.WithAttributes(attribute.String("state", string(state))),
			)
		}
		for _, state := range []notification.DeliveryState{
			notification.DeliveryStatePending,
			notification.DeliveryStateDelivered,
			notification.DeliveryStateFailed,
		} {
			observer.ObserveInt64(
				instruments.notificationDeliveryState,
				snapshot.state.NotificationDeliveryStates[string(state)],
				metric.WithAttributes(attribute.String("state", string(state))),
			)
		}
		snapshotSuccess := int64(0)
		if snapshot.success {
			snapshotSuccess = 1
		}
		observer.ObserveInt64(instruments.snapshotSuccess, snapshotSuccess)
		if snapshot.lastSuccess != nil {
			observer.ObserveFloat64(
				instruments.snapshotLastSuccessTime,
				float64(snapshot.lastSuccess.UnixNano())/float64(time.Second),
			)
		}

		observer.ObserveInt64(
			instruments.databaseConnectionCount.Inst(),
			snapshot.pool.AcquiredConnections,
			metric.WithAttributes(
				instruments.databaseConnectionCount.AttrClientConnectionPoolName("primary"),
				instruments.databaseConnectionCount.AttrClientConnectionState(dbconv.ClientConnectionStateUsed),
			),
		)
		observer.ObserveInt64(
			instruments.databaseConnectionCount.Inst(),
			snapshot.pool.IdleConnections,
			metric.WithAttributes(
				instruments.databaseConnectionCount.AttrClientConnectionPoolName("primary"),
				instruments.databaseConnectionCount.AttrClientConnectionState(dbconv.ClientConnectionStateIdle),
			),
		)
		observer.ObserveInt64(
			instruments.databaseConnectionMax.Inst(),
			snapshot.pool.MaximumConnections,
			metric.WithAttributes(
				instruments.databaseConnectionMax.AttrClientConnectionPoolName("primary"),
			),
		)
		observer.ObserveInt64(instruments.databasePoolAcquireWaits, snapshot.pool.AcquireWaits)
		observer.ObserveInt64(instruments.databasePoolCanceledAcquires, snapshot.pool.CanceledAcquires)
		observer.ObserveFloat64(
			instruments.databasePoolAcquireDuration,
			snapshot.pool.AcquireDuration.Seconds(),
		)

		return nil
	}
}
