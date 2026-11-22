package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/gate"
	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

func (d *Database) ChangeServerGateWindow(ctx context.Context, owner gate.Scope, actor productidentity.ActorSnapshot,
	definition *gate.WeeklyWindow) (gate.ScopeState, error) {
	var schedule gate.WindowSchedule
	if definition != nil {
		var err error
		schedule, err = definition.Compile()
		if err != nil {
			return gate.ScopeState{}, err
		}
	}
	return d.changeServerGateScope(ctx, owner, func(q *storedb.Queries, row storedb.ServerGateScope, at time.Time) (storedb.ServerGateScope, error) {
		if definition == nil && row.WindowDefinition == nil {
			return row, nil
		}
		params := storedb.SetServerGateWindowParams{ID: row.ID}
		action := "remove"
		if definition != nil {
			data, err := json.Marshal(schedule.Definition)
			if err != nil {
				return row, err
			}
			digest := fmt.Sprintf("%x", sha256.Sum256(data))
			o := schedule.Evaluate(at)
			state := string(o.State)
			params.Definition, params.Digest, params.State = data, &digest, &state
			params.EvaluatedAt, params.ValidUntil = databaseTime(at), databaseTime(o.ValidUntil)
			params.IntervalStart, params.NextTransition = optionalDatabaseTime(o.IntervalStart), optionalDatabaseTime(o.NextTransition)
			action = "set"
		}
		actorID, snapshot, err := actorDatabaseFields(&actor)
		if err != nil {
			return row, err
		}
		updated, err := q.SetServerGateWindow(ctx, params)
		if err != nil {
			return row, fmt.Errorf("set Gate window: %w", err)
		}
		err = q.InsertServerWindowEvent(ctx, storedb.InsertServerWindowEventParams{
			ScopeID: row.ID, Version: updated.WindowVersion, Action: action, ActorID: actorID, Actor: snapshot,
			RecordedAt: databaseTime(at), Definition: params.Definition, Digest: params.Digest,
		})
		return updated, err
	})
}

func windowFromRow(row storedb.ServerGateScope, at time.Time) (*gate.ScopeWindow, error) {
	if row.WindowDefinition == nil {
		return nil, nil
	}
	var def gate.WeeklyWindow
	if err := json.Unmarshal(row.WindowDefinition, &def); err != nil {
		return nil, fmt.Errorf("decode weekly Gate window: %w", err)
	}
	o := gate.WindowObservation{State: gate.RequirementState(*row.WindowState), EvaluatedAt: row.WindowEvaluatedAt.Time.UTC(),
		ValidUntil: row.WindowValidUntil.Time.UTC(), IntervalStart: applicationTime(row.WindowIntervalStart),
		NextTransition: applicationTime(row.WindowNextTransition)}
	return &gate.ScopeWindow{Definition: def, Digest: *row.WindowDigest, Observation: o, State: o.StateAt(at)}, nil
}

func populateScopeWindow(ctx context.Context, q *storedb.Queries, state *gate.ScopeState, row storedb.ServerGateScope, at time.Time) error {
	state.WindowVersion = row.WindowVersion
	window, err := windowFromRow(row, at)
	if err != nil {
		return err
	}
	state.Window = window
	rows, err := q.ListServerWindowEvents(ctx, row.ID)
	if err != nil {
		return err
	}
	for _, e := range rows {
		actor, err := actorFromDatabase(e.ActorID, e.Actor)
		if err != nil {
			return err
		}
		event := gate.WindowEvent{Action: e.Action, Version: e.Version, Actor: *actor, RecordedAt: e.RecordedAt.Time.UTC(), Digest: e.WindowDigest}
		if e.WindowDefinition != nil {
			if err := json.Unmarshal(e.WindowDefinition, &event.Definition); err != nil {
				return err
			}
		}
		state.WindowEvents = append(state.WindowEvents, event)
	}
	return nil
}

func (d *Database) WindowCheckTime(ctx context.Context) (time.Time, error) {
	at, err := d.queries.ServerGateCheckTime(ctx)
	return at.Time.UTC(), err
}

// Re-read policy under its scope locks. A stale batch entry cannot overwrite a
// concurrent edit or resurrect a removed policy. Refreshes preserve its version.
func (d *Database) RefreshServerWindows(ctx context.Context, startedAt time.Time) (time.Duration, error) {
	due, err := d.queries.ListDueServerWindows(ctx, databaseTime(startedAt))
	if err != nil {
		return 0, err
	}
	for _, item := range due {
		owner := gate.Scope{Kind: gate.ScopeKind(item.Kind)}
		switch owner.Kind {
		case gate.TargetScope:
			owner.ID = uuid.UUID(item.TargetID.Bytes)
		case gate.StageScope:
			owner.ID = uuid.UUID(item.StageID.Bytes)
		}
		err := d.withLockedGateScope(ctx, owner, func(q *storedb.Queries, row storedb.ServerGateScope, at time.Time) error {
			window, err := windowFromRow(row, at)
			if err != nil || window == nil {
				return err
			}
			if !window.Observation.EvaluatedAt.Before(startedAt) && window.State != gate.RequirementUnavailable {
				return nil
			}
			schedule, err := window.Definition.Compile()
			if err != nil {
				return err
			}
			o := schedule.Evaluate(at)
			state := string(o.State)
			return q.ObserveServerGateWindow(ctx, storedb.ObserveServerGateWindowParams{ID: row.ID, Version: row.WindowVersion,
				State: &state, EvaluatedAt: databaseTime(at), ValidUntil: databaseTime(o.ValidUntil),
				IntervalStart: optionalDatabaseTime(o.IntervalStart), NextTransition: optionalDatabaseTime(o.NextTransition)})
		})
		if err != nil {
			return 0, err
		}
	}
	if len(due) > 0 {
		return 0, nil
	}
	next, err := d.queries.NextServerWindowCheck(ctx)
	return next.NextCheck.Time.Sub(next.CheckedAt.Time), err
}
