package server

import (
	"context"
	"errors"
	"fmt"
)

type serverWorker struct {
	name string
	run  func(context.Context) error
}

func newServerWorker(name string, run func(context.Context)) serverWorker {
	return serverWorker{
		name: name,
		run: func(ctx context.Context) error {
			run(ctx)

			return nil
		},
	}
}

func runServerWorker(ctx context.Context, stop context.CancelCauseFunc, worker serverWorker) {
	err := worker.run(ctx)
	if ctx.Err() != nil {
		return
	}
	if err == nil {
		err = errors.New("returned unexpectedly")
	}

	stop(&serverWorkerError{name: worker.name, cause: err})
}

type serverWorkerError struct {
	name  string
	cause error
}

func (e *serverWorkerError) Error() string {
	return fmt.Sprintf("worker %q failed: %v", e.name, e.cause)
}

func (e *serverWorkerError) Unwrap() error {
	return e.cause
}
