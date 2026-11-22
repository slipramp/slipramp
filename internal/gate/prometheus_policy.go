package gate

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/slipramp/slipramp/internal/kubevalidate"
)

const (
	PrometheusType     = "prometheus"
	MaximumQueryLength = 16 * 1024

	OperatorEqual              = "equal"
	OperatorNotEqual           = "not-equal"
	OperatorLessThan           = "less-than"
	OperatorLessThanOrEqual    = "less-than-or-equal"
	OperatorGreaterThan        = "greater-than"
	OperatorGreaterThanOrEqual = "greater-than-or-equal"
)

type PrometheusDefinition struct {
	Source    string    `json:"source" yaml:"source"`
	Query     string    `json:"query" yaml:"query"`
	Condition Condition `json:"condition" yaml:"condition"`
}

type Condition struct {
	Operator string  `json:"operator" yaml:"operator"`
	Value    float64 `json:"value" yaml:"value"`
}

func (*PrometheusDefinition) SubjectScoped() bool { return false }
func (*PrometheusDefinition) Type() string        { return PrometheusType }
func (p PrometheusDefinition) SourceName() string { return p.Source }

func (p PrometheusDefinition) ValidateFields() []FieldIssue {
	return []FieldIssue{
		{"source", "catalog.gate.source-invalid", kubevalidate.DNSLabel("Gate Prometheus source", p.Source)},
		{"query", "catalog.gate.query-invalid", ValidateQuery(p.Query)},
		{"condition.operator", "catalog.gate.operator-invalid", ValidateOperator(p.Condition.Operator)},
		{"condition.value", "catalog.gate.threshold-invalid", ValidateThreshold(p.Condition.Value)},
	}
}

func (p PrometheusDefinition) Normalized() Policy {
	if p.Condition.Value == 0 {
		p.Condition.Value = 0 // Positive and negative zero represent the same threshold.
	}
	return &p
}

func ValidateQuery(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("Gate Prometheus query is required") //nolint:staticcheck // Gate is a domain name.
	}
	if strings.TrimSpace(value) != value {
		return errors.New("Gate Prometheus query must not have leading or trailing whitespace") //nolint:staticcheck // Gate is a domain name.
	}
	if utf8.RuneCountInString(value) > MaximumQueryLength {
		return fmt.Errorf( //nolint:staticcheck // Gate is a domain name.
			"Gate Prometheus query must not exceed %d characters",
			MaximumQueryLength,
		)
	}

	return nil
}

func (c Condition) Validate() error {
	if err := ValidateOperator(c.Operator); err != nil {
		return err
	}
	return ValidateThreshold(c.Value)
}

func ValidateThreshold(value float64) error {
	if !isFinite(value) {
		return errors.New("Gate condition value must be finite") //nolint:staticcheck // Gate is a domain name.
	}

	return nil
}

func ValidateOperator(value string) error {
	switch value {
	case OperatorEqual,
		OperatorNotEqual,
		OperatorLessThan,
		OperatorLessThanOrEqual,
		OperatorGreaterThan,
		OperatorGreaterThanOrEqual:
		return nil
	default:
		return fmt.Errorf("Gate condition operator %q is not supported", value) //nolint:staticcheck // Gate is a domain name.
	}
}

func (c Condition) Compare(value float64) (bool, error) {
	if err := c.Validate(); err != nil {
		return false, err
	}
	if !isFinite(value) {
		return false, errors.New("Gate observation value must be finite") //nolint:staticcheck // Gate is a domain name.
	}

	switch c.Operator {
	case OperatorEqual:
		return value == c.Value, nil
	case OperatorNotEqual:
		return value != c.Value, nil
	case OperatorLessThan:
		return value < c.Value, nil
	case OperatorLessThanOrEqual:
		return value <= c.Value, nil
	case OperatorGreaterThan:
		return value > c.Value, nil
	case OperatorGreaterThanOrEqual:
		return value >= c.Value, nil
	default:
		panic("validated Gate operator was not handled")
	}
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func prometheusDefinitionSchema() (*jsonschema.Schema, error) {
	schema, err := jsonschema.For[PrometheusDefinition](nil)
	if err != nil {
		return nil, err
	}
	source := schema.Properties["source"]
	source.MaxLength = new(63)
	source.Pattern = `^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`
	query := schema.Properties["query"]
	query.MinLength = new(1)
	query.MaxLength = new(MaximumQueryLength)
	schema.Properties["condition"].Properties["operator"].Enum = []any{
		OperatorEqual, OperatorNotEqual, OperatorLessThan, OperatorLessThanOrEqual,
		OperatorGreaterThan, OperatorGreaterThanOrEqual,
	}
	return schema, nil
}

type PrometheusEvidence struct {
	Value *float64 `json:"value"`
}

func (*PrometheusEvidence) Type() string { return PrometheusType }

func (e *PrometheusEvidence) Validate() error {
	if e == nil || e.Value == nil || !isFinite(*e.Value) {
		return errors.New("prometheus evidence requires a finite value")
	}
	return nil
}

func PrometheusSample(value float64) *Evidence {
	return &Evidence{Payload: &PrometheusEvidence{Value: &value}}
}

func (e *Evidence) PrometheusValue() *float64 {
	if e != nil {
		if sample, ok := e.Payload.(*PrometheusEvidence); ok && sample != nil {
			return sample.Value
		}
	}
	return nil
}
