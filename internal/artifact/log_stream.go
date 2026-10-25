package artifact

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/google/uuid"
)

const logStreamPollPeriod = 250 * time.Millisecond

type LogStreamChunk struct {
	OffsetBytes     int64
	NextOffsetBytes int64
	Contents        []byte
}

// LogStream reads durable chunks on demand. Next calls must be serial; Close
// may run concurrently to cancel a pending read. EOF means the log is drained.
type LogStream interface {
	Next() (LogStreamChunk, error)
	Close() error
}

type durableLogStream struct {
	service    *Service
	ctx        context.Context
	cancel     context.CancelFunc
	artifactID uuid.UUID
	cursor     int64
	pending    []LogChunk
	terminal   error
}

func (s *Service) StreamLog(ctx context.Context, runID uuid.UUID, offsetBytes int64) (LogStream, error) {
	if offsetBytes < 0 {
		return nil, invalid("log offset must not be negative")
	}
	record, err := s.artifacts.GetArtifactByRunAndName(ctx, runID, logArtifactName)
	if err != nil {
		return nil, err
	}
	if record.RetentionState != RetentionRetained {
		return nil, ErrNotFound
	}
	if offsetBytes > record.SizeBytes {
		return nil, invalid("log offset exceeds durable size")
	}

	streamContext, cancel := context.WithCancel(ctx)
	stream := &durableLogStream{
		service:    s,
		ctx:        streamContext,
		cancel:     cancel,
		artifactID: record.ID,
		cursor:     offsetBytes,
	}
	return stream, nil
}

func (s *durableLogStream) Next() (LogStreamChunk, error) {
	if s.terminal != nil {
		return LogStreamChunk{}, s.terminal
	}
	chunk, err := s.next()
	if err != nil {
		s.terminal = err
		_ = s.Close()
	}

	return chunk, err
}

func (s *durableLogStream) next() (LogStreamChunk, error) {
	for {
		if err := s.ctx.Err(); err != nil {
			return LogStreamChunk{}, err
		}
		if len(s.pending) == 0 {
			chunks, err := s.service.logChunks.ListLogChunks(s.ctx, s.artifactID, s.cursor)
			if err != nil {
				return LogStreamChunk{}, logStreamSourceError("list log chunks", err)
			}
			s.pending = chunks
		}
		for len(s.pending) > 0 {
			chunk := s.pending[0]
			s.pending = s.pending[1:]
			if chunk.EndOffset() > s.cursor {
				return s.readChunk(chunk)
			}
		}

		record, err := s.service.artifacts.GetArtifact(s.ctx, s.artifactID)
		if err != nil {
			return LogStreamChunk{}, logStreamSourceError("get log artifact", err)
		}
		if err := s.ctx.Err(); err != nil {
			return LogStreamChunk{}, err
		}
		if s.cursor >= record.SizeBytes {
			switch record.State {
			case StateSealed:
				return LogStreamChunk{}, io.EOF
			case StateFailed:
				return LogStreamChunk{}, ErrLogFailed
			}
		}

		timer := time.NewTimer(logStreamPollPeriod)
		select {
		case <-s.ctx.Done():
			timer.Stop()
			return LogStreamChunk{}, s.ctx.Err()
		case <-timer.C:
		}
	}
}

func (s *durableLogStream) readChunk(chunk LogChunk) (LogStreamChunk, error) {
	object, err := s.service.contents.Open(s.ctx, chunk.ObjectKey)
	if err != nil {
		return LogStreamChunk{}, logStreamSourceError("open log chunk", err)
	}
	closeBody := sync.OnceValue(object.Body.Close)
	stopClose := context.AfterFunc(s.ctx, func() { _ = closeBody() })
	contents, readErr := io.ReadAll(object.Body)
	stopClose()
	closeErr := closeBody()
	if err := s.ctx.Err(); err != nil {
		return LogStreamChunk{}, err
	}
	if readErr != nil || closeErr != nil {
		return LogStreamChunk{}, logStreamSourceError("read log chunk", errors.Join(readErr, closeErr))
	}
	if int64(len(contents)) != chunk.SizeBytes {
		return LogStreamChunk{}, fmt.Errorf(
			"log chunk at byte %d has size %d, expected %d",
			chunk.OffsetBytes,
			len(contents),
			chunk.SizeBytes,
		)
	}

	start := max(s.cursor-chunk.OffsetBytes, 0)
	if start > int64(len(contents)) {
		return LogStreamChunk{}, errors.New("log cursor exceeds chunk contents")
	}
	result := LogStreamChunk{OffsetBytes: s.cursor, NextOffsetBytes: chunk.EndOffset(), Contents: contents[start:]}
	s.cursor = result.NextOffsetBytes

	return result, nil
}

func (s *durableLogStream) Close() error {
	s.cancel()

	return nil
}

func logStreamSourceError(operation string, err error) error {
	if errors.Is(err, io.EOF) {
		return fmt.Errorf("%s: %s: %w", operation, err.Error(), io.ErrUnexpectedEOF)
	}

	return fmt.Errorf("%s: %w", operation, err)
}
