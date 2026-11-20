package gate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/slipramp/slipramp/internal/kubevalidate"
)

const (
	MaximumQueryLength = 16 * 1024

	OperatorEqual              = "equal"
	OperatorNotEqual           = "not-equal"
	OperatorLessThan           = "less-than"
	OperatorLessThanOrEqual    = "less-than-or-equal"
	OperatorGreaterThan        = "greater-than"
	OperatorGreaterThanOrEqual = "greater-than-or-equal"
)

// Definition is the catalog-independent policy evaluated for one Gate name.
// Presentation metadata and the Gate name are deliberately excluded so
// equivalent definitions share one evaluation.
type Definition struct {
	Prometheus PrometheusDefinition `json:"prometheus"`
}

type PrometheusDefinition struct {
	Source    string    `json:"source"`
	Query     string    `json:"query"`
	Condition Condition `json:"condition"`
}

type Condition struct {
	Operator string  `json:"operator"`
	Value    float64 `json:"value"`
}

func (d Definition) Validate() error {
	if err := kubevalidate.DNSLabel("Gate Prometheus source", d.Prometheus.Source); err != nil {
		return err
	}
	if err := ValidateQuery(d.Prometheus.Query); err != nil {
		return err
	}
	if err := d.Prometheus.Condition.Validate(); err != nil {
		return err
	}

	return nil
}

func (d Definition) Digest() (string, error) {
	if err := d.Validate(); err != nil {
		return "", err
	}

	normalized := d
	if normalized.Prometheus.Condition.Value == 0 {
		// Equal numeric thresholds must have equal digests. JSON otherwise
		// preserves the sign bit on negative zero.
		normalized.Prometheus.Condition.Value = 0
	}
	contents, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("encode Gate definition: %w", err)
	}

	digest := sha256.Sum256(contents)

	return hex.EncodeToString(digest[:]), nil
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
	if !isFinite(c.Value) {
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
