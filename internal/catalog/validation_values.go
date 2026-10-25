package catalog

import (
	"fmt"
	"maps"
	"mime"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/slipramp/slipramp/internal/durationvalidate"
	"github.com/slipramp/slipramp/internal/kubevalidate"
	"github.com/slipramp/slipramp/internal/resourcevalidate"
)

var (
	environmentNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	secretKeyPattern       = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

type mountClaim struct {
	subject   string
	directory bool
}

func validateParameters(actionName string, parameters map[string]Parameter, runtime Runtime) []validationFinding {
	findings := []validationFinding{}
	environmentOwners := make(map[string]string, len(runtime.Spec.Environment))
	for name := range runtime.Spec.Environment {
		environmentOwners[name] = "runtime environment"
	}
	for credentialName, credential := range runtime.Spec.Credentials {
		for name := range credential.Inject.Environment {
			environmentOwners[name] = fmt.Sprintf("runtime credential %q", credentialName)
		}
	}

	for _, name := range slices.Sorted(maps.Keys(parameters)) {
		parameter := parameters[name]
		subject := fmt.Sprintf("action %q parameter %q", actionName, name)
		if err := kubevalidate.DNSLabel("parameter name", name); err != nil {
			findings = append(findings, validationFinding{
				fieldPath: ".parameters." + name,
				code:      "catalog.parameter.name-invalid",
				err:       err,
			})
		}

		switch parameter.Type {
		case ParameterTypeString, ParameterTypeBoolean, ParameterTypeNumber:
			if len(parameter.Values) != 0 {
				findings = append(findings, validationFinding{
					fieldPath: ".parameters." + name + ".values",
					code:      "catalog.parameter.values-invalid",
					err:       fmt.Errorf("%s values are only valid for enum parameters", subject),
				})
			}
		case ParameterTypeEnum:
			if len(parameter.Values) == 0 {
				findings = append(findings, validationFinding{
					fieldPath: ".parameters." + name + ".values",
					code:      "catalog.parameter.values-required",
					err:       fmt.Errorf("%s must define values", subject),
				})
			}

			seen := map[string]struct{}{}
			for index, value := range parameter.Values {
				if value == "" {
					findings = append(findings, validationFinding{
						fieldPath: fmt.Sprintf(".parameters.%s.values.%d", name, index),
						code:      "catalog.parameter.value-invalid",
						err:       fmt.Errorf("%s values must not be empty", subject),
					})
				}
				if _, found := seen[value]; found {
					findings = append(findings, validationFinding{
						fieldPath: fmt.Sprintf(".parameters.%s.values.%d", name, index),
						code:      "catalog.parameter.value-duplicate",
						err:       fmt.Errorf("%s value %q is duplicated", subject, value),
					})
				}
				seen[value] = struct{}{}
			}
		default:
			findings = append(findings, validationFinding{
				fieldPath: ".parameters." + name + ".type",
				code:      "catalog.parameter.type-invalid",
				err:       fmt.Errorf("%s type must be string, boolean, number, or enum", subject),
			})
		}

		hasEnvironment := parameter.Environment != ""
		hasArgument := parameter.Argument != nil
		if hasEnvironment == hasArgument {
			findings = append(findings, validationFinding{
				fieldPath: ".parameters." + name,
				code:      "catalog.parameter.delivery-invalid",
				err:       fmt.Errorf("%s must define exactly one of environment or argument", subject),
			})
		}

		if hasEnvironment {
			if err := validateEnvironmentName(subject+" environment", parameter.Environment); err != nil {
				findings = append(findings, validationFinding{
					fieldPath: ".parameters." + name + ".environment",
					code:      "catalog.parameter.environment-invalid",
					err:       err,
				})
			} else if owner, found := environmentOwners[parameter.Environment]; found {
				findings = append(findings, validationFinding{
					fieldPath: ".parameters." + name + ".environment",
					code:      "catalog.parameter.environment-conflict",
					err: fmt.Errorf(
						"%s environment %q conflicts with %s",
						subject,
						parameter.Environment,
						owner,
					),
				})
			} else {
				environmentOwners[parameter.Environment] = subject
			}
		}
		if hasArgument {
			argumentName := parameter.Argument.Name
			if argumentName != strings.TrimSpace(argumentName) ||
				strings.ContainsAny(argumentName, "\x00\t\r\n ") {
				findings = append(findings, validationFinding{
					fieldPath: ".parameters." + name + ".argument.name",
					code:      "catalog.parameter.argument-invalid",
					err:       fmt.Errorf("%s argument name must be one argument", subject),
				})
			}
		}
	}

	return findings
}

func validateArtifacts(actionName string, artifacts map[string]Artifact) []validationFinding {
	findings := []validationFinding{}
	paths := map[string]string{}
	for _, name := range slices.Sorted(maps.Keys(artifacts)) {
		artifact := artifacts[name]
		subject := fmt.Sprintf("action %q artifact %q", actionName, name)
		if err := kubevalidate.DNSLabel("artifact name", name); err != nil {
			findings = append(findings, validationFinding{
				fieldPath: ".artifacts." + name,
				code:      "catalog.artifact.name-invalid",
				err:       err,
			})
		}
		if err := validateContainerPath(subject+" path", artifact.Path); err != nil {
			findings = append(findings, validationFinding{
				fieldPath: ".artifacts." + name + ".path",
				code:      "catalog.artifact.path-invalid",
				err:       err,
			})
		} else if previous, found := paths[artifact.Path]; found {
			findings = append(findings, validationFinding{
				fieldPath: ".artifacts." + name + ".path",
				code:      "catalog.artifact.path-conflict",
				err:       fmt.Errorf("%s path conflicts with artifact %q", subject, previous),
			})
		} else {
			paths[artifact.Path] = name
		}

		if artifact.MediaType != "" {
			if _, _, err := mime.ParseMediaType(artifact.MediaType); err != nil {
				findings = append(findings, validationFinding{
					fieldPath: ".artifacts." + name + ".mediaType",
					code:      "catalog.artifact.media-type-invalid",
					err:       fmt.Errorf("%s mediaType: %w", subject, err),
				})
			}
		}
	}

	return findings
}

func validateInputs(actionName string, inputs map[string]Input) []validationFinding {
	findings := []validationFinding{}
	mountPaths := map[string]string{}
	for _, name := range slices.Sorted(maps.Keys(inputs)) {
		input := inputs[name]
		subject := fmt.Sprintf("action %q input %q", actionName, name)
		if err := kubevalidate.DNSLabel("input name", name); err != nil {
			findings = append(findings, validationFinding{
				fieldPath: ".inputs." + name,
				code:      "catalog.input.name-invalid",
				err:       err,
			})
		}
		if err := kubevalidate.DNSLabel(subject+" source action", input.Artifact.FromAction); err != nil {
			findings = append(findings, validationFinding{
				fieldPath: ".inputs." + name + ".artifact.fromAction",
				code:      "catalog.input.source-action-invalid",
				err:       err,
			})
		}
		switch input.Artifact.Compatibility {
		case "", ArtifactCompatibilityExactCatalogRevision:
		default:
			findings = append(findings, validationFinding{
				fieldPath: ".inputs." + name + ".artifact.compatibility",
				code:      "catalog.input.compatibility-invalid",
				err: fmt.Errorf(
					"%s compatibility must be %q",
					subject,
					ArtifactCompatibilityExactCatalogRevision,
				),
			})
		}
		if err := validateContainerPath(subject+" mountPath", input.MountPath); err != nil {
			findings = append(findings, validationFinding{
				fieldPath: ".inputs." + name + ".mountPath",
				code:      "catalog.input.mount-path-invalid",
				err:       err,
			})
		} else if previous, found := mountPaths[input.MountPath]; found {
			findings = append(findings, validationFinding{
				fieldPath: ".inputs." + name + ".mountPath",
				code:      "catalog.input.mount-path-conflict",
				err:       fmt.Errorf("%s mountPath conflicts with input %q", subject, previous),
			})
		} else {
			mountPaths[input.MountPath] = name
		}
	}

	return findings
}

func validateTimeout(subject, value string) error {
	if value == "" {
		return nil
	}

	_, err := durationvalidate.Positive(subject, value)

	return err
}

func validateResourceValues(subject string, resources ResourceRequirements) error {
	if _, err := resourcevalidate.Values(subject+" resource request", resources.Requests); err != nil {
		return err
	}
	if _, err := resourcevalidate.Values(subject+" resource limit", resources.Limits); err != nil {
		return err
	}

	return nil
}

func validateResources(subject string, resources ResourceRequirements) error {
	return resourcevalidate.Requirements(subject, resources.Requests, resources.Limits)
}

func validateEnvironmentName(subject, value string) error {
	if !environmentNamePattern.MatchString(value) {
		return fmt.Errorf(
			"%s %q must start with a letter or underscore and contain only letters, digits, and underscores",
			subject,
			value,
		)
	}
	if strings.HasPrefix(value, "SLIPRAMP_") {
		return fmt.Errorf("%s %q uses the reserved SLIPRAMP_ namespace", subject, value)
	}

	return nil
}

func validateSecretKey(subject, value string) error {
	if !secretKeyPattern.MatchString(value) {
		return fmt.Errorf("%s %q must contain only letters, digits, dots, underscores, and hyphens", subject, value)
	}

	return nil
}

func validateFileMode(subject string, mode uint32) error {
	if mode > 0o777 {
		return fmt.Errorf("%s %#o must not set bits outside 0777", subject, mode)
	}

	return nil
}

func validateContainerPath(subject, value string) error {
	if value == "" || value == "/" || !path.IsAbs(value) || path.Clean(value) != value {
		return fmt.Errorf("%s %q must be a clean absolute path below root", subject, value)
	}
	if strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("%s must not contain a null byte", subject)
	}

	return nil
}

func claimMount(mountPath string, subject string, directory bool, claimed map[string]mountClaim) error {
	for existingPath, existing := range claimed {
		if mountPath == existingPath ||
			directory && pathWithin(mountPath, existingPath) ||
			existing.directory &&
				pathWithin(existingPath, mountPath) {
			return fmt.Errorf("%s path %q conflicts with %s", subject, mountPath, existing.subject)
		}
	}

	claimed[mountPath] = mountClaim{subject: subject, directory: directory}

	return nil
}

func pathWithin(directory, candidate string) bool {
	return strings.HasPrefix(candidate, strings.TrimSuffix(directory, "/")+"/")
}

func validateRepositoryPath(subject, value string) error {
	if value == "" ||
		value == "." ||
		value == ".." ||
		strings.HasPrefix(value, "../") ||
		path.IsAbs(value) ||
		path.Clean(value) != value {
		return fmt.Errorf("%s %q must be a clean repository-relative path", subject, value)
	}
	if strings.ContainsAny(value, "\\\x00") {
		return fmt.Errorf("%s %q must use slash-separated repository paths", subject, value)
	}

	return nil
}

func validateRepositoryPattern(subject, value string) error {
	if err := validateRepositoryPath(subject, value); err != nil {
		return err
	}
	if _, err := path.Match(value, "placeholder"); err != nil {
		return fmt.Errorf("%s %q is invalid: %w", subject, value, err)
	}

	return nil
}
