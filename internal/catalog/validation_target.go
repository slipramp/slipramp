package catalog

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/slipramp/slipramp/internal/kubevalidate"
	"github.com/slipramp/slipramp/internal/placement"
)

func validateTargetPortable(input Catalog, target Target, runtimes map[string]Runtime) Diagnostics {
	diagnostics := Diagnostics{}
	add := func(fieldPath, code string, err error) {
		if err != nil {
			diagnostics = append(diagnostics, errorDiagnostic(
				input,
				TargetKind,
				target.Metadata.Name,
				target.Source,
				fieldPath,
				code,
				err,
			))
		}
	}

	add(
		"metadata.name",
		"catalog.target.name-invalid",
		kubevalidate.DNSLabel("target metadata.name", target.Metadata.Name),
	)

	if target.Spec.Path != "" {
		add("spec.path", "catalog.target.path-invalid", validateRepositoryPath("target path", target.Spec.Path))
	}

	seenWatchPatterns := map[string]int{}
	for index, pattern := range target.Spec.Watch {
		fieldPath := fmt.Sprintf("spec.watch.%d", index)
		subject := fmt.Sprintf("target watch pattern %d", index+1)
		if err := validateRepositoryPattern(subject, pattern); err != nil {
			add(fieldPath, "catalog.watch.invalid", err)
			continue
		}
		if previous, found := seenWatchPatterns[pattern]; found {
			item := diagnostic(
				input,
				TargetKind,
				target.Metadata.Name,
				target.Source,
				fieldPath,
				"catalog.watch.duplicate",
				SeverityWarning,
				fmt.Errorf("target watch pattern %q is duplicated", pattern),
			)
			item.Hint = "Remove the duplicate pattern; it does not expand the Target's watched paths."
			item.Related = []RelatedLocation{{
				Message: "The first equivalent pattern is here.",
				Location: input.location(
					TargetKind,
					target.Metadata.Name,
					fmt.Sprintf("spec.watch.%d", previous),
					target.Source,
				),
			}}
			diagnostics = append(diagnostics, item)
			continue
		}
		seenWatchPatterns[pattern] = index
	}
	for index, pattern := range target.Spec.Watch {
		if validateRepositoryPattern("target watch pattern", pattern) != nil {
			continue
		}
		for broaderIndex, broader := range target.Spec.Watch {
			if broaderIndex == index ||
				validateRepositoryPattern("target watch pattern", broader) != nil ||
				!recursivePatternSubsumes(broader, pattern) {
				continue
			}

			fieldPath := fmt.Sprintf("spec.watch.%d", index)
			item := diagnostic(
				input,
				TargetKind,
				target.Metadata.Name,
				target.Source,
				fieldPath,
				"catalog.watch.subsumed",
				SeverityWarning,
				fmt.Errorf("target watch pattern %q is completely covered by %q", pattern, broader),
			)
			item.Hint = "Remove the narrower pattern; it does not expand the Target's watched paths."
			item.Related = []RelatedLocation{{
				Message: "The broader pattern is here.",
				Location: input.location(
					TargetKind,
					target.Metadata.Name,
					fmt.Sprintf("spec.watch.%d", broaderIndex),
					target.Source,
				),
			}}
			diagnostics = append(diagnostics, item)

			break
		}
	}

	if target.Spec.Runtime != "" {
		if _, found := runtimes[target.Spec.Runtime]; !found {
			add(
				"spec.runtime",
				"catalog.runtime.not-found",
				fmt.Errorf("runtime %q is not defined", target.Spec.Runtime),
			)
		}
	}

	if target.Spec.Placement != "" {
		add(
			"spec.placement",
			"catalog.placement.name-invalid",
			kubevalidate.DNSLabel("target placement", target.Spec.Placement),
		)
	}

	add(
		"spec.credentials",
		"catalog.credential.binding-invalid",
		validateCredentialBindings("target", target.Spec.Credentials),
	)
	add("spec.timeout", "catalog.timeout.invalid", validateTimeout("target timeout", target.Spec.Timeout))
	add("spec.resources", "catalog.resource.invalid", validateResourceValues("target", target.Spec.Resources))

	if len(target.Spec.Actions) == 0 {
		add("spec.actions", "catalog.action.required", errors.New("target must define at least one action"))
	}

	for _, name := range slices.Sorted(maps.Keys(target.Spec.Actions)) {
		diagnostics = append(diagnostics, validateActionPortable(input, target, name, runtimes)...)
	}
	if err := validateTargetCredentialUsage(target, runtimes); err != nil {
		add("spec.credentials", "catalog.credential.unused", err)
	}

	diagnostics = append(diagnostics, validateInputReferences(input, target)...)

	return diagnostics
}

func validateActionPortable(
	input Catalog,
	target Target,
	actionName string,
	runtimes map[string]Runtime,
) Diagnostics {
	diagnostics := Diagnostics{}
	action := target.Spec.Actions[actionName]
	effective, _ := target.EffectiveAction(actionName)
	basePath := "spec.actions." + actionName
	add := func(fieldPath, code string, err error) {
		if err != nil {
			diagnostics = append(diagnostics, errorDiagnostic(
				input,
				TargetKind,
				target.Metadata.Name,
				target.Source,
				basePath+fieldPath,
				code,
				err,
			))
		}
	}

	add("", "catalog.action.name-invalid", kubevalidate.DNSLabel("action name", actionName))
	if err := validateTriggers(target, actionName, action); err != nil {
		add(".triggers", triggerDiagnosticCode(err), err)
	}
	add(".intent", "catalog.intent.invalid", validateIntent(actionName, action.Intent))
	add(".command", "catalog.command.invalid", validateCommand(actionName, action.Command))

	var runtime Runtime
	runtimeFound := false
	if effective.Runtime == "" {
		add(".runtime", "catalog.runtime.required", fmt.Errorf("action %q must select a runtime", actionName))
	} else {
		runtime, runtimeFound = runtimes[effective.Runtime]
		if !runtimeFound && action.Runtime != "" {
			fieldPath := basePath + ".runtime"
			diagnostics = append(diagnostics, errorDiagnostic(
				input,
				TargetKind,
				target.Metadata.Name,
				target.Source,
				fieldPath,
				"catalog.runtime.not-found",
				fmt.Errorf("runtime %q is not defined", effective.Runtime),
			))
		}
	}

	if effective.Placement == "" {
		add(".placement", "catalog.placement.required", fmt.Errorf("action %q must select a placement", actionName))
	} else if action.Placement != "" {
		fieldPath := basePath + ".placement"
		if err := kubevalidate.DNSLabel(fmt.Sprintf("action %q placement", actionName), effective.Placement); err != nil {
			diagnostics = append(diagnostics, errorDiagnostic(
				input,
				TargetKind,
				target.Metadata.Name,
				target.Source,
				fieldPath,
				"catalog.placement.name-invalid",
				err,
			))
		}
	}

	add(
		".credentials",
		"catalog.credential.binding-invalid",
		validateCredentialBindings(fmt.Sprintf("action %q", actionName), action.Credentials),
	)
	if runtimeFound {
		for _, credentialName := range slices.Sorted(maps.Keys(action.Credentials)) {
			if _, found := runtime.Spec.Credentials[credentialName]; !found {
				add(
					".credentials."+credentialName,
					"catalog.credential.runtime-undeclared",
					fmt.Errorf(
						"action %q binds credential %q not declared by runtime %q",
						actionName,
						credentialName,
						effective.Runtime,
					),
				)
			}
		}
		for _, credentialName := range slices.Sorted(maps.Keys(runtime.Spec.Credentials)) {
			if effective.Credentials[credentialName] == "" {
				add(
					".credentials",
					"catalog.credential.binding-missing",
					fmt.Errorf("action %q must bind runtime credential %q", actionName, credentialName),
				)
			}
		}
	}

	add(".timeout", "catalog.timeout.invalid", validateTimeout(fmt.Sprintf("action %q timeout", actionName), action.Timeout))
	add(".resources", "catalog.resource.invalid", validateResourceValues(fmt.Sprintf("action %q", actionName), action.Resources))
	add(
		".resources",
		"catalog.resource.request-exceeds-limit",
		validateResources(fmt.Sprintf("action %q effective", actionName), effective.Resources),
	)

	if runtimeFound {
		add(".parameters", "catalog.parameter.invalid", validateParameters(actionName, action.Parameters, runtime))
	}
	add(".artifacts", "catalog.artifact.invalid", validateArtifacts(actionName, action.Artifacts))
	add(".inputs", "catalog.input.invalid", validateInputs(actionName, action.Inputs))
	for _, inputName := range slices.Sorted(maps.Keys(action.Inputs)) {
		item := action.Inputs[inputName]
		for _, artifactName := range slices.Sorted(maps.Keys(action.Artifacts)) {
			artifact := action.Artifacts[artifactName]
			if item.MountPath == artifact.Path {
				add(
					".inputs."+inputName+".mountPath",
					"catalog.artifact.mount-conflict",
					fmt.Errorf(
						"action %q input %q mountPath conflicts with artifact %q path",
						actionName,
						inputName,
						artifactName,
					),
				)
			}
		}
	}

	return diagnostics
}

func validateTargetDeployment(
	input Catalog,
	target Target,
	runtimes map[string]Runtime,
	placements placement.Set,
) Diagnostics {
	diagnostics := Diagnostics{}
	if target.Spec.Placement != "" {
		if _, found := placements.Lookup(target.Spec.Placement); !found {
			diagnostics = append(diagnostics, errorDiagnostic(
				input,
				TargetKind,
				target.Metadata.Name,
				target.Source,
				"spec.placement",
				"catalog.placement.not-found",
				fmt.Errorf("placement %q is not defined", target.Spec.Placement),
			))
		}
	}

	for _, actionName := range slices.Sorted(maps.Keys(target.Spec.Actions)) {
		action := target.Spec.Actions[actionName]
		effective, _ := target.EffectiveAction(actionName)
		if effective.Placement == "" {
			continue
		}
		placementConfig, found := placements.Lookup(effective.Placement)
		if !found {
			if action.Placement != "" || target.Spec.Placement == "" {
				diagnostics = append(diagnostics, errorDiagnostic(
					input,
					TargetKind,
					target.Metadata.Name,
					target.Source,
					"spec.actions."+actionName+".placement",
					"catalog.placement.not-found",
					fmt.Errorf("placement %q is not defined", effective.Placement),
				))
			}
			continue
		}

		runtime, runtimeFound := runtimes[effective.Runtime]
		if runtimeFound {
			for _, credentialName := range slices.Sorted(maps.Keys(runtime.Spec.Credentials)) {
				sourceName := effective.Credentials[credentialName]
				if sourceName == "" {
					continue
				}
				if _, offered := placementConfig.Credentials[sourceName]; !offered {
					diagnostics = append(diagnostics, errorDiagnostic(
						input,
						TargetKind,
						target.Metadata.Name,
						target.Source,
						effectiveCredentialFieldPath(target, actionName, credentialName),
						"catalog.credential.source-not-offered",
						fmt.Errorf(
							"action %q credential %q references source %q not offered by placement %q",
							actionName,
							credentialName,
							sourceName,
							effective.Placement,
						),
					))
				}
			}
		}

		if err := validateResources(
			fmt.Sprintf("action %q execution", actionName),
			mergePlacementResources(placementConfig.Resources, effective.Resources),
		); err != nil {
			diagnostics = append(diagnostics, errorDiagnostic(
				input,
				TargetKind,
				target.Metadata.Name,
				target.Source,
				"spec.actions."+actionName+".resources",
				"catalog.resource.request-exceeds-limit",
				err,
			))
		}
	}

	return diagnostics
}

func effectiveCredentialFieldPath(target Target, actionName, credentialName string) string {
	action := target.Spec.Actions[actionName]
	if _, found := action.Credentials[credentialName]; found {
		return "spec.actions." + actionName + ".credentials." + credentialName
	}

	return "spec.credentials." + credentialName
}

func validateTriggers(target Target, actionName string, action Action) error {
	seen := make(map[string]struct{}, len(action.Triggers))
	for _, trigger := range action.Triggers {
		if err := kubevalidate.DNSLabel(fmt.Sprintf("action %q trigger name", actionName), trigger.Name); err != nil {
			return err
		}
		if _, found := seen[trigger.Name]; found {
			return fmt.Errorf("action %q trigger name %q is duplicated", actionName, trigger.Name)
		}
		seen[trigger.Name] = struct{}{}

		if trigger.Git == nil {
			return fmt.Errorf("action %q trigger %q must declare git", actionName, trigger.Name)
		}
		if len(target.Spec.Watch) == 0 {
			return fmt.Errorf("action %q Git trigger %q requires target watch patterns", actionName, trigger.Name)
		}
		if _, err := NormalizeAndValidateParameterValues(action.Parameters, trigger.Parameters); err != nil {
			return fmt.Errorf(
				"action %q Git trigger %q has invalid parameters: %w",
				actionName,
				trigger.Name,
				err,
			)
		}
		if len(action.Inputs) > 0 {
			return fmt.Errorf("action %q Git trigger %q cannot invoke an action with artifact inputs", actionName, trigger.Name)
		}
	}

	return nil
}

func triggerDiagnosticCode(err error) string {
	if strings.Contains(err.Error(), "required parameter") {
		return "catalog.parameter.required"
	}

	return "catalog.trigger.invalid"
}

func recursivePatternSubsumes(broader, narrower string) bool {
	if broader == narrower {
		return false
	}
	if broader == "**" {
		return true
	}
	if !strings.HasSuffix(broader, "/**") {
		return false
	}

	prefix := strings.TrimSuffix(broader, "/**")
	if strings.ContainsAny(prefix, "*?[\\") {
		return false
	}

	return narrower == prefix || strings.HasPrefix(narrower, prefix+"/")
}

func mergePlacementResources(
	placementResources placement.ResourceRequirements,
	actionResources ResourceRequirements,
) ResourceRequirements {
	requests := maps.Clone(placementResources.Requests)
	if requests == nil && actionResources.Requests != nil {
		requests = make(map[string]string, len(actionResources.Requests))
	}
	maps.Copy(requests, actionResources.Requests)

	limits := maps.Clone(placementResources.Limits)
	if limits == nil && actionResources.Limits != nil {
		limits = make(map[string]string, len(actionResources.Limits))
	}
	maps.Copy(limits, actionResources.Limits)

	return ResourceRequirements{Requests: requests, Limits: limits}
}

func validateIntent(actionName, intent string) error {
	switch intent {
	case "", "reconcile", "preview", "operation":
		return nil
	default:
		return fmt.Errorf("action %q intent must be reconcile, preview, or operation", actionName)
	}
}

func validateCommand(actionName string, command []string) error {
	if len(command) == 0 || strings.TrimSpace(command[0]) == "" {
		return fmt.Errorf("action %q command is required", actionName)
	}
	for index, argument := range command {
		if strings.ContainsRune(argument, '\x00') {
			return fmt.Errorf("action %q command argument %d contains a null byte", actionName, index+1)
		}
	}

	return nil
}

func validateCredentialBindings(subject string, bindings map[string]string) error {
	for _, name := range slices.Sorted(maps.Keys(bindings)) {
		if err := kubevalidate.DNSLabel(subject+" credential name", name); err != nil {
			return err
		}
		if err := kubevalidate.DNSLabel(subject+" credential source", bindings[name]); err != nil {
			return err
		}
	}

	return nil
}

func validateTargetCredentialUsage(target Target, runtimes map[string]Runtime) error {
	used := map[string]struct{}{}
	for actionName := range target.Spec.Actions {
		effective, _ := target.EffectiveAction(actionName)
		runtime, found := runtimes[effective.Runtime]
		if !found {
			continue
		}
		for credentialName := range runtime.Spec.Credentials {
			used[credentialName] = struct{}{}
		}
	}

	for _, credentialName := range slices.Sorted(maps.Keys(target.Spec.Credentials)) {
		if _, found := used[credentialName]; !found {
			return fmt.Errorf("target binds credential %q not declared by any selected runtime", credentialName)
		}
	}

	return nil
}

func validateInputReferences(input Catalog, target Target) Diagnostics {
	diagnostics := Diagnostics{}
	for _, actionName := range slices.Sorted(maps.Keys(target.Spec.Actions)) {
		action := target.Spec.Actions[actionName]
		for _, inputName := range slices.Sorted(maps.Keys(action.Inputs)) {
			item := action.Inputs[inputName]
			fieldPath := "spec.actions." + actionName + ".inputs." + inputName + ".artifact.fromAction"
			sourceAction, found := target.Spec.Actions[item.Artifact.FromAction]
			if !found {
				diagnostics = append(diagnostics, errorDiagnostic(
					input,
					TargetKind,
					target.Metadata.Name,
					target.Source,
					fieldPath,
					"catalog.artifact.source-action-missing",
					fmt.Errorf(
						"action %q input %q references undefined action %q",
						actionName,
						inputName,
						item.Artifact.FromAction,
					),
				))
				continue
			}
			if _, found := sourceAction.Artifacts[inputName]; !found {
				diagnostic := errorDiagnostic(
					input,
					TargetKind,
					target.Metadata.Name,
					target.Source,
					fieldPath,
					"catalog.artifact.source-missing",
					fmt.Errorf(
						"action %q input %q references missing artifact %q on action %q",
						actionName,
						inputName,
						inputName,
						item.Artifact.FromAction,
					),
				)
				diagnostic.Related = []RelatedLocation{{
					Message: "The producing Action is defined here.",
					Location: input.location(
						TargetKind,
						target.Metadata.Name,
						"spec.actions."+item.Artifact.FromAction,
						target.Source,
					),
				}}
				diagnostics = append(diagnostics, diagnostic)
			}
		}
	}

	return diagnostics
}
