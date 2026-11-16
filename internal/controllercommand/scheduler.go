// Package controllercommand selects the next domain operation for a Controller.
package controllercommand

import (
	"context"
	"errors"
	"sync"

	"github.com/slipramp/slipramp/internal/placement"
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

type PlacementPreflightSource interface {
	Next(context.Context, string) (placement.Preflight, error)
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
type PlacementPreflight struct{ Preflight placement.Preflight }

func (Assignment) controllerCommand()              {}
func (Cancellation) controllerCommand()            {}
func (PromotionCreation) controllerCommand()       {}
func (PromotionReconciliation) controllerCommand() {}
func (PlacementPreflight) controllerCommand()      {}

type Scheduler struct {
	runs            RunSource
	preflights      PlacementPreflightSource
	creations       PromotionCreationSource
	reconciliations PromotionReconciliationSource
	clustersMutex   sync.Mutex
	clusters        map[string]*clusterSchedule
}

type clusterSchedule struct {
	mutex              sync.Mutex
	yieldCancellation  bool
	nextOrdinarySource ordinarySource
}

type ordinarySource uint8

const (
	ordinaryPromotionCreation ordinarySource = iota
	ordinaryPromotionReconciliation
	ordinaryRunAssignment
	ordinarySourceCount
)

func NewScheduler(
	runs RunSource,
	preflights PlacementPreflightSource,
	creations PromotionCreationSource,
	reconciliations PromotionReconciliationSource,
) *Scheduler {
	return &Scheduler{
		runs:            runs,
		preflights:      preflights,
		creations:       creations,
		reconciliations: reconciliations,
		clusters:        make(map[string]*clusterSchedule),
	}
}

func (s *Scheduler) Next(ctx context.Context, clusterName string) (Command, error) {
	schedule := s.clusterSchedule(clusterName)
	schedule.mutex.Lock()
	defer schedule.mutex.Unlock()

	if !schedule.yieldCancellation {
		command, err := s.nextCancellation(ctx, clusterName)
		if err != nil || command != nil {
			if command != nil {
				schedule.yieldCancellation = true
			}

			return command, err
		}
	}

	command, err := s.nextNonCancellation(ctx, clusterName, schedule)
	if err != nil {
		schedule.yieldCancellation = false
		return nil, err
	}
	if command != nil {
		schedule.yieldCancellation = false
		return command, nil
	}
	if !schedule.yieldCancellation {
		return nil, nil
	}

	command, err = s.nextCancellation(ctx, clusterName)
	if err == nil && command == nil {
		schedule.yieldCancellation = false
	}

	return command, err
}

func (s *Scheduler) nextNonCancellation(
	ctx context.Context,
	clusterName string,
	schedule *clusterSchedule,
) (Command, error) {
	if s.preflights != nil {
		preflight, err := s.preflights.Next(ctx, clusterName)
		if err == nil {
			return PlacementPreflight{Preflight: preflight}, nil
		}
		if !errors.Is(err, placement.ErrNoPreflightCommand) {
			return nil, err
		}
	}

	for offset := range int(ordinarySourceCount) {
		source := (schedule.nextOrdinarySource + ordinarySource(offset)) % ordinarySourceCount
		command, err := s.nextOrdinary(ctx, clusterName, source)
		if err != nil {
			schedule.nextOrdinarySource = (source + 1) % ordinarySourceCount
			return nil, err
		}
		if command != nil {
			schedule.nextOrdinarySource = (source + 1) % ordinarySourceCount
			return command, nil
		}
	}

	return nil, nil
}

func (s *Scheduler) nextOrdinary(
	ctx context.Context,
	clusterName string,
	source ordinarySource,
) (Command, error) {
	switch source {
	case ordinaryPromotionCreation:
		if s.creations == nil {
			return nil, nil
		}
		command, err := s.creations.Next(ctx, clusterName)
		if errors.Is(err, promotion.ErrNoCreationDispatch) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}

		return PromotionCreation{Command: command}, nil
	case ordinaryPromotionReconciliation:
		if s.reconciliations == nil {
			return nil, nil
		}
		command, err := s.reconciliations.Next(ctx, clusterName)
		if errors.Is(err, promotion.ErrNoReconciliation) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}

		return PromotionReconciliation{Command: command}, nil
	case ordinaryRunAssignment:
		return s.nextRunAssignment(ctx, clusterName)
	default:
		panic("unsupported ordinary Controller command source")
	}
}

func (s *Scheduler) nextCancellation(ctx context.Context, clusterName string) (Command, error) {
	record, err := s.runs.NextRunToCancel(ctx, clusterName)
	if errors.Is(err, run.ErrNoRunToCancel) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return Cancellation{Run: record}, nil
}

func (s *Scheduler) nextRunAssignment(ctx context.Context, clusterName string) (Command, error) {
	record, err := s.runs.NextRunAssignment(ctx, clusterName)
	if errors.Is(err, run.ErrNoRunAssignment) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return Assignment{Run: record}, nil
}

func (s *Scheduler) clusterSchedule(clusterName string) *clusterSchedule {
	s.clustersMutex.Lock()
	defer s.clustersMutex.Unlock()

	schedule := s.clusters[clusterName]
	if schedule == nil {
		schedule = &clusterSchedule{}
		s.clusters[clusterName] = schedule
	}

	return schedule
}
