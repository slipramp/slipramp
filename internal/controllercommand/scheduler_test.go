package controllercommand

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/placement"
	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/run"
)

func TestSchedulerSelectsCommandsInPriorityOrder(t *testing.T) {
	t.Parallel()

	record := run.Run{ID: uuid.New()}
	preflight := placement.Preflight{ID: uuid.New()}
	creation := promotion.CreationCommand{PromotionID: uuid.New(), Attempt: 2}
	uid := "promotion-uid"
	reconciliation := promotion.ReconciliationCommand{PromotionID: uuid.New(), KargoUID: &uid}
	failure := errors.New("source unavailable")
	tests := []struct {
		name              string
		cancelError       error
		preflightReady    bool
		preflightError    error
		creationError     error
		reconcileError    error
		assignmentError   error
		withoutCreations  bool
		withoutReconciles bool
		want              Command
		wantError         error
		wantCalls         string
	}{
		{
			name: "cancellation before other work", want: Cancellation{Run: record}, wantCalls: "cancel",
		},
		{
			name: "preflight after cancellation", cancelError: run.ErrNoRunToCancel,
			preflightReady: true, want: PlacementPreflight{Preflight: preflight}, wantCalls: "cancel,preflight",
		},
		{
			name: "creation before reconciliation and assignment", cancelError: run.ErrNoRunToCancel,
			want: PromotionCreation{Command: creation}, wantCalls: "cancel,preflight,create",
		},
		{
			name: "reconciliation before assignment", cancelError: run.ErrNoRunToCancel,
			creationError: promotion.ErrNoCreationDispatch,
			want:          PromotionReconciliation{Command: reconciliation}, wantCalls: "cancel,preflight,create,reconcile",
		},
		{
			name: "assignment", cancelError: run.ErrNoRunToCancel, creationError: promotion.ErrNoCreationDispatch,
			reconcileError: promotion.ErrNoReconciliation,
			want:           Assignment{Run: record}, wantCalls: "cancel,preflight,create,reconcile,assign",
		},
		{
			name: "no work", cancelError: run.ErrNoRunToCancel, creationError: promotion.ErrNoCreationDispatch,
			reconcileError: promotion.ErrNoReconciliation, assignmentError: run.ErrNoRunAssignment,
			wantCalls: "cancel,preflight,create,reconcile,assign",
		},
		{
			name: "cancellation failure stops selection", cancelError: failure,
			wantError: failure, wantCalls: "cancel",
		},
		{
			name: "preflight failure stops selection", cancelError: run.ErrNoRunToCancel, preflightError: failure,
			wantError: failure, wantCalls: "cancel,preflight",
		},
		{
			name: "creation failure stops selection", cancelError: run.ErrNoRunToCancel, creationError: failure,
			wantError: failure, wantCalls: "cancel,preflight,create",
		},
		{
			name: "reconciliation failure stops selection", cancelError: run.ErrNoRunToCancel,
			creationError: promotion.ErrNoCreationDispatch, reconcileError: failure,
			wantError: failure, wantCalls: "cancel,preflight,create,reconcile",
		},
		{
			name: "assignment failure", cancelError: run.ErrNoRunToCancel,
			creationError: promotion.ErrNoCreationDispatch, reconcileError: promotion.ErrNoReconciliation,
			assignmentError: failure, wantError: failure, wantCalls: "cancel,preflight,create,reconcile,assign",
		},
		{
			name: "optional creation source", cancelError: run.ErrNoRunToCancel, withoutCreations: true,
			want: PromotionReconciliation{Command: reconciliation}, wantCalls: "cancel,preflight,reconcile",
		},
		{
			name: "optional reconciliation source", cancelError: run.ErrNoRunToCancel,
			creationError: promotion.ErrNoCreationDispatch, withoutReconciles: true,
			want: Assignment{Run: record}, wantCalls: "cancel,preflight,create,assign",
		},
		{
			name: "no Promotion sources", cancelError: run.ErrNoRunToCancel,
			withoutCreations: true, withoutReconciles: true,
			want: Assignment{Run: record}, wantCalls: "cancel,preflight,assign",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			var calls []string
			recordCall := func(callCtx context.Context, cluster, operation string) {
				t.Helper()
				if callCtx != ctx || cluster != "colo" {
					t.Errorf("%s received context %v and Cluster %q", operation, callCtx, cluster)
				}
				calls = append(calls, operation)
			}
			runs := schedulerRunSource{
				cancel: func(ctx context.Context, cluster string) (run.Run, error) {
					recordCall(ctx, cluster, "cancel")
					return record, test.cancelError
				},
				assign: func(ctx context.Context, cluster string) (run.Run, error) {
					recordCall(ctx, cluster, "assign")
					return record, test.assignmentError
				},
			}
			preflights := schedulerPreflightSource(func(
				ctx context.Context,
				cluster string,
			) (placement.Preflight, error) {
				recordCall(ctx, cluster, "preflight")
				if test.preflightReady {
					return preflight, nil
				}
				if test.preflightError != nil {
					return placement.Preflight{}, test.preflightError
				}

				return placement.Preflight{}, placement.ErrNoPreflightCommand
			})
			var creations PromotionCreationSource
			if !test.withoutCreations {
				creations = schedulerCreationSource(func(ctx context.Context, cluster string) (promotion.CreationCommand, error) {
					recordCall(ctx, cluster, "create")
					return creation, test.creationError
				})
			}
			var reconciliations PromotionReconciliationSource
			if !test.withoutReconciles {
				reconciliations = schedulerReconciliationSource(func(
					ctx context.Context,
					cluster string,
				) (promotion.ReconciliationCommand, error) {
					recordCall(ctx, cluster, "reconcile")
					return reconciliation, test.reconcileError
				})
			}

			got, err := NewScheduler(runs, preflights, creations, reconciliations).Next(ctx, "colo")
			if !errors.Is(err, test.wantError) || !reflect.DeepEqual(got, test.want) {
				t.Errorf("Next() = %#v, %v; want %#v, %v", got, err, test.want, test.wantError)
			}
			if gotCalls := strings.Join(calls, ","); gotCalls != test.wantCalls {
				t.Errorf("source calls = %q, want %q", gotCalls, test.wantCalls)
			}
		})
	}
}

type schedulerRunSource struct {
	cancel func(context.Context, string) (run.Run, error)
	assign func(context.Context, string) (run.Run, error)
}

func (s schedulerRunSource) NextRunToCancel(ctx context.Context, cluster string) (run.Run, error) {
	return s.cancel(ctx, cluster)
}

func (s schedulerRunSource) NextRunAssignment(ctx context.Context, cluster string) (run.Run, error) {
	return s.assign(ctx, cluster)
}

type schedulerCreationSource func(context.Context, string) (promotion.CreationCommand, error)

type schedulerPreflightSource func(context.Context, string) (placement.Preflight, error)

func (s schedulerPreflightSource) Next(ctx context.Context, cluster string) (placement.Preflight, error) {
	return s(ctx, cluster)
}

func (s schedulerCreationSource) Next(ctx context.Context, cluster string) (promotion.CreationCommand, error) {
	return s(ctx, cluster)
}

type schedulerReconciliationSource func(context.Context, string) (promotion.ReconciliationCommand, error)

func (s schedulerReconciliationSource) Next(
	ctx context.Context,
	cluster string,
) (promotion.ReconciliationCommand, error) {
	return s(ctx, cluster)
}
