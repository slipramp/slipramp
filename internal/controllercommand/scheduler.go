// Package controllercommand selects the next domain operation for a Controller.
package controllercommand

import (
	"context"
	"errors"

	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/run"
)

type RunSource interface {
	NextRunToCancel(context.Context, string) (run.Run, error)
	NextRunAssignment(context.Context, string) (run.Run, error)
}

type PromotionCreationSource interface {
	Next(context.Context, string) (promotion.CreationCommand, error)
}

type PromotionReconciliationSource interface {
	Next(context.Context, string) (promotion.ReconciliationCommand, error)
}

// Command identifies the selected operation and retains its domain payload.
// A nil Command means no work is available.
type Command interface {
	controllerCommand()
}

type Assignment struct{ Run run.Run }
type Cancellation struct{ Run run.Run }
type PromotionCreation struct{ Command promotion.CreationCommand }
type PromotionReconciliation struct {
	Command promotion.ReconciliationCommand
}

func (Assignment) controllerCommand()              {}
func (Cancellation) controllerCommand()            {}
func (PromotionCreation) controllerCommand()       {}
func (PromotionReconciliation) controllerCommand() {}

type Scheduler struct {
	runs            RunSource
	creations       PromotionCreationSource
	reconciliations PromotionReconciliationSource
}

func NewScheduler(
	runs RunSource,
	creations PromotionCreationSource,
	reconciliations PromotionReconciliationSource,
) *Scheduler {
	return &Scheduler{runs: runs, creations: creations, reconciliations: reconciliations}
}

func (s *Scheduler) Next(ctx context.Context, clusterName string) (Command, error) {
	record, err := s.runs.NextRunToCancel(ctx, clusterName)
	if err == nil {
		return Cancellation{Run: record}, nil
	}
	if !errors.Is(err, run.ErrNoRunToCancel) {
		return nil, err
	}

	if s.creations != nil {
		command, err := s.creations.Next(ctx, clusterName)
		if err == nil {
			return PromotionCreation{Command: command}, nil
		}
		if !errors.Is(err, promotion.ErrNoCreationDispatch) {
			return nil, err
		}
	}

	if s.reconciliations != nil {
		command, err := s.reconciliations.Next(ctx, clusterName)
		if err == nil {
			return PromotionReconciliation{Command: command}, nil
		}
		if !errors.Is(err, promotion.ErrNoReconciliation) {
			return nil, err
		}
	}

	record, err = s.runs.NextRunAssignment(ctx, clusterName)
	if errors.Is(err, run.ErrNoRunAssignment) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return Assignment{Run: record}, nil
}
