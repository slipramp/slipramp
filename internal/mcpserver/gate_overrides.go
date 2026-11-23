package mcpserver

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/gate"
	"github.com/slipramp/slipramp/internal/productidentity"
)

type gateSubjectInput struct {
	Subject   gate.SubjectType `json:"subject" jsonschema:"Gate subject: run or promotion."`
	SubjectID string           `json:"subjectId" jsonschema:"Slipramp Run or Promotion UUID."`
}

type overrideGateInput struct {
	gateSubjectInput
	Key     gate.Key     `json:"key" jsonschema:"Exact eligible Gate key from list_gate_overrides."`
	Version gate.Version `json:"version" jsonschema:"Exact policy version returned with that key."`
	Reason  string       `json:"reason,omitempty" jsonschema:"Optional reason for waiving this Gate on this subject (up to 1024 characters)."`
}

func (input gateSubjectInput) id() (uuid.UUID, error) {
	switch input.Subject {
	case gate.RunSubject, gate.PromotionSubject:
		return requiredUUID(input.SubjectID, "subjectId")
	default:
		return uuid.Nil, errors.New("subject must be run or promotion")
	}
}

func (input gateSubjectInput) attributes() []any {
	id, err := input.id()
	if err != nil {
		return nil
	}
	return []any{string(input.Subject) + "_id", id}
}

func (s *toolHandler) listGateOverrides(ctx context.Context, _ *mcp.CallToolRequest, input gateSubjectInput) (
	*mcp.CallToolResult, api.GateOverrideCollection, error,
) {
	if _, err := authorize(ctx, productidentity.PermissionRead); err != nil {
		return nil, api.GateOverrideCollection{}, err
	}
	id, err := input.id()
	if err != nil {
		return nil, api.GateOverrideCollection{}, err
	}
	list, err := s.gateOverrides.ListGateOverrides(ctx, input.Subject, id, s.gateEvaluatorProcessID)
	if errors.Is(err, gate.ErrSubjectNotFound) {
		return nil, api.GateOverrideCollection{}, err
	}
	if err != nil {
		return nil, api.GateOverrideCollection{}, s.internalError("list Gate overrides", err)
	}
	return nil, api.NewGateOverrideCollection(list, s.gateSources), nil
}

func (s *toolHandler) overrideGate(ctx context.Context, _ *mcp.CallToolRequest, input overrideGateInput) (
	*mcp.CallToolResult, api.GateRequirement, error,
) {
	id, err := input.id()
	if err != nil {
		return nil, api.GateRequirement{}, err
	}
	permission := productidentity.PermissionRunsOverrideGates
	if input.Subject == gate.PromotionSubject {
		permission = productidentity.PermissionPromotionsOverrideGates
	}
	auth, err := authorize(ctx, permission)
	if err != nil {
		return nil, api.GateRequirement{}, err
	}
	actor, err := auth.Principal.ActorSnapshot()
	if err != nil {
		return nil, api.GateRequirement{}, s.internalError("identify Gate override actor", err)
	}
	requirement, err := s.gateOverrides.RecordGateOverride(ctx, gate.OverrideRequest{
		Subject: input.Subject, SubjectID: id,
		Key: input.Key, Version: input.Version, Reason: input.Reason,
		Actor: actor, RecordedAt: time.Now().UTC(), EvaluatorProcessID: s.gateEvaluatorProcessID,
	})
	if errors.Is(err, gate.ErrSubjectNotFound) || errors.Is(err, gate.ErrOverrideConflict) ||
		errors.Is(err, gate.ErrOverrideInvalid) {
		return nil, api.GateRequirement{}, err
	}
	if err != nil {
		return nil, api.GateRequirement{}, s.internalError("record Gate override", err)
	}
	return nil, api.NewGateRequirement(requirement, s.gateSources), nil
}
