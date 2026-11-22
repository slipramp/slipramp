package gate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/google/jsonschema-go/jsonschema"
	"go.yaml.in/yaml/v3"
)

// The supported evaluator types determine how authored policies, Server
// sources, and persisted evidence are decoded.
var evaluatorCodecs = map[string]evaluatorCodec{
	PrometheusType: {
		policySchema: mustResolveEvaluatorSchema(prometheusDefinitionSchema()),
		sourceSchema: mustResolveEvaluatorSchema(jsonschema.For[sourceFilePrometheus](nil)),
		newPolicy:    func() Policy { return &PrometheusDefinition{} },
		newEvidence:  func() EvidencePayload { return &PrometheusEvidence{} },
		decodeSource: decodePrometheusSource,
	},
	WebhookType: {
		policySchema: mustResolveEvaluatorSchema(webhookDefinitionSchema()),
		sourceSchema: mustResolveEvaluatorSchema(jsonschema.For[sourceFileHTTP](nil)),
		newPolicy:    func() Policy { return &WebhookDefinition{} },
		newEvidence:  func() EvidencePayload { return &WebhookEvidence{} },
		decodeSource: decodeWebhookSource,
	},
}

type evaluatorCodec struct {
	policySchema *jsonschema.Resolved
	sourceSchema *jsonschema.Resolved
	newPolicy    func() Policy
	newEvidence  func() EvidencePayload
	decodeSource func(string, json.RawMessage) (Source, error)
}

func mustResolveEvaluatorSchema(schema *jsonschema.Schema, err error) *jsonschema.Resolved {
	if err != nil {
		panic(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		panic(err)
	}
	return resolved
}

func lookupEvaluator(name string) (evaluatorCodec, error) {
	codec, found := evaluatorCodecs[name]
	if !found {
		return evaluatorCodec{}, fmt.Errorf("unknown Gate evaluator type %q", name)
	}
	return codec, nil
}

// DefinitionSchema builds the authored evaluator union in stable type order.
func DefinitionSchema() *jsonschema.Schema {
	union := &jsonschema.Schema{}
	for _, name := range slices.Sorted(maps.Keys(evaluatorCodecs)) {
		fragment := evaluatorCodecs[name].policySchema.Schema().CloneSchemas()
		if fragment.Properties == nil {
			fragment.Properties = make(map[string]*jsonschema.Schema)
		}
		fragment.Properties["type"] = &jsonschema.Schema{Const: new(any(name))}
		fragment.Required = append([]string{"type"}, fragment.Required...)
		union.OneOf = append(union.OneOf, fragment)
	}
	return union
}

// Decode the YAML mapping before selecting its type so aliases and merge keys
// have the same meaning in evaluator fields as in the enclosing document.
func evaluatorFields(node *yaml.Node) (string, map[string]any, error) {
	var value any
	if err := node.Decode(&value); err != nil {
		return "", nil, err
	}
	fields, ok := value.(map[string]any)
	if !ok {
		return "", nil, errors.New("evaluator must be an object with a type")
	}
	name, ok := fields["type"].(string)
	if !ok || name == "" {
		return "", nil, errors.New("a string evaluator type is required")
	}
	delete(fields, "type")
	return name, fields, nil
}

func validatedFields(fields map[string]any, schema *jsonschema.Resolved) (json.RawMessage, error) {
	if err := schema.Validate(fields); err != nil {
		return nil, err
	}
	return json.Marshal(fields)
}

func decodeEvaluatorDefinition(node *yaml.Node) (Definition, error) {
	name, fields, err := evaluatorFields(node)
	if err != nil {
		return Definition{}, err
	}
	codec, err := lookupEvaluator(name)
	if err != nil {
		return Definition{}, err
	}
	raw, err := validatedFields(fields, codec.policySchema)
	if err != nil {
		return Definition{}, fmt.Errorf("validate %s evaluator: %w", name, err)
	}
	return codec.decodeDefinition(raw)
}

func (c evaluatorCodec) decodeDefinition(raw json.RawMessage) (Definition, error) {
	policy := c.newPolicy()
	if err := decodePayload(raw, policy); err != nil {
		return Definition{}, err
	}
	return Definition{Evaluator: policy}, nil
}

// Durable definitions and evidence use a single type key. This keeps the
// canonical Prometheus definition bytes (and existing override digests) stable.
func encodeTagged(value interface{ Type() string }) ([]byte, error) {
	return json.Marshal(map[string]any{value.Type(): value})
}

func splitTaggedPayload(data []byte) (string, json.RawMessage, error) {
	var tagged map[string]json.RawMessage
	if err := json.Unmarshal(data, &tagged); err != nil {
		return "", nil, err
	}
	if len(tagged) == 1 {
		for name, raw := range tagged {
			if len(raw) == 0 || raw[0] != '{' {
				return "", nil, errors.New("expected an object for the Gate payload")
			}
			return name, raw, nil
		}
	}
	return "", nil, errors.New("expected exactly one Gate payload")
}

func decodePayload(raw json.RawMessage, dst any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(dst)
}
