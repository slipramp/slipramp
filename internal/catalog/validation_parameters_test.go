package catalog_test

import (
	"testing"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/testcheck"
)

func TestValidateParameters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		parameter          catalog.Parameter
		runtimeEnvironment string
		wantError          string
	}{
		{
			name: "string environment",
			parameter: catalog.Parameter{
				Type:        catalog.ParameterTypeString,
				Required:    true,
				Environment: "VERSION",
			},
		},
		{
			name: "boolean flag",
			parameter: catalog.Parameter{
				Type:     catalog.ParameterTypeBoolean,
				Argument: &catalog.ParameterArgument{Name: "--force"},
			},
		},
		{
			name: "number positional argument",
			parameter: catalog.Parameter{
				Type:     catalog.ParameterTypeNumber,
				Argument: &catalog.ParameterArgument{},
			},
		},
		{
			name: "enum argument",
			parameter: catalog.Parameter{
				Type:     catalog.ParameterTypeEnum,
				Values:   []string{"safe", "fast"},
				Argument: &catalog.ParameterArgument{Name: "--mode"},
			},
		},
		{
			name:      "unknown type",
			parameter: catalog.Parameter{Type: "integer", Environment: "COUNT"},
			wantError: "type must be string, boolean, number, or enum",
		},
		{
			name: "enum without values",
			parameter: catalog.Parameter{
				Type: catalog.ParameterTypeEnum, Environment: "MODE",
			},
			wantError: "must define values",
		},
		{
			name: "duplicate enum value",
			parameter: catalog.Parameter{
				Type: catalog.ParameterTypeEnum, Values: []string{"safe", "safe"}, Environment: "MODE",
			},
			wantError: "is duplicated",
		},
		{
			name: "values on string",
			parameter: catalog.Parameter{
				Type: catalog.ParameterTypeString, Values: []string{"value"}, Environment: "VALUE",
			},
			wantError: "only valid for enum",
		},
		{
			name:      "missing mapping",
			parameter: catalog.Parameter{Type: catalog.ParameterTypeString},
			wantError: "exactly one",
		},
		{
			name: "multiple mappings",
			parameter: catalog.Parameter{
				Type: catalog.ParameterTypeString, Environment: "VALUE", Argument: &catalog.ParameterArgument{},
			},
			wantError: "exactly one",
		},
		{
			name: "invalid environment",
			parameter: catalog.Parameter{
				Type: catalog.ParameterTypeString, Environment: "invalid-name",
			},
			wantError: "must start with",
		},
		{
			name: "environment collision",
			parameter: catalog.Parameter{
				Type: catalog.ParameterTypeString, Environment: "VALUE",
			},
			runtimeEnvironment: "VALUE",
			wantError:          "conflicts with runtime environment",
		},
		{
			name: "invalid argument name",
			parameter: catalog.Parameter{
				Type: catalog.ParameterTypeString, Argument: &catalog.ParameterArgument{Name: "--bad name"},
			},
			wantError: "must be one argument",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			input := validCatalog()
			if test.runtimeEnvironment != "" {
				input.Runtimes[0].Spec.Environment = map[string]string{
					test.runtimeEnvironment: "runtime-value",
				}
			}
			action := input.Targets[0].Spec.Actions["preview"]
			action.Parameters = map[string]catalog.Parameter{"value": test.parameter}
			input.Targets[0].Spec.Actions["preview"] = action

			testcheck.ErrorContains(t, catalogValidationError(input, testPlacements(t)), test.wantError)
		})
	}
}
