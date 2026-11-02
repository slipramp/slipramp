package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/slipramp/slipramp/internal/placement"
	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

var _ placement.PreflightStore = (*Database)(nil)

func (d *Database) CreatePlacementPreflight(
	ctx context.Context,
	record placement.Preflight,
) (placement.Preflight, error) {
	snapshot, err := json.Marshal(record.Snapshot)
	if err != nil {
		return placement.Preflight{}, fmt.Errorf("encode Placement preflight snapshot: %w", err)
	}
	actor, err := json.Marshal(record.RequestedBy)
	if err != nil {
		return placement.Preflight{}, fmt.Errorf("encode Placement preflight actor: %w", err)
	}

	row, err := d.queries.CreatePlacementPreflight(ctx, storedb.CreatePlacementPreflightParams{
		ID:                     databaseUUID(record.ID),
		PlacementName:          record.Snapshot.PlacementName,
		ClusterName:            record.Snapshot.ClusterName,
		PlacementSnapshot:      snapshot,
		RequestedByPrincipalID: databaseUUID(record.RequestedBy.PrincipalID),
		RequestedBySnapshot:    actor,
		CreatedAt:              databaseTime(record.CreatedAt),
	})
	if err != nil {
		if constraintMatches(err, "placement_preflights_active_placement_idx") {
			return placement.Preflight{}, placement.ErrPreflightActive
		}
		return placement.Preflight{}, fmt.Errorf("create Placement preflight: %w", err)
	}

	return placementPreflightFromDatabase(row)
}

func (d *Database) ListPlacementPreflights(
	ctx context.Context,
	filter placement.PreflightListFilter,
) (placement.PreflightList, error) {
	total, err := d.queries.CountPlacementPreflights(ctx, filter.PlacementName)
	if err != nil {
		return placement.PreflightList{}, fmt.Errorf("count Placement preflights: %w", err)
	}
	rows, err := d.queries.ListPlacementPreflights(ctx, storedb.ListPlacementPreflightsParams{
		PlacementName: filter.PlacementName,
		ResultLimit:   filter.Limit,
		ResultOffset:  filter.Offset,
	})
	if err != nil {
		return placement.PreflightList{}, fmt.Errorf("list Placement preflights: %w", err)
	}

	items := make([]placement.Preflight, 0, len(rows))
	for _, row := range rows {
		record, err := placementPreflightFromDatabase(row)
		if err != nil {
			return placement.PreflightList{}, err
		}
		items = append(items, record)
	}

	return placement.PreflightList{Items: items, Total: total}, nil
}

func (d *Database) ClaimPlacementPreflight(
	ctx context.Context,
	clusterName string,
	dispatchBefore time.Time,
	dispatchedAt time.Time,
) (placement.Preflight, error) {
	row, err := d.queries.ClaimPlacementPreflight(ctx, storedb.ClaimPlacementPreflightParams{
		ClusterName:    clusterName,
		DispatchBefore: databaseTime(dispatchBefore),
		DispatchedAt:   databaseTime(dispatchedAt),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return placement.Preflight{}, placement.ErrNoPreflightCommand
	}
	if err != nil {
		return placement.Preflight{}, fmt.Errorf("claim Placement preflight: %w", err)
	}

	return placementPreflightFromDatabase(row)
}

func (d *Database) RecordPlacementPreflightReport(
	ctx context.Context,
	report placement.PreflightReport,
) (placement.Preflight, error) {
	transaction, err := d.pool.Begin(ctx)
	if err != nil {
		return placement.Preflight{}, fmt.Errorf("begin Placement preflight report: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()

	queries := d.queries.WithTx(transaction)
	existing, err := queries.GetPlacementPreflightForUpdate(ctx, databaseUUID(report.ID))
	if errors.Is(err, pgx.ErrNoRows) {
		return placement.Preflight{}, placement.ErrPreflightNotFound
	}
	if err != nil {
		return placement.Preflight{}, fmt.Errorf("get Placement preflight for report: %w", err)
	}
	if existing.ClusterName != report.ClusterName {
		return placement.Preflight{}, placement.ErrPreflightNotFound
	}
	current, err := placementPreflightFromDatabase(existing)
	if err != nil {
		return placement.Preflight{}, err
	}
	if err := placement.ValidateReportTransition(current, report); err != nil {
		return placement.Preflight{}, err
	}
	if current.State == placement.PreflightStateSucceeded || current.State == placement.PreflightStateFailed {
		return current, nil
	}

	diagnosticsToStore := report.Diagnostics
	if diagnosticsToStore == nil {
		diagnosticsToStore = []placement.PreflightDiagnostic{}
	}
	diagnostics, err := json.Marshal(diagnosticsToStore)
	if err != nil {
		return placement.Preflight{}, fmt.Errorf("encode Placement preflight diagnostics: %w", err)
	}
	var updated storedb.PlacementPreflight
	if report.State == placement.PreflightStateRunning {
		updated, err = queries.MarkPlacementPreflightRunning(ctx, storedb.MarkPlacementPreflightRunningParams{
			ID:          databaseUUID(report.ID),
			ObservedAt:  databaseTime(report.ObservedAt),
			JobUid:      report.JobUID,
			Diagnostics: diagnostics,
		})
	} else {
		updated, err = queries.FinishPlacementPreflight(ctx, storedb.FinishPlacementPreflightParams{
			ID:           databaseUUID(report.ID),
			State:        string(report.State),
			CleanupState: string(report.CleanupState),
			ObservedAt:   databaseTime(report.ObservedAt),
			JobUid:       report.JobUID,
			Reason:       report.Reason,
			Message:      report.Message,
			Diagnostics:  diagnostics,
		})
	}
	if err != nil {
		return placement.Preflight{}, fmt.Errorf("record Placement preflight report: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return placement.Preflight{}, fmt.Errorf("commit Placement preflight report: %w", err)
	}

	return placementPreflightFromDatabase(updated)
}

func placementPreflightFromDatabase(row storedb.PlacementPreflight) (placement.Preflight, error) {
	var snapshot placement.PreflightSnapshot
	if err := json.Unmarshal(row.PlacementSnapshot, &snapshot); err != nil {
		return placement.Preflight{}, fmt.Errorf("decode Placement preflight snapshot: %w", err)
	}
	var actor productidentity.ActorSnapshot
	if err := json.Unmarshal(row.RequestedBySnapshot, &actor); err != nil {
		return placement.Preflight{}, fmt.Errorf("decode Placement preflight actor: %w", err)
	}
	var diagnostics []placement.PreflightDiagnostic
	if err := json.Unmarshal(row.Diagnostics, &diagnostics); err != nil {
		return placement.Preflight{}, fmt.Errorf("decode Placement preflight diagnostics: %w", err)
	}
	if diagnostics == nil {
		diagnostics = []placement.PreflightDiagnostic{}
	}

	return placement.Preflight{
		ID:               uuid.UUID(row.ID.Bytes),
		Snapshot:         snapshot,
		State:            placement.PreflightState(row.State),
		CleanupState:     placement.PreflightCleanupState(row.CleanupState),
		RequestedBy:      actor,
		CreatedAt:        row.CreatedAt.Time.UTC(),
		StartedAt:        applicationTime(row.StartedAt),
		FinishedAt:       applicationTime(row.FinishedAt),
		LastDispatchedAt: applicationTime(row.LastDispatchedAt),
		JobUID:           cloneStringPointer(row.JobUid),
		Reason:           cloneStringPointer(row.Reason),
		Message:          cloneStringPointer(row.Message),
		Diagnostics:      diagnostics,
	}, nil
}
