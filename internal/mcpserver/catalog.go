package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/productidentity"
)

func targetActionToolCallFields(input explainTargetActionInput, _ any) []any {
	fields := uuidToolCallField(nil, "target_id", input.TargetID)
	if action, err := requiredCatalogName(input.Action, "action"); err == nil {
		fields = append(fields, "action", action)
	}

	return fields
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
