package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"github.com/google/uuid"
)

const maximumLogChunkBytes = 256 * 1024

type logSummary struct {
	sizeBytes int64
	sha256    string
	sealed    bool
}

type logStreamResult struct {
	summary logSummary
	err     error
}

type logSession struct {
	result <-chan logStreamResult
}

func (c *Controller) startLogSession(
	ctx context.Context,
	handle jobHandle,
	follow bool,
) (*logSession, error) {
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
		result <- logStreamResult{summary: summary, err: streamErr}
	}()

	return &logSession{result: result}, nil
}

func (c *Controller) forwardLog(
	ctx context.Context,
	runID uuid.UUID,
	stream io.Reader,
) (logSummary, error) {
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
	buffer := make([]byte, maximumLogChunkBytes)
	var sourceOffset int64
	for {
		count, readErr := stream.Read(buffer)
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
				if err := c.appendLogChunk(
					ctx,
					runID,
					cursor.nextOffsetBytes,
					contents,
				); err != nil {
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

func (c *Controller) getLogCursor(
	ctx context.Context,
	runID uuid.UUID,
) (logCursor, error) {
	for {
		cursor, err := c.logs.GetLogCursor(ctx, runID)
		if err == nil {
			return cursor, nil
		}
		if permanentLogError(err) {
			return logCursor{}, err
		}
		if !waitForRetry(ctx, c.retryDelay) {
			return logCursor{}, ctx.Err()
		}
	}
}

func (c *Controller) appendLogChunk(
	ctx context.Context,
	runID uuid.UUID,
	offset int64,
	contents []byte,
) error {
	for {
		err := c.logs.AppendLogChunk(ctx, runID, offset, contents)
		if err == nil {
			return nil
		}
		if permanentLogError(err) {
			return err
		}
		if !waitForRetry(ctx, c.retryDelay) {
			return ctx.Err()
		}
	}
}

func (c *Controller) sealLog(
	ctx context.Context,
	runID uuid.UUID,
	summary logSummary,
) error {
	if summary.sealed {
		return nil
	}

	for {
		err := c.logs.SealLog(ctx, runID, summary.sizeBytes, summary.sha256)
		if err == nil {
			return nil
		}
		if permanentLogError(err) {
			return err
		}
		if !waitForRetry(ctx, c.retryDelay) {
			return ctx.Err()
		}
	}
}

func permanentLogError(err error) bool {
	return errors.Is(err, errControllerCredentialRejected) ||
		isUnknownRunRejection(err)
}

func emptyLogSummary() logSummary {
	digest := sha256.Sum256(nil)

	return logSummary{sha256: hex.EncodeToString(digest[:])}
}
