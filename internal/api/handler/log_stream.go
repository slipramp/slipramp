package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/artifact"
)

func (h *Handler) StreamRunLog(
	ctx context.Context,
	request api.StreamRunLogRequestObject,
) (api.StreamRunLogResponseObject, error) {
	offset := int64(0)
	if request.Params.OffsetBytes != nil {
		offset = *request.Params.OffsetBytes
	}
	if request.Params.LastEventID != nil {
		parsed, err := strconv.ParseInt(*request.Params.LastEventID, 10, 64)
		if err != nil || parsed < 0 {
			problem := NewProblem(http.StatusBadRequest, "Last-Event-ID must be a nonnegative byte offset.")
			return api.StreamRunLog400ApplicationProblemPlusJSONResponse(problem), nil
		}
		if request.Params.OffsetBytes == nil {
			offset = parsed
		}
	}

	stream, err := h.artifacts.StreamLog(ctx, request.RunID, offset)
	if errors.Is(err, artifact.ErrInvalid) {
		problem := NewProblem(http.StatusBadRequest, err.Error())
		return api.StreamRunLog400ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, artifact.ErrNotFound) {
		problem := NewProblem(http.StatusNotFound, "The Run log does not exist.")
		return api.StreamRunLog404ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}

	return api.StreamRunLog200TextEventStreamResponse{
		Body: &logEventReader{stream: stream, cursor: offset},
	}, nil
}

// logEventReader formats one chunk at a time so HTTP reads retain backpressure.
// The generated response writer handles flushing and closes the underlying stream.
type logEventReader struct {
	stream   artifact.LogStream
	cursor   int64
	pending  []byte
	finished bool
}

func (r *logEventReader) Read(destination []byte) (int, error) {
	if len(destination) == 0 {
		return 0, nil
	}
	if len(r.pending) == 0 {
		if r.finished {
			return 0, io.EOF
		}

		chunk, err := r.stream.Next()
		switch {
		case err == io.EOF: //nolint:errorlint // Wrapped EOF is a source failure, not stream completion.
			r.pending = fmt.Appendf(nil, "id: %d\nevent: sealed\ndata: {}\n\n", r.cursor)
			r.finished = true
		case err != nil:
			return 0, err
		default:
			encoded, err := json.Marshal(api.LogEvent{
				OffsetBytes:     chunk.OffsetBytes,
				NextOffsetBytes: chunk.NextOffsetBytes,
				ContentsBase64:  chunk.Contents,
			})
			if err != nil {
				return 0, err
			}

			r.pending = fmt.Appendf(nil, "id: %d\nevent: chunk\ndata: %s\n\n", chunk.NextOffsetBytes, encoded)
			r.cursor = chunk.NextOffsetBytes
		}
	}

	written := copy(destination, r.pending)
	r.pending = r.pending[written:]

	return written, nil
}

func (r *logEventReader) Close() error {
	return r.stream.Close()
}
