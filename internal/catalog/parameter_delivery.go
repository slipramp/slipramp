package catalog

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
)

type ParameterDelivery struct {
	Arguments   []string          `json:"arguments"`
	Environment map[string]string `json:"environment"`
}

type ResolvedParameters struct {
	Values     map[string]any
	Delivery   ParameterDelivery
	Parameters map[string]ParameterDelivery
}

// ParseParameterValues converts CLI strings according to the Action's declared
// types. Strings and enums remain literal, including values resembling JSON.
func ParseParameterValues(declarations map[string]Parameter, supplied map[string]string) (map[string]any, error) {
	values := make(map[string]any, len(supplied))
	for _, name := range slices.Sorted(maps.Keys(supplied)) {
		declaration, found := declarations[name]
		if !found {
			return nil, fmt.Errorf("parameter %q is not declared by the action", name)
		}

		text := supplied[name]
		switch declaration.Type {
		case ParameterTypeBoolean:
			value, err := strconv.ParseBool(text)
			if err != nil {
				return nil, fmt.Errorf("parameter %q must be a boolean", name)
			}
			values[name] = value
		case ParameterTypeNumber:
			value, err := strconv.ParseFloat(text, 64)
			if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
				return nil, fmt.Errorf("parameter %q must be a finite number", name)
			}
			values[name] = value
		default:
			values[name] = text
		}
	}

	return NormalizeAndValidateParameterValues(declarations, values)
}

// ResolveParameters is shared by static explanation and Controller execution.
// It validates the same normalized values accepted at Run creation and emits
// arguments in lexical parameter-name order, omitting unset optional values.
func ResolveParameters(declarations map[string]Parameter, values map[string]any) (ResolvedParameters, error) {
	validated, err := NormalizeAndValidateParameterValues(declarations, values)
	if err != nil {
		return ResolvedParameters{}, err
	}

	result := ResolvedParameters{
		Values:     validated,
		Delivery:   ParameterDelivery{Arguments: []string{}, Environment: map[string]string{}},
		Parameters: make(map[string]ParameterDelivery, len(declarations)),
	}
	for _, name := range slices.Sorted(maps.Keys(declarations)) {
		declaration := declarations[name]
		delivery := ParameterDelivery{Arguments: []string{}, Environment: map[string]string{}}
		value, supplied := validated[name]
		if supplied {
			text := parameterText(value)
			switch {
			case declaration.Environment != "":
				delivery.Environment[declaration.Environment] = text
			case declaration.Argument == nil:
				return ResolvedParameters{}, fmt.Errorf("parameter %q has no execution mapping", name)
			case declaration.Argument.Name == "":
				delivery.Arguments = append(delivery.Arguments, text)
			case declaration.Type == ParameterTypeBoolean:
				if value == true {
					delivery.Arguments = append(delivery.Arguments, declaration.Argument.Name)
				}
			default:
				delivery.Arguments = append(delivery.Arguments, declaration.Argument.Name, text)
			}
		}
		result.Parameters[name] = delivery
		result.Delivery.Arguments = append(result.Delivery.Arguments, delivery.Arguments...)
		maps.Copy(result.Delivery.Environment, delivery.Environment)
	}

	return result, nil
}

func parameterText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case bool:
		return strconv.FormatBool(typed)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		panic("parameter value was not normalized")
	}
}
