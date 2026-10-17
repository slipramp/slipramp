package controller

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
)

func TestSyncLogResumesFromDurableCursorAfterRestart(t *testing.T) {
	t.Parallel()

	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1501")
	reporter := &memoryLogReporter{contents: []byte("already ")}
	jobs := &logRunJobs{contents: []byte("already durable\n")}
	controller := &Controller{jobs: jobs, logs: reporter}
	handle := jobHandle{runID: runID, namespace: "runs", name: runJobName(runID), uid: "job-1"}

	if err := controller.syncLog(context.Background(), handle, true); err != nil {
		t.Fatalf("syncLog() error = %v", err)
	}
	if string(reporter.contents) != "already durable\n" {
		t.Errorf("durable contents = %q", reporter.contents)
	}
	if reporter.appendCalls != 1 || reporter.appendOffsets[0] != 8 {
		t.Errorf("append calls = %d at %v, want one at 8", reporter.appendCalls, reporter.appendOffsets)
	}
	if !reporter.sealed {
		t.Error("log was not sealed")
	}
}

func TestSyncLogRecoversWhenAppendResponseIsLost(t *testing.T) {
	t.Parallel()

	runID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1501")
	reporter := &memoryLogReporter{loseFirstResponse: true}
	jobs := &logRunJobs{contents: []byte("durable\n")}
	controller := &Controller{jobs: jobs, logs: reporter}
	handle := jobHandle{runID: runID, uid: "job-1"}

	if err := controller.syncLog(context.Background(), handle, false); err == nil {
		t.Fatal("first syncLog() error = nil, want lost response")
	}
	if err := controller.syncLog(context.Background(), handle, true); err != nil {
		t.Fatalf("retry syncLog() error = %v", err)
	}
	if reporter.appendCalls != 1 {
		t.Errorf("accepted append calls = %d, want 1", reporter.appendCalls)
	}
	if string(reporter.contents) != "durable\n" || !reporter.sealed {
		t.Errorf("reporter = %#v", reporter)
	}
}

type logRunJobs struct {
	contents []byte
}

func (j *logRunJobs) Ensure(context.Context, api.RunAssignment) (ensuredRunJob, error) {
	return ensuredRunJob{}, nil
}

func (j *logRunJobs) Cancel(context.Context, api.CancelRunCommand) (*string, error) {
	return nil, nil
}

func (j *logRunJobs) Recover(context.Context) ([]jobHandle, error) {
	return nil, nil
}

func (j *logRunJobs) Observe(context.Context, jobHandle) (runObservation, error) {
	return runObservation{}, nil
}

func (j *logRunJobs) ReadLog(context.Context, jobHandle) ([]byte, error) {
	return append([]byte(nil), j.contents...), nil
}

type memoryLogReporter struct {
	contents          []byte
	appendCalls       int
	appendOffsets     []int64
	loseFirstResponse bool
	lostResponse      bool
	sealed            bool
}

func (r *memoryLogReporter) GetLogCursor(
	context.Context,
	uuid.UUID,
) (logCursor, error) {
	return logCursor{nextOffsetBytes: int64(len(r.contents)), sealed: r.sealed}, nil
}

func (r *memoryLogReporter) AppendLogChunk(
	_ context.Context,
	_ uuid.UUID,
	offset int64,
	contents []byte,
) error {
	if offset != int64(len(r.contents)) {
		return errors.New("unexpected offset")
	}
	r.contents = append(r.contents, contents...)
	r.appendCalls++
	r.appendOffsets = append(r.appendOffsets, offset)
	if r.loseFirstResponse && !r.lostResponse {
		r.lostResponse = true
		return errors.New("response lost")
	}

	return nil
}

func (r *memoryLogReporter) SealLog(
	context.Context,
	uuid.UUID,
	int64,
	string,
) error {
	r.sealed = true
	return nil
}
