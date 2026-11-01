package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/kubevalidate"
	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/run"
)

type toolServer struct {
	catalogs  CatalogService
	runs      RunService
	artifacts ArtifactService
	logger    *slog.Logger
}

func (s *toolServer) register(server *mcp.Server) {
	closedWorld := false
	destructive := true

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_targets",
		Title:       "List Slipramp Targets",
		Description: "List active Targets and their available Actions. Use this to resolve a targetId before inspecting or creating a Run.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &closedWorld},
	}, s.listTargets)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "explain_target_action",
		Title:       "Explain a Target Action",
		Description: "Resolve an Action's effective command, parameters, runtime, placement, credentials, artifacts, inputs, triggers, and diagnostics without executing it.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &closedWorld},
	}, s.explainTargetAction)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_run",
		Title:       "Create a Slipramp Run",
		Description: "Create an immutable MCP-sourced Run for one Target Action. This may change external infrastructure. Call explain_target_action first and use a unique idempotencyKey.",
		Annotations: &mcp.ToolAnnotations{
			DestructiveHint: &destructive,
			IdempotentHint:  true,
		},
	}, s.createRun)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_run",
		Title:       "Get a Slipramp Run",
		Description: "Get the current persisted state and outcome of a Run.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &closedWorld},
	}, s.getRun)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "read_run_log",
		Title:       "Read a Slipramp Run Log",
		Description: "Read one bounded page of currently durable Run log bytes. Use nextOffsetBytes to continue reading live or retained logs.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &closedWorld},
	}, s.readRunLog)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_run_artifacts",
		Title:       "List Slipramp Run Artifacts",
		Description: "List durable Artifact metadata, provenance, and structured report summaries for a Run.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &closedWorld},
	}, s.listRunArtifacts)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "read_artifact",
		Title:       "Read a Slipramp Artifact",
		Description: "Read one bounded page of retained Artifact contents. Sensitive contents require operator or administrator authorization.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &closedWorld},
	}, s.readArtifact)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "cancel_run",
		Title:       "Cancel a Slipramp Run",
		Description: "Request cancellation of a queued or active Run. Active execution stops only after its Controller observes the request.",
		Annotations: &mcp.ToolAnnotations{
			DestructiveHint: &destructive,
			IdempotentHint:  true,
		},
	}, s.cancelRun)
}

type listTargetsInput struct {
	RepositoryID string `json:"repositoryId,omitempty" jsonschema:"Optional repository UUID to filter by."`
	Query        string `json:"query,omitempty" jsonschema:"Optional case-insensitive Target name or title search, up to 200 characters."`
	Runtime      string `json:"runtime,omitempty" jsonschema:"Optional catalog runtime name to filter by."`
	Placement    string `json:"placement,omitempty" jsonschema:"Optional placement name to filter by."`
	Limit        int32  `json:"limit,omitempty" jsonschema:"Maximum Targets to return, from 1 to 100. Defaults to 25."`
	Offset       int32  `json:"offset,omitempty" jsonschema:"Zero-based pagination offset."`
}

func (s *toolServer) listTargets(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input listTargetsInput,
) (*mcp.CallToolResult, targetListOutput, error) {
	if _, err := authorize(ctx, productidentity.PermissionRead); err != nil {
		return nil, targetListOutput{}, err
	}

	filter, err := targetListFilter(input)
	if err != nil {
		return nil, targetListOutput{}, err
	}
	targets, err := s.catalogs.ListTargets(ctx, filter)
	if err != nil {
		return nil, targetListOutput{}, s.internalError("list Targets", err)
	}

	items := make([]targetOutput, 0, len(targets.Items))
	for _, target := range targets.Items {
		actions := slices.Sorted(maps.Keys(target.Target.Spec.Actions))
		items = append(items, targetOutput{
			ID:                  target.ID.String(),
			Name:                target.Target.Metadata.Name,
			Title:               target.Target.Metadata.Title,
			RepositoryID:        target.RepositoryID.String(),
			RepositoryName:      target.RepositoryName,
			CatalogRevisionID:   target.CatalogRevisionID.String(),
			CatalogCommitSHA:    target.CatalogCommitSHA,
			DesiredCommitSHA:    target.DesiredCommitSHA,
			Runtime:             target.Target.Spec.Runtime,
			Placement:           target.Target.Spec.Placement,
			Actions:             actions,
			ReconciliationState: string(target.ReconciliationComparison()),
		})
	}

	return nil, targetListOutput{
		Items:  items,
		Limit:  filter.Limit,
		Offset: filter.Offset,
		Total:  targets.Total,
	}, nil
}

func targetListFilter(input listTargetsInput) (catalog.TargetListFilter, error) {
	limit := input.Limit
	if limit == 0 {
		limit = catalog.DefaultTargetListLimit
	}
	if limit < 1 || limit > catalog.MaximumTargetListLimit {
		return catalog.TargetListFilter{}, fmt.Errorf(
			"limit must be between 1 and %d",
			catalog.MaximumTargetListLimit,
		)
	}
	if input.Offset < 0 || input.Offset > catalog.MaximumTargetListOffset {
		return catalog.TargetListFilter{}, errors.New("offset must be between 0 and 2147483647")
	}

	query := strings.TrimSpace(input.Query)
	if len(query) > catalog.MaximumTargetSearchLength {
		return catalog.TargetListFilter{}, errors.New("query must not exceed 200 characters")
	}
	runtime, err := optionalCatalogName(input.Runtime, "runtime")
	if err != nil {
		return catalog.TargetListFilter{}, err
	}
	placement, err := optionalCatalogName(input.Placement, "placement")
	if err != nil {
		return catalog.TargetListFilter{}, err
	}

	var repositoryID *uuid.UUID
	if strings.TrimSpace(input.RepositoryID) != "" {
		parsed, err := uuid.Parse(input.RepositoryID)
		if err != nil {
			return catalog.TargetListFilter{}, errors.New("repositoryId must be a UUID")
		}
		repositoryID = &parsed
	}

	return catalog.TargetListFilter{
		RepositoryID: repositoryID,
		Query:        query,
		Runtime:      runtime,
		Placement:    placement,
		Limit:        limit,
		Offset:       input.Offset,
	}, nil
}

type explainTargetActionInput struct {
	TargetID string `json:"targetId" jsonschema:"Durable Target UUID."`
	Action   string `json:"action" jsonschema:"Action name declared on the Target."`
}

func (s *toolServer) explainTargetAction(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input explainTargetActionInput,
) (*mcp.CallToolResult, any, error) {
	if _, err := authorize(ctx, productidentity.PermissionRead); err != nil {
		return nil, nil, err
	}
	targetID, err := requiredUUID(input.TargetID, "targetId")
	if err != nil {
		return nil, nil, err
	}
	action, err := requiredCatalogName(input.Action, "action")
	if err != nil {
		return nil, nil, err
	}

	explanation, err := s.catalogs.ExplainAction(ctx, targetID, action)
	if errors.Is(err, catalog.ErrNotFound) ||
		errors.Is(err, catalog.ErrTargetNotFound) ||
		errors.Is(err, catalog.ErrActionNotFound) {
		return nil, nil, errors.New("the active repository catalog, Target, or Action does not exist")
	}
	if err != nil {
		return nil, nil, s.internalError("explain Target Action", err)
	}

	return nil, explanation, nil
}

type createRunInput struct {
	TargetID       string         `json:"targetId" jsonschema:"Durable Target UUID."`
	Action         string         `json:"action" jsonschema:"Action name declared on the Target."`
	Parameters     map[string]any `json:"parameters,omitempty" jsonschema:"Values for parameters declared by the selected Action."`
	IdempotencyKey string         `json:"idempotencyKey" jsonschema:"Unique key for this logical Run request, up to 128 characters. Reuse only when retrying the identical request."`
}

func (s *toolServer) createRun(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input createRunInput,
) (*mcp.CallToolResult, runOutput, error) {
	authentication, err := authorize(ctx, productidentity.PermissionRunsWrite)
	if err != nil {
		return nil, runOutput{}, err
	}
	targetID, err := requiredUUID(input.TargetID, "targetId")
	if err != nil {
		return nil, runOutput{}, err
	}
	action, err := requiredCatalogName(input.Action, "action")
	if err != nil {
		return nil, runOutput{}, err
	}
	parameters := input.Parameters
	if parameters == nil {
		parameters = map[string]any{}
	}

	created, err := s.runs.CreateRun(ctx, run.CreateRequest{
		TargetID:       targetID,
		ActionName:     action,
		Parameters:     parameters,
		Inputs:         map[string]uuid.UUID{},
		Source:         run.Source{Type: run.SourceTypeMCP},
		IdempotencyKey: &input.IdempotencyKey,
		Actor:          &authentication.Principal,
	})
	if err != nil {
		return nil, runOutput{}, s.createRunError(err)
	}

	return nil, newRunOutput(created), nil
}

type runIDInput struct {
	RunID string `json:"runId" jsonschema:"Run UUID."`
}

func (s *toolServer) getRun(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input runIDInput,
) (*mcp.CallToolResult, runOutput, error) {
	if _, err := authorize(ctx, productidentity.PermissionRead); err != nil {
		return nil, runOutput{}, err
	}
	runID, err := requiredUUID(input.RunID, "runId")
	if err != nil {
		return nil, runOutput{}, err
	}

	record, err := s.runs.GetRun(ctx, runID)
	if errors.Is(err, run.ErrRunNotFound) {
		return nil, runOutput{}, errors.New("the Run does not exist")
	}
	if err != nil {
		return nil, runOutput{}, s.internalError("get Run", err)
	}

	return nil, newRunOutput(record), nil
}

func (s *toolServer) cancelRun(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input runIDInput,
) (*mcp.CallToolResult, runOutput, error) {
	authentication, err := authorize(ctx, productidentity.PermissionRunsWrite)
	if err != nil {
		return nil, runOutput{}, err
	}
	runID, err := requiredUUID(input.RunID, "runId")
	if err != nil {
		return nil, runOutput{}, err
	}

	record, err := s.runs.CancelRun(ctx, run.CancelRequest{
		RunID: runID,
		Actor: &authentication.Principal,
	})
	if errors.Is(err, run.ErrRunNotFound) {
		return nil, runOutput{}, errors.New("the Run does not exist")
	}
	if errors.Is(err, run.ErrRunFinished) {
		return nil, runOutput{}, errors.New("the Run already finished and cannot be canceled")
	}
	if err != nil {
		return nil, runOutput{}, s.internalError("cancel Run", err)
	}

	return nil, newRunOutput(record), nil
}

func (s *toolServer) createRunError(err error) error {
	switch {
	case errors.Is(err, run.ErrInvalidRequest),
		errors.Is(err, run.ErrInvalidParameters),
		errors.Is(err, run.ErrInvalidInputs):
		return err
	case errors.Is(err, run.ErrRepositoryNotFound),
		errors.Is(err, run.ErrCatalogNotFound),
		errors.Is(err, run.ErrTargetNotFound),
		errors.Is(err, run.ErrActionNotFound),
		errors.Is(err, run.ErrRuntimeNotFound),
		errors.Is(err, run.ErrPlacementNotFound),
		errors.Is(err, run.ErrArtifactNotFound):
		return errors.New("the requested Action could not be resolved from the active catalog")
	case errors.Is(err, run.ErrIdempotencyConflict):
		return errors.New("the idempotency key was already used for a different Run request")
	default:
		return s.internalError("create Run", err)
	}
}

func (s *toolServer) internalError(operation string, err error) error {
	s.logger.Error("handle MCP tool call", "operation", operation, "error", err)

	return errors.New("Slipramp could not complete the request") //nolint:staticcheck // Product name.
}

func requiredUUID(value, name string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil || parsed == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%s must be a UUID", name)
	}

	return parsed, nil
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
