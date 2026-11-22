package mcpserver

import (
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/gate"
)

func (s *toolHandler) registerGateTools(server *mcp.Server) {
	read := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: new(false)}
	write := &mcp.ToolAnnotations{DestructiveHint: new(true)}

	// UUIDs and tagged Gate evidence use custom JSON encodings in the product responses.
	schemas := &jsonschema.ForOptions{TypeSchemas: map[reflect.Type]*jsonschema.Schema{
		reflect.TypeFor[uuid.UUID](): {Type: "string", Format: "uuid"},
	}}
	schemas.TypeSchemas[reflect.TypeFor[gate.Evidence]()] = &jsonschema.Schema{
		Type: "object",
		OneOf: []*jsonschema.Schema{
			gateToolSchema[api.LockGateEvidence](schemas),
			gateToolSchema[api.WindowGateEvidence](schemas),
			gateToolSchema[api.PrometheusGateEvidence](schemas),
			gateToolSchema[api.WebhookGateEvidence](schemas),
		},
	}
	scope := gateToolSchema[api.ServerGateScope](schemas)
	addObservedTool(s, server, &mcp.Tool{
		Name: "get_gate_scope", Title: "Inspect Gate policy",
		Description: "Read the current lock, weekly window, independent versions, and recent changes for one scope.",
		Annotations: read, OutputSchema: scope,
	}, s.getGateScope, gateToolAttributes)
	addObservedTool(s, server, &mcp.Tool{
		Name: "lock_gate_scope", Title: "Lock a Gate scope",
		Description: "Hold gated work at this scope for the given reason, optionally until a specified time.",
		Annotations: write, OutputSchema: scope,
	}, s.lockGateScope, gateToolAttributes)
	addObservedTool(s, server, &mcp.Tool{
		Name: "unlock_gate_scope", Title: "Release a Gate lock",
		Description: "Release the observed lock for all waiting work at this scope. A newer active lock causes a conflict.",
		Annotations: write, OutputSchema: scope,
	}, s.unlockGateScope, gateToolAttributes)
	addObservedTool(s, server, &mcp.Tool{
		Name: "set_gate_window", Title: "Set a weekly Gate window",
		Description: "Set this scope's weekly window. Every accepted set advances the version and invalidates earlier waivers.",
		Annotations: write, OutputSchema: scope,
	}, s.setGateWindow, gateToolAttributes)
	addObservedTool(s, server, &mcp.Tool{
		Name: "remove_gate_window", Title: "Remove a weekly Gate window",
		Description: "Clear this scope's weekly schedule for all waiting work.",
		Annotations: write, OutputSchema: scope,
	}, s.removeGateWindow, gateToolAttributes)
	addObservedTool(s, server, &mcp.Tool{
		Name: "list_gate_overrides", Title: "Inspect Gate overrides",
		Description: "List eligible Gate keys, exact versions, evidence, and prior decisions for one Run or Promotion.",
		Annotations: read, OutputSchema: gateToolSchema[api.GateOverrideCollection](schemas),
	}, s.listGateOverrides, gateToolAttributes)
	addObservedTool(s, server, &mcp.Tool{
		Name: "override_gate", Title: "Override a Gate for a Run or Promotion",
		Description: "Waive one Gate on one waiting subject using the key and version from list_gate_overrides and a reason.",
		Annotations: write, OutputSchema: gateToolSchema[api.GateRequirement](schemas),
	}, s.overrideGate, gateToolAttributes)
}

func gateToolSchema[T any](options *jsonschema.ForOptions) *jsonschema.Schema {
	schema, err := jsonschema.For[T](options)
	if err != nil {
		panic(err)
	}
	return schema
}

func gateToolAttributes[Input interface{ attributes() []any }, Output any](input Input, _ Output) []any {
	return input.attributes()
}
