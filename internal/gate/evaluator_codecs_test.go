package gate

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestGateDefinitionRequiresKnownEvaluator(t *testing.T) {
	valid := `evaluator: {type: prometheus, source: prod, query: up, condition: {operator: equal, value: 0}}`
	for _, input := range []string{
		"{}", "evaluator: {}", "evaluator: null", "prometheus: {source: prod}",
		strings.Replace(valid, "type: prometheus, ", "", 1),
		strings.Replace(valid, "prometheus", "missing", 1),
		strings.Replace(valid, "type: prometheus", "type: prometheus, type: prometheus", 1),
		strings.Replace(valid, "source: prod", "extra: oops, source: prod", 1),
		strings.Replace(valid, ", value: 0", "", 1),
		strings.Replace(valid, "value: 0", "value: null", 1),
		strings.Replace(valid, "query: up", "query: 123", 1),
		strings.Replace(valid, "source: prod", "source: true", 1),
	} {
		var definition Definition
		if err := yaml.Unmarshal([]byte(input), &definition); err == nil && definition.Validate() == nil {
			t.Errorf("accepted malformed definition %s", input)
		}
	}
	var def, roundTrip Definition
	if err := yaml.Unmarshal([]byte(valid), &def); err != nil {
		t.Fatal(err)
	}
	encoded, err := yaml.Marshal(def)
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(encoded, &roundTrip); err != nil || !reflect.DeepEqual(roundTrip, def) {
		t.Fatalf("YAML round trip %s, %v", encoded, err)
	}
}

func TestDecodeTypedEvidence(t *testing.T) {
	for _, input := range []string{`{}`, `{"prometheus":null}`, `{"prometheus":{}}`, `{"other":{"value":0}}`,
		`{"prometheus":{"value":null}}`, `{"prometheus":{"value":"NaN"}}`, `{"prometheus":{"value":1e400}}`,
		`{"prometheus":{"value":0,"labels":{}}}`, `{"prometheus":{"value":0},"other":{}}`} {
		var evidence Evidence
		if err := json.Unmarshal([]byte(input), &evidence); err == nil {
			t.Errorf("accepted invalid evidence %s", input)
		}
	}
}
