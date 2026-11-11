package kubevalidate

import (
	"fmt"
	"maps"
	"slices"

	"k8s.io/apimachinery/pkg/api/resource"
)

func ResourceValues(subject string, values map[string]string) (map[string]resource.Quantity, error) {
	quantities := make(map[string]resource.Quantity, len(values))
	for _, name := range slices.Sorted(maps.Keys(values)) {
		if err := LabelKey(subject+" name", name); err != nil {
			return nil, err
		}

		quantity, err := resource.ParseQuantity(values[name])
		if err != nil {
			return nil, fmt.Errorf("%s %q has invalid quantity %q: %w", subject, name, values[name], err)
		}
		if quantity.Sign() < 0 {
			return nil, fmt.Errorf("%s %q must not be negative", subject, name)
		}

		quantities[name] = quantity
	}

	return quantities, nil
}

func ResourceRequirements(subject string, requests, limits map[string]string) error {
	requestSubject := joinSubject(subject, "resource request")
	parsedRequests, err := ResourceValues(requestSubject, requests)
	if err != nil {
		return err
	}

	parsedLimits, err := ResourceValues(joinSubject(subject, "resource limit"), limits)
	if err != nil {
		return err
	}

	for _, name := range slices.Sorted(maps.Keys(parsedRequests)) {
		request := parsedRequests[name]
		limit, found := parsedLimits[name]
		if found && request.Cmp(limit) > 0 {
			return fmt.Errorf("%s %q must not exceed its limit", requestSubject, name)
		}
	}

	return nil
}

func joinSubject(prefix, value string) string {
	if prefix == "" {
		return value
	}

	return prefix + " " + value
}
