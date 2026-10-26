package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/slipramp/slipramp/internal/api"
)

const (
	maximumSSELineBytes = 16 << 20
	logReconnectDelay   = 100 * time.Millisecond
)

var errLogOutput = errors.New("write log output")

type logStreamResult int

const (
	logStreamDisconnected logStreamResult = iota
	logStreamSealed
	logStreamFailed
)

type runLogsOptions struct {
	serverURL string
}

func (a App) newRunLogsCommand(exitCode *int) *cobra.Command {
	options := runLogsOptions{}
	command := &cobra.Command{
		Use:   "logs RUN_ID",
		Short: "Follow a Run's durable log",
		Args:  requireRunIdentity,
		Run: func(_ *cobra.Command, arguments []string) {
			*exitCode = a.runRunLogs(options, arguments[0])
		},
	}
	command.Flags().StringVar(&options.serverURL, "server", "", "Slipramp Server URL (default: SLIPRAMP_URL)")

	return command
}

func (a App) runRunLogs(options runLogsOptions, identity string) int {
	runID, err := parseRunIdentity(identity)
	if err != nil {
		return a.invocationError(err.Error())
	}

	client, err := a.remoteClient(options.serverURL)
	if err != nil {
		return a.invocationError(err.Error())
	}
	ctx := a.commandContext()
	offset := int64(0)
	connected := false
	for {
		params := api.StreamRunLogParams{OffsetBytes: &offset}
		response, requestErr := client.API.StreamRunLog(ctx, runID, &params)
		if requestErr != nil {
			if ctx.Err() != nil {
				return a.remoteError(fmt.Errorf("follow Run log: canceled: %w", ctx.Err()))
			}
			if !connected {
				return a.remoteError(client.NetworkError("follow Run log", requestErr))
			}
			if waitErr := a.waitForLogReconnect(ctx, offset); waitErr != nil {
				return a.logReconnectError(waitErr)
			}

			continue
		}

		if response.StatusCode != http.StatusOK {
			body, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
			_ = response.Body.Close()
			if readErr != nil {
				return a.remoteError(client.InvalidResponse("follow Run log", readErr))
			}
			if connected && retryableLogStatus(response.StatusCode) {
				if waitErr := a.waitForLogReconnect(ctx, offset); waitErr != nil {
					return a.logReconnectError(waitErr)
				}

				continue
			}

			return a.remoteError(client.ResponseError("follow Run log", response.StatusCode, body))
		}
		mediaType, _, mediaErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
		if mediaErr != nil || mediaType != "text/event-stream" {
			_ = response.Body.Close()

			return a.remoteError(errors.New("follow Run log: invalid Server response: expected text/event-stream"))
		}

		connected = true
		result, streamErr := consumeLogStream(response.Body, a.Stdout, &offset)
		closeErr := response.Body.Close()
		if streamErr != nil {
			if ctx.Err() != nil {
				return a.remoteError(fmt.Errorf("follow Run log: canceled: %w", ctx.Err()))
			}
			if errors.Is(streamErr, errLogOutput) {
				return a.internalError("write Run log", streamErr)
			}

			return a.remoteError(client.InvalidResponse("follow Run log", streamErr))
		}
		if closeErr != nil {
			return a.remoteError(client.NetworkError("close Run log stream", closeErr))
		}

		switch result {
		case logStreamSealed:
			return ExitValid
		case logStreamFailed:
			if _, err := fmt.Fprintf(
				a.Stderr,
				"slipramp: Run log is incomplete: the Server retained %d bytes before collection failed\n",
				offset,
			); err != nil {
				return ExitInternal
			}

			return ExitIncomplete
		case logStreamDisconnected:
			if waitErr := a.waitForLogReconnect(ctx, offset); waitErr != nil {
				return a.logReconnectError(waitErr)
			}
		}
	}
}

func (a App) waitForLogReconnect(ctx context.Context, offset int64) error {
	if _, err := fmt.Fprintf(
		a.Stderr,
		"slipramp: Run log stream disconnected; reconnecting from byte %d\n",
		offset,
	); err != nil {
		return fmt.Errorf("write reconnect diagnostic: %w", err)
	}
	timer := time.NewTimer(logReconnectDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (a App) logReconnectError(err error) int {
	if a.commandContext().Err() != nil {
		return a.remoteError(fmt.Errorf("follow Run log: canceled: %w", err))
	}

	return a.internalError("follow Run log", err)
}

func consumeLogStream(body io.Reader, output io.Writer, offset *int64) (logStreamResult, error) {
	reader := bufio.NewReader(body)
	eventName := ""
	eventID := ""
	dataLines := []string{}
	for {
		line, err := reader.ReadString('\n')
		if len(line) > maximumSSELineBytes {
			return logStreamDisconnected, errors.New("SSE line exceeds the maximum supported size")
		}
		if len(line) != 0 {
			line = strings.TrimSuffix(line, "\n")
			line = strings.TrimSuffix(line, "\r")
			if line == "" {
				if eventName != "" || eventID != "" || len(dataLines) != 0 {
					result, dispatchErr := consumeLogEvent(
						eventName,
						eventID,
						strings.Join(dataLines, "\n"),
						output,
						offset,
					)
					if dispatchErr != nil || result != logStreamDisconnected {
						return result, dispatchErr
					}
				}
				eventName = ""
				eventID = ""
				dataLines = dataLines[:0]
			} else if !strings.HasPrefix(line, ":") {
				field, value, _ := strings.Cut(line, ":")
				value = strings.TrimPrefix(value, " ")
				switch field {
				case "event":
					eventName = value
				case "id":
					eventID = value
				case "data":
					dataLines = append(dataLines, value)
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return logStreamDisconnected, nil
			}

			return logStreamDisconnected, nil
		}
	}
}

func consumeLogEvent(
	eventName string,
	eventID string,
	data string,
	output io.Writer,
	offset *int64,
) (logStreamResult, error) {
	if eventName == "" {
		eventName = "message"
	}
	parsedID, err := strconv.ParseInt(eventID, 10, 64)
	if err != nil || parsedID < 0 {
		return logStreamDisconnected, errors.New("SSE event ID must be a nonnegative byte offset")
	}

	switch eventName {
	case "chunk":
		var event api.LogEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return logStreamDisconnected, fmt.Errorf("decode chunk event: %w", err)
		}
		if event.OffsetBytes < 0 || event.NextOffsetBytes < event.OffsetBytes ||
			event.NextOffsetBytes-event.OffsetBytes != int64(len(event.ContentsBase64)) ||
			parsedID != event.NextOffsetBytes {
			return logStreamDisconnected, errors.New("chunk event contains inconsistent byte offsets")
		}
		if event.OffsetBytes > *offset || event.NextOffsetBytes < *offset {
			return logStreamDisconnected, fmt.Errorf(
				"chunk event does not continue from durable byte offset %d",
				*offset,
			)
		}
		overlap := *offset - event.OffsetBytes
		contents := event.ContentsBase64[overlap:]
		written, err := output.Write(contents)
		if err != nil {
			return logStreamDisconnected, fmt.Errorf("%w: %w", errLogOutput, err)
		}
		if written != len(contents) {
			return logStreamDisconnected, fmt.Errorf("%w: %w", errLogOutput, io.ErrShortWrite)
		}
		*offset = event.NextOffsetBytes

		return logStreamDisconnected, nil
	case "sealed":
		if parsedID != *offset {
			return logStreamDisconnected, errors.New("sealed event offset does not match received log bytes")
		}

		return logStreamSealed, nil
	case "failed":
		if parsedID != *offset {
			return logStreamDisconnected, errors.New("failed event offset does not match received log bytes")
		}

		return logStreamFailed, nil
	default:
		return logStreamDisconnected, fmt.Errorf("unsupported SSE event %q", eventName)
	}
}

func retryableLogStatus(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500
}
