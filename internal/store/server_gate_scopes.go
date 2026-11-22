package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/slipramp/slipramp/internal/gate"
	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

func (d *Database) GetServerGateScope(ctx context.Context, owner gate.Scope) (gate.ScopeState, error) {
	// Read the current policy and its recent history from the same snapshot.
	tx, err := d.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return gate.ScopeState{}, fmt.Errorf("begin Gate scope read: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	queries := d.queries.WithTx(tx)
	row, err := queries.GetServerGateScope(ctx, storedb.GetServerGateScopeParams{
		Kind: string(owner.Kind), OwnerID: databaseUUID(owner.ID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return gate.ScopeState{}, gate.ErrScopeNotFound
	}
	if err != nil {
		return gate.ScopeState{}, fmt.Errorf("read Gate scope: %w", err)
	}
	return serverGateScopeState(ctx, queries, owner, row.ServerGateScope, row.CheckedAt.Time.UTC())
}

func (d *Database) ChangeServerGateLock(ctx context.Context, owner gate.Scope, actor productidentity.ActorSnapshot,
	request *gate.LockRequest) (gate.ScopeState, error) {
	return d.changeServerGateScope(ctx, owner, func(q *storedb.Queries, row storedb.ServerGateScope, at time.Time) (storedb.ServerGateScope, error) {
		return changeScopeLock(ctx, q, row, actor, request, at)
	})
}

type scopePolicyChange func(*storedb.Queries, storedb.ServerGateScope, time.Time) (storedb.ServerGateScope, error)

func (d *Database) changeServerGateScope(ctx context.Context, owner gate.Scope, change scopePolicyChange) (gate.ScopeState, error) {
	var state gate.ScopeState
	err := d.withLockedGateScope(ctx, owner, func(q *storedb.Queries, row storedb.ServerGateScope, at time.Time) error {
		updated, err := change(q, row, at)
		if err != nil {
			return err
		}
		state, err = serverGateScopeState(ctx, q, owner, updated, at)
		return err
	})
	return state, err
}

func (d *Database) withLockedGateScope(ctx context.Context, owner gate.Scope,
	apply func(*storedb.Queries, storedb.ServerGateScope, time.Time) error) error {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin Gate scope change: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	q := d.queries.WithTx(tx)
	rows, err := q.LockServerGateScopes(ctx, storedb.LockServerGateScopesParams{Kind: string(owner.Kind), OwnerID: databaseUUID(owner.ID)})
	if err != nil {
		return fmt.Errorf("lock Gate scopes: %w", err)
	}
	if len(rows) == 0 || rows[len(rows)-1].Kind != string(owner.Kind) {
		return gate.ErrScopeNotFound
	}
	at, err := q.ServerGateCheckTime(ctx)
	if err != nil {
		return fmt.Errorf("read Gate check time: %w", err)
	}
	if err := apply(q, rows[len(rows)-1], at.Time.UTC()); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func serverGateLockActive(row storedb.ServerGateScope, at time.Time) bool {
	return row.LockedAt.Valid && (!row.LockExpiresAt.Valid || row.LockExpiresAt.Time.After(at))
}

func changeScopeLock(ctx context.Context, queries *storedb.Queries, row storedb.ServerGateScope,
	actor productidentity.ActorSnapshot, request *gate.LockRequest, at time.Time) (storedb.ServerGateScope, error) {
	active := serverGateLockActive(row, at)
	if request == nil && !active {
		return row, nil
	}

	actorID, snapshot, err := actorDatabaseFields(&actor)
	if err != nil {
		return row, err
	}
	update := storedb.UpdateServerGateLockParams{ID: row.ID}
	event := storedb.InsertServerGateLockEventParams{
		ScopeID: row.ID, ActorID: actorID, Actor: snapshot, RecordedAt: databaseTime(at),
	}
	if request != nil {
		expiry := optionalDatabaseTime(request.ExpiresAt)
		if expiry.Valid {
			expiry.Time = expiry.Time.UTC().Truncate(time.Microsecond)
			if !expiry.Time.After(at) {
				return row, gate.ErrLockExpiry
			}
		}
		if active {
			if row.LockActorID == actorID && *row.LockReason == request.Reason &&
				row.LockExpiresAt.Valid == expiry.Valid && row.LockExpiresAt.Time.Equal(expiry.Time) {
				return row, nil
			}
			return row, gate.ErrLockConflict
		}
		update.Reason, update.ActorID, update.Actor = &request.Reason, actorID, snapshot
		update.LockedAt, update.ExpiresAt = event.RecordedAt, expiry
		event.Action, event.Reason, event.ExpiresAt = "lock", &request.Reason, expiry
	} else {
		event.Action, event.Reason = "unlock", row.LockReason
	}
	updated, err := queries.UpdateServerGateLock(ctx, update)
	if err != nil {
		return row, fmt.Errorf("update Gate lock: %w", err)
	}
	event.LockVersion = updated.LockVersion
	if err := queries.InsertServerGateLockEvent(ctx, event); err != nil {
		return row, fmt.Errorf("record Gate lock event: %w", err)
	}
	return updated, nil
}

// The caller has established that the scope contains a lock.
func scopeLockFromRow(row storedb.ServerGateScope) (*gate.ScopeLock, error) {
	actor, err := actorFromDatabase(row.LockActorID, row.LockActor)
	if err != nil {
		return nil, err
	}
	return &gate.ScopeLock{
		Actor: *actor, Reason: *row.LockReason,
		CreatedAt: row.LockedAt.Time.UTC(), ExpiresAt: applicationTime(row.LockExpiresAt),
	}, nil
}

func serverGateScopeState(ctx context.Context, queries *storedb.Queries, owner gate.Scope,
	row storedb.ServerGateScope, at time.Time) (gate.ScopeState, error) {
	state := gate.ScopeState{ID: uuid.UUID(row.ID.Bytes), Owner: owner, CheckedAt: at, LockVersion: row.LockVersion}
	if serverGateLockActive(row, at) {
		lock, err := scopeLockFromRow(row)
		if err != nil {
			return state, err
		}
		state.Lock = lock
	}
	events, err := queries.ListServerGateScopeEvents(ctx, row.ID)
	if err != nil {
		return state, fmt.Errorf("read Gate scope history: %w", err)
	}
	for _, event := range events {
		actor, err := actorFromDatabase(event.ActorID, event.Actor)
		if err != nil {
			return state, err
		}
		state.Events = append(state.Events, gate.LockEvent{
			Action: event.Action, Version: event.Version, Actor: *actor, Reason: *event.Reason,
			RecordedAt: event.RecordedAt.Time.UTC(), ExpiresAt: applicationTime(event.ExpiresAt),
		})
	}
	err = populateScopeWindow(ctx, queries, &state, row, at)
	return state, err
}
