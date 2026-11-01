package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/run"
)

func createRunToolCallFields(input createRunInput, output runOutput) []any {
	fields := uuidToolCallField(nil, "target_id", input.TargetID)
	if action, err := requiredCatalogName(input.Action, "action"); err == nil {
		fields = append(fields, "action", action)
	}
	fields = uuidToolCallField(fields, "run_id", output.ID)

	return fields
}

func runToolCallFields(input runIDInput, _ runOutput) []any {
	return uuidToolCallField(nil, "run_id", input.RunID)
}

func listRunsToolCallFields(input listRunsInput, _ runListOutput) []any {
	fields := uuidToolCallField(nil, "target_id", input.TargetID)

	return uuidToolCallField(fields, "artifact_id", input.InputArtifactID)
}

type createRunInput struct {
	TargetID       string            `json:"targetId" jsonschema:"Durable Target UUID."`
	Action         string            `json:"action" jsonschema:"Action name declared on the Target."`
	Parameters     map[string]any    `json:"parameters,omitempty" jsonschema:"Values for parameters declared by the selected Action."`
	Inputs         map[string]string `json:"inputs,omitempty" jsonschema:"Explicit bindings from declared input names to retained Artifact UUIDs. Bind every input reported by explain_target_action."`
	IdempotencyKey string            `json:"idempotencyKey" jsonschema:"Unique key for this logical Run request, up to 128 characters. Reuse only when retrying the identical request."`
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
	inputs, err := artifactInputBindings(input.Inputs)
	if err != nil {
		return nil, runOutput{}, err
	}

	created, err := s.runs.CreateRun(ctx, run.CreateRequest{
		TargetID:       targetID,
		ActionName:     action,
		Parameters:     parameters,
		Inputs:         inputs,
		Source:         run.Source{Type: run.SourceTypeMCP},
		IdempotencyKey: &input.IdempotencyKey,
		Actor:          &authentication.Principal,
	})
	if err != nil {
		return nil, runOutput{}, s.createRunError(err)
	}

	return nil, newRunOutput(created), nil
}

func artifactInputBindings(values map[string]string) (map[string]uuid.UUID, error) {
	result := make(map[string]uuid.UUID, len(values))
	for _, rawName := range slices.Sorted(maps.Keys(values)) {
		name, err := requiredCatalogName(rawName, "input name")
		if err != nil {
			return nil, err
		}
		if _, exists := result[name]; exists {
			return nil, fmt.Errorf("input name %q is bound more than once", name)
		}

		artifactID, err := requiredUUID(values[rawName], fmt.Sprintf("input %q artifact ID", name))
		if err != nil {
			return nil, err
		}
		result[name] = artifactID
	}

	return result, nil
}

type runIDInput struct {
	RunID string `json:"runId" jsonschema:"Run UUID."`
}

type listRunsInput struct {
	RepositoryID    string `json:"repositoryId,omitempty" jsonschema:"Optional repository UUID to filter by."`
	TargetID        string `json:"targetId,omitempty" jsonschema:"Optional durable Target UUID to filter by."`
	CommitSHA       string `json:"commitSha,omitempty" jsonschema:"Optional lowercase full Git object ID. Requires repositoryId."`
	InputArtifactID string `json:"inputArtifactId,omitempty" jsonschema:"Optional Artifact UUID to find Runs that consume it."`
	Cluster         string `json:"cluster,omitempty" jsonschema:"Optional execution cluster name to filter by."`
	Phase           string `json:"phase,omitempty" jsonschema:"Optional phase: queued, assigned, preparing, running, or finished."`
	ReviewState     string `json:"reviewState,omitempty" jsonschema:"Optional review state: not-required, pending, approved, or rejected."`
	Limit           int32  `json:"limit,omitempty" jsonschema:"Maximum Runs to return, from 1 to 100. Defaults to 50."`
	Offset          int32  `json:"offset,omitempty" jsonschema:"Zero-based pagination offset."`
}

func (s *toolServer) listRuns(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input listRunsInput,
) (*mcp.CallToolResult, runListOutput, error) {
	if _, err := authorize(ctx, productidentity.PermissionRead); err != nil {
		return nil, runListOutput{}, err
	}

	filter, err := runListFilter(input)
	if err != nil {
		return nil, runListOutput{}, err
	}
	records, err := s.runs.ListRuns(ctx, filter)
	if errors.Is(err, run.ErrInvalidRequest) {
		return nil, runListOutput{}, err
	}
	if err != nil {
		return nil, runListOutput{}, s.internalError("list Runs", err)
	}

	items := make([]runOutput, 0, len(records.Items))
	for _, record := range records.Items {
		items = append(items, newRunOutput(record))
	}

	return nil, runListOutput{
		Items:  items,
		Limit:  filter.Limit,
		Offset: filter.Offset,
		Total:  records.Total,
	}, nil
}

func runListFilter(input listRunsInput) (run.RunListFilter, error) {
	repositoryID, err := optionalUUID(input.RepositoryID, "repositoryId")
	if err != nil {
		return run.RunListFilter{}, err
	}
	targetID, err := optionalUUID(input.TargetID, "targetId")
	if err != nil {
		return run.RunListFilter{}, err
	}
	inputArtifactID, err := optionalUUID(input.InputArtifactID, "inputArtifactId")
	if err != nil {
		return run.RunListFilter{}, err
	}

	limit := input.Limit
	if limit == 0 {
		limit = run.DefaultRunListLimit
	}
	if limit < 1 || limit > run.MaximumRunListLimit {
		return run.RunListFilter{}, fmt.Errorf("limit must be between 1 and %d", run.MaximumRunListLimit)
	}
	if input.Offset < 0 || input.Offset > run.MaximumRunListOffset {
		return run.RunListFilter{}, errors.New("offset must be between 0 and 2147483647")
	}

	return run.RunListFilter{
		RepositoryID:    repositoryID,
		TargetID:        targetID,
		CommitSHA:       input.CommitSHA,
		InputArtifactID: inputArtifactID,
		ClusterName:     input.Cluster,
		Phase:           run.Phase(input.Phase),
		ReviewState:     run.ReviewState(input.ReviewState),
		Limit:           limit,
		Offset:          input.Offset,
	}, nil
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
