package gate

import (
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/slipramp/slipramp/internal/kubevalidate"
)

const WebhookType = "webhook"

type WebhookDefinition struct {
	Source string         `json:"source" yaml:"source"`
	Input  map[string]any `json:"input" yaml:"input"`
}

func (*WebhookDefinition) SubjectScoped() bool  { return true }
func (*WebhookDefinition) Type() string         { return WebhookType }
func (p *WebhookDefinition) SourceName() string { return p.Source }
func (p *WebhookDefinition) Normalized() Policy { return p }
func (p *WebhookDefinition) ValidateFields() []FieldIssue {
	return []FieldIssue{
		{"source", "catalog.gate.source-invalid", kubevalidate.DNSLabel("Gate webhook source", p.Source)},
		{"input", "catalog.gate.input-invalid", validateWebhookInput(p.Input)},
	}
}

func webhookDefinitionSchema() (*jsonschema.Schema, error) {
	schema, err := jsonschema.For[WebhookDefinition](nil)
	if err != nil {
		return nil, err
	}
	schema.Properties["source"].Pattern = `^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`
	schema.Properties["input"] = &jsonschema.Schema{
		Type:          "object",
		MaxProperties: new(64),
		PropertyNames: &jsonschema.Schema{MinLength: new(1), MaxLength: new(64)},
	}
	return schema, nil
}

func validateWebhookInput(input map[string]any) error {
	if input == nil {
		return errors.New("webhook input must be an object")
	}
	properties := 0
	if err := validateInputValue(input, 1, &properties); err != nil {
		return err
	}
	encoded, err := json.Marshal(input)
	if err != nil || len(encoded) > 8192 {
		return errors.New("webhook input exceeds the 8192-byte JSON limit")
	}
	return nil
}

func validateInputValue(value any, depth int, properties *int) error {
	if depth > 8 {
		return errors.New("webhook input exceeds eight nesting levels")
	}
	switch v := value.(type) {
	case map[string]any:
		*properties += len(v)
		if *properties > 64 {
			return errors.New("webhook input exceeds 64 properties")
		}
		for key, child := range v {
			if key == "" || utf8.RuneCountInString(key) > 64 || strings.ContainsRune(key, 0) {
				return errors.New("webhook input has an invalid property name")
			}
			if err := validateInputValue(child, depth+1, properties); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range v {
			if err := validateInputValue(child, depth+1, properties); err != nil {
				return err
			}
		}
	case string:
		if utf8.RuneCountInString(v) > 1024 || strings.ContainsRune(v, 0) {
			return errors.New("webhook input has an invalid string")
		}
	case float64:
		if !isFinite(v) || (math.Trunc(v) == v && math.Abs(v) > 1<<53-1) {
			return errors.New("webhook input number is outside the supported range")
		}
	case nil, bool:
	default:
		return errors.New("webhook input contains an unsupported JSON value")
	}
	return nil
}

type WebhookEvidence struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

var webhookCode = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

func (*WebhookEvidence) Type() string { return WebhookType }
func (e *WebhookEvidence) Validate() error {
	if e == nil || !webhookCode.MatchString(e.Code) || strings.TrimSpace(e.Message) == "" ||
		utf8.RuneCountInString(e.Message) > 256 ||
		strings.IndexFunc(e.Message, func(r rune) bool { return !unicode.IsPrint(r) }) >= 0 {
		return errors.New("invalid webhook evidence")
	}
	return nil
}
