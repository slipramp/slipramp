package gate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"go.yaml.in/yaml/v3"
)

// Definition is the catalog-independent policy evaluated for one Gate name.
// Presentation metadata and the Gate name are deliberately excluded so
// equivalent definitions have the same digest.
type Definition struct {
	Evaluator Policy `json:"-" yaml:"-"`
}

type Policy interface {
	Type() string
	SourceName() string
	SubjectScoped() bool
	ValidateFields() []FieldIssue
	Normalized() Policy
}

type FieldIssue struct {
	Field string
	Code  string
	Err   error
}

func (d Definition) MarshalJSON() ([]byte, error) {
	if d.Evaluator == nil {
		return nil, errors.New("missing Gate evaluator")
	}
	return encodeTagged(d.Evaluator)
}

func (d *Definition) UnmarshalJSON(data []byte) error {
	name, raw, err := splitTaggedPayload(data)
	if err != nil {
		return err
	}
	codec, err := lookupEvaluator(name)
	if err != nil {
		return err
	}
	var fields any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	if err := codec.policySchema.Validate(fields); err != nil {
		return fmt.Errorf("invalid stored %s policy: %w", name, err)
	}
	decoded, err := codec.decodeDefinition(raw)
	if err == nil {
		*d = decoded
	}
	return err
}

func (d Definition) MarshalYAML() (any, error) {
	if d.Evaluator == nil {
		return nil, errors.New("missing Gate evaluator")
	}
	var node yaml.Node
	if err := node.Encode(d.Evaluator); err != nil {
		return nil, err
	}
	node.Content = append([]*yaml.Node{
		{Kind: yaml.ScalarNode, Tag: "!!str", Value: "type"},
		{Kind: yaml.ScalarNode, Tag: "!!str", Value: d.Evaluator.Type()},
	}, node.Content...)
	return map[string]*yaml.Node{"evaluator": &node}, nil
}

func (d *Definition) UnmarshalYAML(node *yaml.Node) error {
	var spec map[string]yaml.Node
	if err := node.Decode(&spec); err != nil {
		return err
	}
	evaluator, found := spec["evaluator"]
	if !found || len(spec) != 1 {
		return errors.New("expected exactly one evaluator field in Gate spec")
	}
	decoded, err := decodeEvaluatorDefinition(&evaluator)
	if err == nil {
		*d = decoded
	}
	return err
}

func (d Definition) Issues() []FieldIssue {
	if d.Evaluator == nil {
		return []FieldIssue{{
			Field: "type", Code: "catalog.gate.type-invalid", Err: errors.New("missing Gate evaluator"),
		}}
	}
	return slices.DeleteFunc(d.Evaluator.ValidateFields(), func(issue FieldIssue) bool { return issue.Err == nil })
}

func (d Definition) Validate() error {
	if issues := d.Issues(); len(issues) > 0 {
		return issues[0].Err
	}
	return nil
}

func (d Definition) Digest() (string, error) {
	if err := d.Validate(); err != nil {
		return "", err
	}
	contents, err := encodeTagged(d.Evaluator.Normalized())
	if err != nil {
		return "", fmt.Errorf("encode Gate definition: %w", err)
	}
	digest := sha256.Sum256(contents)
	return hex.EncodeToString(digest[:]), nil
}
