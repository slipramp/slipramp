package mcpserver

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/slipramp/slipramp/internal/kubevalidate"
)

var errInternalToolCall = errors.New("Slipramp could not complete the request") //nolint:staticcheck // Product name.

type toolHandler struct {
	catalogs  CatalogService
	runs      RunService
	artifacts ArtifactService
	logger    *slog.Logger
	observer  ToolCallObserver
}

func (s *toolHandler) register(server *mcp.Server) {
	closedWorld := false
	destructive := true

	addObservedTool(s, server, &mcp.Tool{
		Name:  "list_targets",
		Title: "List Slipramp Targets",
		Description: "List active Targets and their available Actions. Use this to resolve a targetId before " +
			"inspecting or creating a Run.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &closedWorld},
	}, s.listTargets, nil)
	addObservedTool(s, server, &mcp.Tool{
		Name:  "explain_target_action",
		Title: "Explain a Target Action",
		Description: "Resolve an Action's effective command, parameters, runtime, placement, credentials, " +
			"artifacts, inputs, triggers, and diagnostics without executing it.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &closedWorld},
	}, s.explainTargetAction, targetActionToolCallAttributes)
	addObservedTool(s, server, &mcp.Tool{
		Name:  "create_run",
		Title: "Create a Slipramp Run",
		Description: "Create an immutable MCP-sourced Run for one Target Action, optionally binding declared " +
			"inputs to retained Artifacts. This may change external infrastructure. Call explain_target_action " +
			"first and use a unique idempotencyKey.",
		Annotations: &mcp.ToolAnnotations{
			DestructiveHint: &destructive,
			IdempotentHint:  true,
		},
	}, s.createRun, createRunToolCallAttributes)
	addObservedTool(s, server, &mcp.Tool{
		Name:        "get_run",
		Title:       "Get a Slipramp Run",
		Description: "Get the current persisted state and outcome of a Run.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &closedWorld},
	}, s.getRun, runToolCallAttributes)
	addObservedTool(s, server, &mcp.Tool{
		Name:  "list_runs",
		Title: "List Slipramp Runs",
		Description: "List recent Runs in descending creation order, with filters for repository, commit, " +
			"consumed Artifact, Target, cluster, phase, and review state.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &closedWorld},
	}, s.listRuns, listRunsToolCallAttributes)
	addObservedTool(s, server, &mcp.Tool{
		Name:  "read_run_log",
		Title: "Read a Slipramp Run Log",
		Description: "Read one bounded page of currently durable Run log bytes. Use nextOffsetBytes to continue " +
			"reading live or retained logs.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &closedWorld},
	}, s.readRunLog, runLogToolCallAttributes)
	addObservedTool(s, server, &mcp.Tool{
		Name:        "list_run_artifacts",
		Title:       "List Slipramp Run Artifacts",
		Description: "List durable Artifact metadata, provenance, and structured report summaries for a Run.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &closedWorld},
	}, s.listRunArtifacts, runArtifactsToolCallAttributes)
	addObservedTool(s, server, &mcp.Tool{
		Name:  "read_artifact",
		Title: "Read a Slipramp Artifact",
		Description: "Read one bounded page of retained Artifact contents. Sensitive contents require operator " +
			"or administrator authorization.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &closedWorld},
	}, s.readArtifact, artifactToolCallAttributes)
	addObservedTool(s, server, &mcp.Tool{
		Name:  "cancel_run",
		Title: "Cancel a Slipramp Run",
		Description: "Request cancellation of a queued or active Run. Active execution stops only after its " +
			"Controller observes the request.",
		Annotations: &mcp.ToolAnnotations{
			DestructiveHint: &destructive,
			IdempotentHint:  true,
		},
	}, s.cancelRun, runToolCallAttributes)
}

func appendUUIDToolCallAttribute(attributes []any, name, value string) []any {
	parsed, err := requiredUUID(value, name)
	if err != nil {
		return attributes
	}

	return append(attributes, name, parsed)
}

func (s *toolHandler) internalError(operation string, err error) error {
	s.logger.Error("handle MCP tool call", "operation", operation, "error", err)

	return errInternalToolCall
}

func requiredUUID(value, name string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil || parsed == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%s must be a UUID", name)
	}

	return parsed, nil
}

func optionalUUID(value, name string) (*uuid.UUID, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}

	parsed, err := requiredUUID(value, name)
	if err != nil {
		return nil, err
	}

	return &parsed, nil
}

func requiredCatalogName(value, name string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if err := kubevalidate.DNSLabel(name, trimmed); err != nil {
		return "", fmt.Errorf("%s must be a valid catalog name", name)
	}

	return trimmed, nil
}

func optionalCatalogName(value, name string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}

	return requiredCatalogName(trimmed, name)
}
