package catalog

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// NormalizeParameterValues converts supported Go numeric types to the
// JSON-compatible float64 representation used by persisted Run parameters.
func NormalizeParameterValues(values map[string]any) (map[string]any, error) {
	normalized := make(map[string]any, len(values))
	for _, name := range slices.Sorted(maps.Keys(values)) {
		value := values[name]
		switch typed := value.(type) {
		case string:
			normalized[name] = typed
		case bool:
			normalized[name] = typed
		default:
			number, ok := parameterNumberValue(value)
			if !ok {
				return nil, fmt.Errorf("parameter %q must be a string, boolean, or number", name)
			}

			normalized[name] = number
		}
	}

	return normalized, nil
}

// ValidateParameterValues verifies normalized parameter values against an
// action's declarations and returns a detached copy for durable use.
func ValidateParameterValues(declarations map[string]Parameter, values map[string]any) (map[string]any, error) {
	validated := make(map[string]any, len(values))

	for _, name := range slices.Sorted(maps.Keys(values)) {
		declaration, found := declarations[name]
		if !found {
			return nil, fmt.Errorf("parameter %q is not declared by the action", name)
		}

		value, err := validateParameterValue(name, declaration, values[name])
		if err != nil {
			return nil, err
		}

		validated[name] = value
	}

	for _, name := range slices.Sorted(maps.Keys(declarations)) {
		if declarations[name].Required {
			if _, supplied := validated[name]; !supplied {
				return nil, fmt.Errorf("required parameter %q was not supplied", name)
			}
		}
	}

	return validated, nil
}

// NormalizeAndValidateParameterValues applies the same primitive
// normalization and action declaration validation used by Run creation.
func NormalizeAndValidateParameterValues(
	declarations map[string]Parameter,
	values map[string]any,
) (map[string]any, error) {
	normalized, err := NormalizeParameterValues(values)
	if err != nil {
		return nil, err
	}

	return ValidateParameterValues(declarations, normalized)
}

func validateParameterValue(name string, declaration Parameter, value any) (any, error) {
	switch declaration.Type {
	case ParameterTypeString:
		if typed, ok := value.(string); ok {
			return typed, nil
		}
	case ParameterTypeBoolean:
		if typed, ok := value.(bool); ok {
			return typed, nil
		}
	case ParameterTypeNumber:
		if typed, ok := parameterNumberValue(value); ok {
			return typed, nil
		}
	case ParameterTypeEnum:
		typed, ok := value.(string)
		if ok && slices.Contains(declaration.Values, typed) {
			return typed, nil
		}
		if ok {
			return nil, fmt.Errorf(
				"parameter %q must be one of %s",
				name,
				strings.Join(declaration.Values, ", "),
			)
		}
	}

	return nil, fmt.Errorf("parameter %q must be a %s", name, declaration.Type)
}

func parameterNumberValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint:
		return float64(typed), true
	case uint32:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	default:
		return 0, false
	}
}
