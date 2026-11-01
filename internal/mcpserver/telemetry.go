package mcpserver

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ToolCallResult string

const (
	ToolCallSucceeded ToolCallResult = "succeeded"
	ToolCallRejected  ToolCallResult = "rejected"
	ToolCallFailed    ToolCallResult = "failed"
)

type ToolCallObserver interface {
	ObserveMCPToolCall(context.Context, string, ToolCallResult, time.Duration)
}

type noopToolCallObserver struct{}

func (noopToolCallObserver) ObserveMCPToolCall(context.Context, string, ToolCallResult, time.Duration) {
}

type toolCallFields[Input, Output any] func(Input, Output) []any

func addObservedTool[Input, Output any](
	tools *toolServer,
	server *mcp.Server,
	tool *mcp.Tool,
	handler mcp.ToolHandlerFor[Input, Output],
	fields toolCallFields[Input, Output],
) {
	mcp.AddTool(
		server,
		tool,
		func(
			ctx context.Context,
			request *mcp.CallToolRequest,
			input Input,
		) (*mcp.CallToolResult, Output, error) {
			startedAt := time.Now()
			result, output, err := handler(ctx, request, input)
			observeToolCall(tools, ctx, request, tool.Name, startedAt, input, output, err, fields)

			return result, output, err
		},
	)
}

func observeToolCall[Input, Output any](
	s *toolServer,
	ctx context.Context,
	request *mcp.CallToolRequest,
	tool string,
	startedAt time.Time,
	input Input,
	output Output,
	err error,
	fields toolCallFields[Input, Output],
) {
	duration := time.Since(startedAt)
	result := toolCallResult(err)
	attributes := []any{
		"tool", tool,
		"result", result,
		"duration", duration,
	}
	if authentication, authenticationErr := currentAuthentication(ctx); authenticationErr == nil {
		attributes = append(
			attributes,
			"principal_id", authentication.Principal.ID,
			"principal_kind", authentication.Principal.Kind,
			"authentication_method", authentication.Method,
			"credential_id", authentication.CredentialID,
		)
	}
	if request != nil {
		if client := request.ClientInfo(); client != nil {
			attributes = append(
				attributes,
				"client_name", client.Name,
				"client_version", client.Version,
			)
		}
		if protocolVersion := strings.TrimSpace(request.ProtocolVersion()); protocolVersion != "" {
			attributes = append(attributes, "protocol_version", protocolVersion)
		}
	}
	if fields != nil {
		attributes = append(attributes, fields(input, output)...)
	}
	if err != nil {
		attributes = append(attributes, "error", err)
	}

	s.logger.Info("MCP tool call", attributes...)
	s.observer.ObserveMCPToolCall(ctx, tool, result, duration)
}

func toolCallResult(err error) ToolCallResult {
	switch {
	case err == nil:
		return ToolCallSucceeded
	case errors.Is(err, errInternalToolCall):
		return ToolCallFailed
	default:
		return ToolCallRejected
	}
}

func sdkLogger(logger *slog.Logger) *slog.Logger {
	return slog.New(minimumLevelHandler{next: logger.Handler(), minimum: slog.LevelWarn})
}

type minimumLevelHandler struct {
	next    slog.Handler
	minimum slog.Level
}

func (h minimumLevelHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.minimum && h.next.Enabled(ctx, level)
}

func (h minimumLevelHandler) Handle(ctx context.Context, record slog.Record) error {
	return h.next.Handle(ctx, record)
}

func (h minimumLevelHandler) WithAttrs(attributes []slog.Attr) slog.Handler {
	return minimumLevelHandler{next: h.next.WithAttrs(attributes), minimum: h.minimum}
}

func (h minimumLevelHandler) WithGroup(name string) slog.Handler {
	return minimumLevelHandler{next: h.next.WithGroup(name), minimum: h.minimum}
}
