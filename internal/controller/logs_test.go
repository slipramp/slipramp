package controller

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestForwardLogResumesFromDurableCursor(t *testing.T) {
	t.Parallel()

	payload := bytes.Repeat([]byte("log line\n"), 40_000)
	prefixSize := 13
	reporter := &memoryLogReporter{contents: bytes.Clone(payload[:prefixSize])}
	controller := &Controller{logs: reporter, retryDelay: time.Millisecond}

	summary, err := controller.forwardLog(context.Background(), uuid.New(), bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("forwardLog() error = %v", err)
	}
	if !bytes.Equal(reporter.bytes(), payload) {
		t.Fatal("durable log does not match the streamed source")
	}
	if reporter.acceptedAppends != 2 {
		t.Errorf("accepted appends = %d, want 2 bounded chunks", reporter.acceptedAppends)
	}
	wantDigest := sha256.Sum256(payload)
	if summary.sizeBytes != int64(len(payload)) ||
		summary.sha256 != hex.EncodeToString(wantDigest[:]) {
		t.Errorf("log summary = %#v", summary)
	}
}

func TestForwardLogRetriesLostAppendResponseIdempotently(t *testing.T) {
	t.Parallel()

	reporter := &memoryLogReporter{loseFirstAppendResponse: true}
	controller := &Controller{logs: reporter, retryDelay: time.Millisecond}
	payload := []byte("durable after a lost response\n")

	_, err := controller.forwardLog(context.Background(), uuid.New(), bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("forwardLog() error = %v", err)
	}
	if !bytes.Equal(reporter.bytes(), payload) {
		t.Errorf("durable log = %q", reporter.bytes())
	}
	if reporter.appendAttempts != 2 || reporter.acceptedAppends != 1 {
		t.Errorf("append attempts = %d, accepted = %d; want 2, 1", reporter.appendAttempts, reporter.acceptedAppends)
	}
}

type memoryLogReporter struct {
	mutex                   sync.Mutex
	contents                []byte
	sealed                  bool
	sha256                  string
	cursorErrors            []error
	sealErrors              []error
	loseFirstAppendResponse bool
	lostAppendResponse      bool
	appendAttempts          int
	acceptedAppends         int
}

func (r *memoryLogReporter) GetLogCursor(context.Context, uuid.UUID) (logCursor, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if len(r.cursorErrors) > 0 {
		err := r.cursorErrors[0]
		r.cursorErrors = r.cursorErrors[1:]

		return logCursor{}, err
	}

	return logCursor{
		nextOffsetBytes: int64(len(r.contents)),
		sealed:          r.sealed,
		sha256:          r.sha256,
	}, nil
}

func (r *memoryLogReporter) AppendLogChunk(_ context.Context, _ uuid.UUID, offset int64, contents []byte) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.appendAttempts++
	end := offset + int64(len(contents))
	if offset < int64(len(r.contents)) {
		if end > int64(len(r.contents)) ||
			!bytes.Equal(r.contents[offset:end], contents) {
			return errors.New("conflicting repeated append")
		}

		return nil
	}
	if offset != int64(len(r.contents)) {
		return fmt.Errorf("append offset %d, want %d", offset, len(r.contents))
	}

	r.contents = append(r.contents, contents...)
	r.acceptedAppends++
	if r.loseFirstAppendResponse && !r.lostAppendResponse {
		r.lostAppendResponse = true

		return errors.New("response lost")
	}

	return nil
}

func (r *memoryLogReporter) SealLog(_ context.Context, _ uuid.UUID, sizeBytes int64, digest string) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if len(r.sealErrors) > 0 {
		err := r.sealErrors[0]
		r.sealErrors = r.sealErrors[1:]

		return err
	}
	if sizeBytes != int64(len(r.contents)) {
		return fmt.Errorf("seal size %d, want %d", sizeBytes, len(r.contents))
	}
	wantDigest := sha256.Sum256(r.contents)
	if digest != hex.EncodeToString(wantDigest[:]) {
		return errors.New("seal digest does not match durable contents")
	}

	r.sealed = true
	r.sha256 = digest

	return nil
}

func (r *memoryLogReporter) bytes() []byte {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	return bytes.Clone(r.contents)
}

func (r *memoryLogReporter) isSealed() bool {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	return r.sealed
}
