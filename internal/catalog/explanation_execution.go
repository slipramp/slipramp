package catalog

import (
	"maps"
	"slices"

	"github.com/slipramp/slipramp/internal/placement"
)

func (b explanationBuilder) explainPlacement(result *Explanation, cfg *placement.Config) {
	declared := b.target.Spec.Actions[b.action.Name]
	result.Placement = PlacementExplanation{
		Name:           explained(b.action.Placement, b.scalarOrigin("placement", declared.Placement)),
		Cluster:        unresolved[string](b.placementOrigin("cluster")),
		Namespace:      unresolved[string](b.placementOrigin("jobNamespace")),
		ServiceAccount: unresolved[string](b.placementOrigin("serviceAccountName")),
		NodeSelector:   unresolved[map[string]string](b.placementOrigin("nodeSelector")),
		PodExtensions:  unresolved[bool](b.placementOrigin("podTemplate")),
	}
	result.Timeout = unresolved[string](b.placementOrigin("timeout"))
	baseResources := ResourceRequirements{}
	if cfg != nil {
		result.Placement.ConfigurationAvailable = true
		result.Placement.Cluster = explained(cfg.ClusterName, b.placementOrigin("cluster"))
		result.Placement.Namespace = explained(cfg.JobNamespace, b.placementOrigin("jobNamespace"))
		result.Placement.ServiceAccount = explained(cfg.ServiceAccountName, b.placementOrigin("serviceAccountName"))
		if cfg.ServiceAccountName == "" {
			result.Placement.ServiceAccount = explained("default", systemOrigin("serviceAccountName"))
		}
		selector := maps.Clone(cfg.NodeSelector)
		if selector == nil {
			selector = map[string]string{}
		}
		result.Placement.NodeSelector = explained(selector, b.placementOrigin("nodeSelector"))
		result.Placement.PodExtensions = explained(cfg.PodTemplate != nil, b.placementOrigin("podTemplate"))
		result.Timeout = explained("", systemOrigin("timeout"))
		if cfg.Timeout > 0 {
			result.Timeout = explained(cfg.Timeout.String(), b.placementOrigin("timeout"))
		}
		baseResources = ResourceRequirements{Requests: cfg.Resources.Requests, Limits: cfg.Resources.Limits}
	}
	if b.action.Timeout != "" {
		result.Timeout = explained(b.action.Timeout, b.scalarOrigin("timeout", declared.Timeout))
	}

	resources := MergeResources(baseResources, b.action.Resources)
	result.Resources = ResourcesExplanation{
		Requests:                  b.explainResourceMap("requests", resources.Requests),
		Limits:                    b.explainResourceMap("limits", resources.Limits),
		PlacementDefaultsResolved: cfg != nil,
	}
	if cfg == nil {
		result.Resources.UnresolvedReason = missingPlacementReason
	}
}

func (b explanationBuilder) explainResourceMap(
	kind string,
	effective map[string]string,
) map[string]ExplainedValue[string] {
	declared := b.target.Spec.Actions[b.action.Name]
	actionValues := declared.Resources.Requests
	targetValues := b.target.Spec.Resources.Requests
	if kind == "limits" {
		actionValues = declared.Resources.Limits
		targetValues = b.target.Spec.Resources.Limits
	}
	result := make(map[string]ExplainedValue[string], len(effective))
	for name, value := range effective {
		field := "resources." + kind + "." + name
		origin := b.placementOrigin(field)
		if _, ok := targetValues[name]; ok {
			origin = b.targetOrigin("spec." + field)
			origin.Kind = "target-default"
		}
		if _, ok := actionValues[name]; ok {
			origin = b.actionOrigin("." + field)
			origin.Kind = "action-override"
		}
		result[name] = explained(value, origin)
	}

	return result
}

func (b explanationBuilder) explainCredentials(result *Explanation, cfg *placement.Config) {
	for _, name := range slices.Sorted(maps.Keys(b.runtime.Spec.Credentials)) {
		binding := b.action.Credentials[name]
		field := effectiveCredentialFieldPath(b.target, b.action.Name, name)
		origin := b.targetOrigin(field)
		origin.Kind = "target-default"
		if _, ok := b.target.Spec.Actions[b.action.Name].Credentials[name]; ok {
			origin = b.actionOrigin(".credentials." + name)
			origin.Kind = "action-override"
		}
		credential := CredentialExplanation{
			Requirement: name,
			Binding:     explained(binding, origin),
			Source:      unresolved[CredentialSourceExplanation](b.placementOrigin("credentials." + binding)),
			Injection: explained(
				b.runtime.Spec.Credentials[name].Inject,
				b.runtimeOrigin("credentials."+name+".inject"),
			),
		}
		if cfg != nil {
			source := cfg.Credentials[binding]
			credential.Source = explained(
				CredentialSourceExplanation{SecretName: source.SecretRef.Name},
				b.placementOrigin("credentials."+binding+".secretRef.name"),
			)
		}
		result.Credentials = append(result.Credentials, credential)
	}
}
