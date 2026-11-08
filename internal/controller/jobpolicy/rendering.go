// Package jobpolicy renders the Placement-owned pod policy shared by
// execution and preflight Jobs.
package jobpolicy

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/catalog"
)

const (
	ManagedByLabel      = "app.kubernetes.io/managed-by"
	ManagedByLabelValue = "slipramp-controller"
	ClusterLabel        = "slipramp.com/cluster"
)

func PodTemplate(encoded *map[string]any) (corev1.PodTemplateSpec, error) {
	if encoded == nil {
		return corev1.PodTemplateSpec{}, nil
	}

	data, err := json.Marshal(encoded)
	if err != nil {
		return corev1.PodTemplateSpec{}, fmt.Errorf("encode pod template: %w", err)
	}

	var template corev1.PodTemplateSpec
	if err := json.Unmarshal(data, &template); err != nil {
		return corev1.PodTemplateSpec{}, fmt.Errorf("decode pod template: %w", err)
	}

	if len(template.Spec.Containers) != 0 || len(template.Spec.InitContainers) != 0 {
		return corev1.PodTemplateSpec{}, fmt.Errorf("pod template must not define containers")
	}

	return template, nil
}

func Resources(
	base *api.ResourceRequirements,
	overrides *api.ResourceRequirements,
) (corev1.ResourceRequirements, error) {
	resources := catalog.MergeResources(resourceValues(base), resourceValues(overrides))
	requests, err := parseResourceList("request", resources.Requests)
	if err != nil {
		return corev1.ResourceRequirements{}, err
	}
	limits, err := parseResourceList("limit", resources.Limits)
	if err != nil {
		return corev1.ResourceRequirements{}, err
	}

	return corev1.ResourceRequirements{Requests: requests, Limits: limits}, nil
}

func resourceValues(resources *api.ResourceRequirements) catalog.ResourceRequirements {
	if resources == nil {
		return catalog.ResourceRequirements{}
	}

	return catalog.ResourceRequirements{
		Requests: cloneOptionalMap(resources.Requests),
		Limits:   cloneOptionalMap(resources.Limits),
	}
}

func parseResourceList(kind string, values map[string]string) (corev1.ResourceList, error) {
	if len(values) == 0 {
		return nil, nil
	}

	result := make(corev1.ResourceList, len(values))
	for _, name := range slices.Sorted(maps.Keys(values)) {
		quantity, err := resource.ParseQuantity(values[name])
		if err != nil {
			return nil, fmt.Errorf("parse resource %s %q: %w", kind, name, err)
		}
		result[corev1.ResourceName(name)] = quantity
	}

	return result, nil
}

func cloneOptionalMap(value *map[string]string) map[string]string {
	if value == nil {
		return nil
	}

	return maps.Clone(*value)
}
