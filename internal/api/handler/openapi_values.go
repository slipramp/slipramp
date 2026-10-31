package handler

import "maps"

// These helpers translate domain zero values into omitted OpenAPI fields.
func optionalString(value string) *string {
	if value == "" {
		return nil
	}

	return &value
}

func optionalBool(value bool) *bool {
	if !value {
		return nil
	}

	return &value
}

func optionalUint32(value uint32) *uint32 {
	if value == 0 {
		return nil
	}

	return &value
}

func optionalStringMap(value map[string]string) *map[string]string {
	if len(value) == 0 {
		return nil
	}

	cloned := maps.Clone(value)

	return &cloned
}

func optionalMap[K comparable, V any](value map[K]V) *map[K]V {
	if len(value) == 0 {
		return nil
	}

	return &value
}

func optionalSlice[T any](value []T) *[]T {
	if len(value) == 0 {
		return nil
	}

	cloned := append([]T(nil), value...)

	return &cloned
}
