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

func validateTarget(
	target Target,
	runtimes map[string]Runtime,
	placements placement.Set,
) error {
	if err := kubevalidate.DNSLabel(
		"target metadata.name",
		target.Metadata.Name,
	); err != nil {
		return validationError(target.Source, err)
	}

	if target.Spec.Path != "" {
		if err := validateRepositoryPath("target path", target.Spec.Path); err != nil {
			return validationError(target.Source, err)
		}
	}

	seenWatchPatterns := map[string]struct{}{}
	for index, pattern := range target.Spec.Watch {
		subject := fmt.Sprintf("target watch pattern %d", index+1)
		if err := validateRepositoryPattern(subject, pattern); err != nil {
			return validationError(target.Source, err)
		}
		if _, found := seenWatchPatterns[pattern]; found {
			return validationError(
				target.Source,
				fmt.Errorf("target watch pattern %q is duplicated", pattern),
			)
		}
		seenWatchPatterns[pattern] = struct{}{}
	}

	if target.Spec.Runtime != "" {
		if err := requireRuntime(runtimes, target.Spec.Runtime); err != nil {
			return validationError(target.Source, err)
		}
	}

	if target.Spec.Placement != "" {
		if err := kubevalidate.DNSLabel(
			"target placement",
			target.Spec.Placement,
		); err != nil {
			return validationError(target.Source, err)
		}
		if _, found := placements.Lookup(target.Spec.Placement); !found {
			return validationError(
				target.Source,
				fmt.Errorf(
					"placement %q is not defined",
					target.Spec.Placement,
				),
			)
		}
	}

	if err := validateCredentialBindings(
		"target",
		target.Spec.Credentials,
	); err != nil {
		return validationError(target.Source, err)
	}
	if err := validateTimeout("target timeout", target.Spec.Timeout); err != nil {
		return validationError(target.Source, err)
	}
	if err := validateResourceValues(
		"target",
		target.Spec.Resources,
	); err != nil {
		return validationError(target.Source, err)
	}

	if len(target.Spec.Actions) == 0 {
		return validationError(
			target.Source,
			errors.New("target must define at least one action"),
		)
	}

	for _, name := range slices.Sorted(maps.Keys(target.Spec.Actions)) {
		effective, _ := target.EffectiveAction(name)
		if err := validateAction(
			target.Source,
			target.Spec.Actions[name],
			effective,
			runtimes,
			placements,
		); err != nil {
			return err
		}
	}
	if err := validateTargetCredentialUsage(target, runtimes); err != nil {
		return validationError(target.Source, err)
	}

	return validateInputReferences(target)
}

func validateAction(
	source Source,
	action Action,
	effective EffectiveAction,
	runtimes map[string]Runtime,
	placements placement.Set,
) error {
	name := effective.Name
	if err := kubevalidate.DNSLabel("action name", name); err != nil {
		return validationError(source, err)
	}

	if len(action.Triggers) > 0 {
		return validationError(
			source,
			fmt.Errorf(
				"action %q declares triggers, which are not supported",
				name,
			),
		)
	}

	if err := validateIntent(name, action.Intent); err != nil {
		return validationError(source, err)
	}
	if err := validateCommand(name, action.Command); err != nil {
		return validationError(source, err)
	}

	if effective.Runtime == "" {
		return validationError(
			source,
			fmt.Errorf("action %q must select a runtime", name),
		)
	}
	runtime, found := runtimes[effective.Runtime]
	if !found {
		return validationError(
			source,
			fmt.Errorf("runtime %q is not defined", effective.Runtime),
		)
	}

	if effective.Placement == "" {
		return validationError(
			source,
			fmt.Errorf("action %q must select a placement", name),
		)
	}
	if err := kubevalidate.DNSLabel(
		fmt.Sprintf("action %q placement", name),
		effective.Placement,
	); err != nil {
		return validationError(source, err)
	}
	placementConfig, found := placements.Lookup(effective.Placement)
	if !found {
		return validationError(
			source,
			fmt.Errorf("placement %q is not defined", effective.Placement),
		)
	}

	if err := validateCredentialBindings(
		fmt.Sprintf("action %q", name),
		action.Credentials,
	); err != nil {
		return validationError(source, err)
	}
	for _, credentialName := range slices.Sorted(maps.Keys(action.Credentials)) {
		if _, found := runtime.Spec.Credentials[credentialName]; !found {
			return validationError(
				source,
				fmt.Errorf(
					"action %q binds credential %q not declared by runtime %q",
					name,
					credentialName,
					effective.Runtime,
				),
			)
		}
	}
	for _, credentialName := range slices.Sorted(maps.Keys(runtime.Spec.Credentials)) {
		sourceName := effective.Credentials[credentialName]
		if sourceName == "" {
			return validationError(
				source,
				fmt.Errorf(
					"action %q must bind runtime credential %q",
					name,
					credentialName,
				),
			)
		}
		if _, found := placementConfig.Credentials[sourceName]; !found {
			return validationError(
				source,
				fmt.Errorf(
					"action %q credential %q references source %q not offered by placement %q",
					name,
					credentialName,
					sourceName,
					effective.Placement,
				),
			)
		}
	}

	if err := validateTimeout(
		fmt.Sprintf("action %q timeout", name),
		action.Timeout,
	); err != nil {
		return validationError(source, err)
	}
	if err := validateResourceValues(
		fmt.Sprintf("action %q", name),
		action.Resources,
	); err != nil {
		return validationError(source, err)
	}
	if err := validateResources(
		fmt.Sprintf("action %q effective", name),
		effective.Resources,
	); err != nil {
		return validationError(source, err)
	}

	if err := validateParameters(name, action.Parameters, runtime); err != nil {
		return validationError(source, err)
	}
	if err := validateArtifacts(name, action.Artifacts); err != nil {
		return validationError(source, err)
	}
	if err := validateInputs(name, action.Inputs); err != nil {
		return validationError(source, err)
	}
	for _, inputName := range slices.Sorted(maps.Keys(action.Inputs)) {
		input := action.Inputs[inputName]
		for _, artifactName := range slices.Sorted(maps.Keys(action.Artifacts)) {
			artifact := action.Artifacts[artifactName]
			if input.MountPath == artifact.Path {
				return validationError(
					source,
					fmt.Errorf(
						"action %q input %q mountPath conflicts with artifact %q path",
						name,
						inputName,
						artifactName,
					),
				)
			}
		}
	}

	return nil
}

func validateIntent(actionName, intent string) error {
	switch intent {
	case "", "reconcile", "preview", "operation":
		return nil
	default:
		return fmt.Errorf(
			"action %q intent must be reconcile, preview, or operation",
			actionName,
		)
	}
}

func validateCommand(actionName string, command []string) error {
	if len(command) == 0 || strings.TrimSpace(command[0]) == "" {
		return fmt.Errorf("action %q command is required", actionName)
	}
	for index, argument := range command {
		if strings.ContainsRune(argument, '\x00') {
			return fmt.Errorf(
				"action %q command argument %d contains a null byte",
				actionName,
				index+1,
			)
		}
	}

	return nil
}

func validateCredentialBindings(subject string, bindings map[string]string) error {
	for _, name := range slices.Sorted(maps.Keys(bindings)) {
		if err := kubevalidate.DNSLabel(subject+" credential name", name); err != nil {
			return err
		}
		if err := kubevalidate.DNSLabel(
			subject+" credential source",
			bindings[name],
		); err != nil {
			return err
		}
	}

	return nil
}

func validateTargetCredentialUsage(
	target Target,
	runtimes map[string]Runtime,
) error {
	used := map[string]struct{}{}
	for actionName := range target.Spec.Actions {
		effective, _ := target.EffectiveAction(actionName)
		runtime := runtimes[effective.Runtime]
		for credentialName := range runtime.Spec.Credentials {
			used[credentialName] = struct{}{}
		}
	}

	for _, credentialName := range slices.Sorted(maps.Keys(target.Spec.Credentials)) {
		if _, found := used[credentialName]; !found {
			return fmt.Errorf(
				"target binds credential %q not declared by any selected runtime",
				credentialName,
			)
		}
	}

	return nil
}

func validateInputReferences(target Target) error {
	for _, actionName := range slices.Sorted(maps.Keys(target.Spec.Actions)) {
		action := target.Spec.Actions[actionName]
		for _, inputName := range slices.Sorted(maps.Keys(action.Inputs)) {
			input := action.Inputs[inputName]
			sourceAction, found := target.Spec.Actions[input.Artifact.FromAction]
			if !found {
				return validationError(
					target.Source,
					fmt.Errorf(
						"action %q input %q references undefined action %q",
						actionName,
						inputName,
						input.Artifact.FromAction,
					),
				)
			}
			if _, found := sourceAction.Artifacts[inputName]; !found {
				return validationError(
					target.Source,
					fmt.Errorf(
						"action %q input %q references missing artifact %q on action %q",
						actionName,
						inputName,
						inputName,
						input.Artifact.FromAction,
					),
				)
			}
		}
	}

	return nil
}
