package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
)

const (
	maximumLogChunkBytes = 256 * 1024
	logChunkFlushPeriod  = 100 * time.Millisecond
)

type logSummary struct {
	sizeBytes int64
	sha256    string
	sealed    bool
	failed    bool
}

type logStreamResult struct {
	summary logSummary
	err     error
}

type logSession struct {
	result <-chan logStreamResult
}

type logReadResult struct {
	contents []byte
	err      error
}

// batchingLogReader keeps live logs responsive while avoiding one durable
// object for every small read returned by the Kubernetes log stream.
type batchingLogReader struct {
	ctx        context.Context
	cancel     context.CancelFunc
	results    <-chan logReadResult
	pending    []byte
	pendingErr error
	terminal   error
	period     time.Duration
}

func (c *Controller) startLogSession(ctx context.Context, handle jobHandle, follow bool) (*logSession, error) {
	stream, err := c.jobs.OpenLog(ctx, handle, follow)
	if err != nil {
		return nil, err
	}

	result := make(chan logStreamResult, 1)
	go func() {
		summary, streamErr := c.forwardLog(ctx, handle.runID, stream)
		closeErr := stream.Close()
		if streamErr == nil && closeErr != nil {
			streamErr = fmt.Errorf("close execution log stream: %w", closeErr)
		}
		if errors.Is(streamErr, errControllerCredentialRejected) && c.stop != nil {
			c.stop(streamErr)
		}
		result <- logStreamResult{summary: summary, err: streamErr}
	}()

	return &logSession{result: result}, nil
}

func (c *Controller) forwardLog(ctx context.Context, runID uuid.UUID, stream io.Reader) (logSummary, error) {
	cursor, err := c.getLogCursor(ctx, runID)
	if err != nil {
		return logSummary{}, err
	}
	if cursor.sealed {
		if cursor.sha256 == "" {
			return logSummary{}, errors.New("sealed Run log has no digest")
		}

		return logSummary{
			sizeBytes: cursor.nextOffsetBytes,
			sha256:    cursor.sha256,
			sealed:    true,
		}, nil
	}

	digest := sha256.New()
	batchedStream := newBatchingLogReader(ctx, stream, logChunkFlushPeriod)
	defer batchedStream.Close()
	buffer := make([]byte, maximumLogChunkBytes)
	var sourceOffset int64
	for {
		count, readErr := batchedStream.Read(buffer)
		if count > 0 {
			chunk := buffer[:count]
			_, _ = digest.Write(chunk)

			nextSourceOffset := sourceOffset + int64(count)
			if nextSourceOffset > cursor.nextOffsetBytes {
				chunkStart := 0
				if cursor.nextOffsetBytes > sourceOffset {
					chunkStart = int(cursor.nextOffsetBytes - sourceOffset)
				}

				contents := chunk[chunkStart:]
				if err := c.appendLogChunk(ctx, runID, cursor.nextOffsetBytes, contents); err != nil {
					return logSummary{}, err
				}
				cursor.nextOffsetBytes += int64(len(contents))
			}
			sourceOffset = nextSourceOffset
		}

		if errors.Is(readErr, io.EOF) {
			if sourceOffset < cursor.nextOffsetBytes {
				return logSummary{}, fmt.Errorf(
					"execution log ended at byte %d before durable byte %d",
					sourceOffset,
					cursor.nextOffsetBytes,
				)
			}

			return logSummary{
				sizeBytes: sourceOffset,
				sha256:    hex.EncodeToString(digest.Sum(nil)),
			}, nil
		}
		if readErr != nil {
			return logSummary{}, fmt.Errorf("read execution log stream: %w", readErr)
		}
	}
}

func newBatchingLogReader(ctx context.Context, source io.Reader, period time.Duration) *batchingLogReader {
	readerContext, cancel := context.WithCancel(ctx)
	results := make(chan logReadResult)
	reader := &batchingLogReader{
		ctx:     readerContext,
		cancel:  cancel,
		results: results,
		period:  period,
	}

	go func() {
		defer close(results)

		buffer := make([]byte, maximumLogChunkBytes)
		for {
			count, err := source.Read(buffer)
			result := logReadResult{err: err}
			if count > 0 {
				result.contents = append([]byte(nil), buffer[:count]...)
			}
			if count > 0 || err != nil {
				select {
				case results <- result:
				case <-readerContext.Done():
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	return reader
}

func (r *batchingLogReader) Read(destination []byte) (int, error) {
	if len(destination) == 0 {
		return 0, nil
	}
	if r.terminal != nil {
		return 0, r.terminal
	}

	written, err := r.copyPending(destination)
	if err != nil || written == len(destination) {
		return written, err
	}

	var result logReadResult
	var ok bool
	select {
	case <-r.ctx.Done():
		r.terminal = r.ctx.Err()
		return written, r.terminal
	case result, ok = <-r.results:
		if !ok {
			r.terminal = io.EOF
			return written, r.terminal
		}
	}
	written, err = r.copyResult(destination, written, result)
	if err != nil || written == len(destination) {
		return written, err
	}

	timer := time.NewTimer(r.period)
	defer timer.Stop()
	for written < len(destination) {
		select {
		case <-timer.C:
			return written, nil
		default:
		}

		select {
		case <-r.ctx.Done():
			r.terminal = r.ctx.Err()
			return written, r.terminal
		case <-timer.C:
			return written, nil
		case result, ok = <-r.results:
			if !ok {
				r.terminal = io.EOF
				return written, r.terminal
			}
			written, err = r.copyResult(destination, written, result)
			if err != nil {
				return written, err
			}
		}
	}

	return written, nil
}

func (r *batchingLogReader) copyPending(destination []byte) (int, error) {
	written := copy(destination, r.pending)
	r.pending = r.pending[written:]
	if len(r.pending) == 0 && r.pendingErr != nil {
		r.terminal = r.pendingErr
		r.pendingErr = nil
		return written, r.terminal
	}

	return written, nil
}

func (r *batchingLogReader) copyResult(
	destination []byte,
	written int,
	result logReadResult,
) (int, error) {
	copied := copy(destination[written:], result.contents)
	written += copied
	if copied < len(result.contents) {
		r.pending = result.contents[copied:]
		r.pendingErr = result.err
		return written, nil
	}
	if result.err != nil {
		r.terminal = result.err
		return written, result.err
	}

	return written, nil
}

func (r *batchingLogReader) Close() {
	r.cancel()
}

func (c *Controller) getLogCursor(ctx context.Context, runID uuid.UUID) (logCursor, error) {
	for {
		cursor, err := c.logs.GetLogCursor(ctx, runID)
		if err == nil {
			return cursor, nil
		}
		if permanentServerError(err) {
			return logCursor{}, err
		}
		if !waitForRetry(ctx, c.retryDelay) {
			return logCursor{}, ctx.Err()
		}
	}
}

func (c *Controller) appendLogChunk(ctx context.Context, runID uuid.UUID, offset int64, contents []byte) error {
	for {
		err := c.logs.AppendLogChunk(ctx, runID, offset, contents)
		if err == nil {
			return nil
		}
		if permanentServerError(err) {
			return err
		}
		if !waitForRetry(ctx, c.retryDelay) {
			return ctx.Err()
		}
	}
}

func (c *Controller) sealLog(ctx context.Context, runID uuid.UUID, summary logSummary) error {
	if summary.sealed || summary.failed {
		return nil
	}

	for {
		err := c.logs.SealLog(ctx, runID, summary.sizeBytes, summary.sha256)
		if err == nil {
			return nil
		}
		if permanentServerError(err) {
			return err
		}
		if !waitForRetry(ctx, c.retryDelay) {
			return ctx.Err()
		}
	}
}

func (c *Controller) failLog(ctx context.Context, runID uuid.UUID) error {
	for {
		err := c.logs.FailLog(ctx, runID)
		if err == nil {
			return nil
		}
		if permanentServerError(err) {
			return err
		}
		if !waitForRetry(ctx, c.retryDelay) {
			return ctx.Err()
		}
	}
}

func emptyLogSummary() logSummary {
	digest := sha256.Sum256(nil)

	return logSummary{sha256: hex.EncodeToString(digest[:])}
}
