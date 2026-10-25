package controller

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	corev1 "k8s.io/api/core/v1"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/catalog"
)

// The generated assignment is a transport shape; parameter interpretation and
// delivery remain owned by the catalog, shared with local explanation.
func assignmentParameters(assignment api.RunAssignment) ([]corev1.EnvVar, []string, error) {
	declarations := map[string]catalog.Parameter{}
	if assignment.Action.ParameterDefinitions != nil {
		for name, definition := range *assignment.Action.ParameterDefinitions {
			parameter := catalog.Parameter{
				Type: string(definition.Type), Environment: valueOrEmpty(definition.Environment),
			}
			if definition.Required != nil {
				parameter.Required = *definition.Required
			}
			if definition.Values != nil {
				parameter.Values = *definition.Values
			}
			if definition.Argument != nil {
				parameter.Argument = &catalog.ParameterArgument{Name: valueOrEmpty(definition.Argument.Name)}
			}
			declarations[name] = parameter
		}
	}
	values := make(map[string]any, len(assignment.Parameters))
	for _, name := range slices.Sorted(maps.Keys(assignment.Parameters)) {
		encoded, err := assignment.Parameters[name].MarshalJSON()
		if err != nil {
			return nil, nil, fmt.Errorf("encode run parameter %q: %w", name, err)
		}
		var value any
		if err := json.Unmarshal(encoded, &value); err != nil {
			return nil, nil, fmt.Errorf("decode run parameter %q: %w", name, err)
		}
		values[name] = value
	}
	resolved, err := catalog.ResolveParameters(declarations, values)
	if err != nil {
		return nil, nil, fmt.Errorf("render run parameters: %w", err)
	}
	environment := make([]corev1.EnvVar, 0, len(resolved.Delivery.Environment))
	for _, name := range slices.Sorted(maps.Keys(resolved.Delivery.Environment)) {
		environment = append(environment, corev1.EnvVar{Name: name, Value: resolved.Delivery.Environment[name]})
	}

	return environment, resolved.Delivery.Arguments, nil
}
