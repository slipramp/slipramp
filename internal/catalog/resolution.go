package catalog

import "maps"

// EffectiveAction is an action with its target-level execution defaults
// applied. Maps and slices are copied so callers can safely freeze or modify a
// resolved action without changing the loaded catalog.
type EffectiveAction struct {
	Name        string
	Intent      string
	Command     []string
	Runtime     string
	Placement   string
	Credentials map[string]string
	Timeout     string
	Resources   ResourceRequirements
	Parameters  map[string]Parameter
	Artifacts   map[string]Artifact
	Inputs      map[string]Input
}

func (t Target) EffectiveAction(name string) (EffectiveAction, bool) {
	action, found := t.Spec.Actions[name]
	if !found {
		return EffectiveAction{}, false
	}

	runtimeName := action.Runtime
	if runtimeName == "" {
		runtimeName = t.Spec.Runtime
	}

	placementName := action.Placement
	if placementName == "" {
		placementName = t.Spec.Placement
	}

	timeout := action.Timeout
	if timeout == "" {
		timeout = t.Spec.Timeout
	}

	credentials := maps.Clone(t.Spec.Credentials)
	if credentials == nil && action.Credentials != nil {
		credentials = make(map[string]string, len(action.Credentials))
	}
	maps.Copy(credentials, action.Credentials)

	requests := maps.Clone(t.Spec.Resources.Requests)
	if requests == nil && action.Resources.Requests != nil {
		requests = make(map[string]string, len(action.Resources.Requests))
	}
	maps.Copy(requests, action.Resources.Requests)

	limits := maps.Clone(t.Spec.Resources.Limits)
	if limits == nil && action.Resources.Limits != nil {
		limits = make(map[string]string, len(action.Resources.Limits))
	}
	maps.Copy(limits, action.Resources.Limits)

	return EffectiveAction{
		Name:        name,
		Intent:      action.Intent,
		Command:     append([]string(nil), action.Command...),
		Runtime:     runtimeName,
		Placement:   placementName,
		Credentials: credentials,
		Timeout:     timeout,
		Resources: ResourceRequirements{
			Requests: requests,
			Limits:   limits,
		},
		Parameters: cloneParameters(action.Parameters),
		Artifacts:  maps.Clone(action.Artifacts),
		Inputs:     cloneInputs(action.Inputs),
	}, true
}

func cloneParameters(input map[string]Parameter) map[string]Parameter {
	if input == nil {
		return nil
	}

	cloned := make(map[string]Parameter, len(input))
	for name, parameter := range input {
		parameter.Values = append([]string(nil), parameter.Values...)
		if parameter.Argument != nil {
			argument := *parameter.Argument
			parameter.Argument = &argument
		}
		cloned[name] = parameter
	}

	return cloned
}

func cloneInputs(input map[string]Input) map[string]Input {
	if input == nil {
		return nil
	}

	cloned := make(map[string]Input, len(input))
	for name, item := range input {
		if item.Artifact.Compatibility == "" {
			item.Artifact.Compatibility =
				ArtifactCompatibilityExactCatalogRevision
		}
		cloned[name] = item
	}

	return cloned
}
