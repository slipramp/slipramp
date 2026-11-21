package gate_test

import (
	"math"
	"strings"
	"testing"

	"github.com/slipramp/slipramp/internal/gate"
	"github.com/slipramp/slipramp/internal/testcheck"
)

func TestDefinitionDigest(t *testing.T) {
	t.Parallel()

	definition := validDefinition()
	digest, err := definition.Digest()
	if err != nil {
		t.Fatalf("Digest() error = %v", err)
	}
	const expectedDigest = "e4684a174dde536d54bbf4061b007d22cec86db231ef242b976d32fcd2a4fb23"
	if digest != expectedDigest {
		t.Fatalf("Digest() = %q, want %q", digest, expectedDigest)
	}

	changed := definition
	changed.Prometheus.Query = "active_sessions > 0"
	changedDigest, err := changed.Digest()
	if err != nil {
		t.Fatalf("changed Digest() error = %v", err)
	}
	if changedDigest == digest {
		t.Errorf("changed Digest() = %q, want a different digest", changedDigest)
	}
	negativeZero := definition
	negativeZero.Prometheus.Condition.Value = math.Copysign(0, -1)
	if got, err := negativeZero.Digest(); err != nil || got != digest {
		t.Errorf("negative zero digest = %q, %v, want %q", got, err, digest)
	}
}

func TestDefinitionValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		configure func(*gate.Definition)
		wantError string
	}{
		{name: "valid"},
		{
			name: "source",
			configure: func(definition *gate.Definition) {
				definition.Prometheus.Source = "NOT_VALID"
			},
			wantError: "Gate Prometheus source",
		},
		{
			name: "query whitespace",
			configure: func(definition *gate.Definition) {
				definition.Prometheus.Query = " active_sessions"
			},
			wantError: "leading or trailing whitespace",
		},
		{
			name: "query length",
			configure: func(definition *gate.Definition) {
				definition.Prometheus.Query = strings.Repeat("x", gate.MaximumQueryLength+1)
			},
			wantError: "must not exceed",
		},
		{
			name: "operator",
			configure: func(definition *gate.Definition) {
				definition.Prometheus.Condition.Operator = "approximately"
			},
			wantError: "is not supported",
		},
		{
			name: "threshold",
			configure: func(definition *gate.Definition) {
				definition.Prometheus.Condition.Value = math.Inf(1)
			},
			wantError: "must be finite",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			definition := validDefinition()
			if test.configure != nil {
				test.configure(&definition)
			}
			testcheck.ErrorContains(t, definition.Validate(), test.wantError)
		})
	}
}

func TestConditionCompare(t *testing.T) {
	t.Parallel()

	tests := []struct {
		operator string
		value    float64
		want     bool
	}{
		{operator: gate.OperatorEqual, value: 2, want: true},
		{operator: gate.OperatorEqual, value: 1, want: false},
		{operator: gate.OperatorNotEqual, value: 1, want: true},
		{operator: gate.OperatorLessThan, value: 1, want: true},
		{operator: gate.OperatorLessThanOrEqual, value: 2, want: true},
		{operator: gate.OperatorGreaterThan, value: 3, want: true},
		{operator: gate.OperatorGreaterThanOrEqual, value: 2, want: true},
	}

	for _, test := range tests {
		t.Run(test.operator, func(t *testing.T) {
			t.Parallel()

			condition := gate.Condition{Operator: test.operator, Value: 2}
			got, err := condition.Compare(test.value)
			if err != nil {
				t.Fatalf("Compare() error = %v", err)
			}
			if got != test.want {
				t.Errorf("Compare() = %t, want %t", got, test.want)
			}
		})
	}

	condition := gate.Condition{Operator: gate.OperatorEqual, Value: 0}
	_, err := condition.Compare(math.NaN())
	testcheck.ErrorContains(t, err, "observation value must be finite")
}

func validDefinition() gate.Definition {
	return gate.Definition{
		Prometheus: gate.PrometheusDefinition{
			Source: "homelab",
			Query:  "active_sessions",
			Condition: gate.Condition{
				Operator: gate.OperatorLessThanOrEqual,
				Value:    0,
			},
		},
	}
}
