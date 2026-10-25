package controller

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/slipramp/slipramp/internal/api"
)

func TestAssignmentParametersUsesCatalogDelivery(t *testing.T) {
	t.Parallel()

	for _, boolean := range []string{"true", "false"} {
		t.Run(boolean, func(t *testing.T) {
			t.Parallel()
			assignment := testRunAssignment(t)
			// Deliberately order declarations and values differently from the
			// required lexical delivery order.
			definitions := `{
  "z-env":{"type":"boolean","environment":"BOOL"},
  "optional":{"type":"string","argument":{}},
  "d-number":{"type":"number","argument":{"name":"--count"}},
  "b-positional":{"type":"boolean","argument":{}},
  "a-flag":{"type":"boolean","argument":{"name":"--force"}}
}`
			var declarations map[string]api.Parameter
			if err := json.Unmarshal([]byte(definitions), &declarations); err != nil {
				t.Fatal(err)
			}
			assignment.Action.ParameterDefinitions = &declarations
			values := `{"z-env":false,"d-number":1e2,"b-positional":false,"a-flag":` + boolean + `}`
			assignment.Parameters = nil
			if err := json.Unmarshal([]byte(values), &assignment.Parameters); err != nil {
				t.Fatal(err)
			}
			environment, arguments, err := assignmentParameters(assignment)
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"false", "--count", "100"}
			if boolean == "true" {
				want = append([]string{"--force"}, want...)
			}
			if !reflect.DeepEqual(arguments, want) || len(environment) != 1 ||
				environment[0].Name != "BOOL" || environment[0].Value != "false" {
				t.Errorf("delivery = %#v, %#v; want argv %#v and BOOL=false", arguments, environment, want)
			}
		})
	}
}
