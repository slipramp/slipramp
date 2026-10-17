package catalog

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/slipramp/slipramp/internal/kubevalidate"
)

func validateRuntime(runtime Runtime) error {
	if err := kubevalidate.DNSLabel(
		"runtime metadata.name",
		runtime.Metadata.Name,
	); err != nil {
		return validationError(runtime.Source, err)
	}

	if strings.TrimSpace(runtime.Spec.Image) == "" {
		return validationError(
			runtime.Source,
			errors.New("runtime spec.image is required"),
		)
	}
	if strings.ContainsAny(runtime.Spec.Image, "\x00\t\r\n ") {
		return validationError(
			runtime.Source,
			errors.New("runtime spec.image must not contain whitespace"),
		)
	}

	if runtime.Spec.WorkingDirectory != "" {
		if err := validateContainerPath(
			"runtime workingDirectory",
			runtime.Spec.WorkingDirectory,
		); err != nil {
			return validationError(runtime.Source, err)
		}
	}

	claimedEnvironment := make(map[string]string, len(runtime.Spec.Environment))
	for _, name := range slices.Sorted(maps.Keys(runtime.Spec.Environment)) {
		if err := validateEnvironmentName("runtime environment name", name); err != nil {
			return validationError(runtime.Source, err)
		}
		if strings.ContainsRune(runtime.Spec.Environment[name], '\x00') {
			return validationError(
				runtime.Source,
				fmt.Errorf("runtime environment %q contains a null byte", name),
			)
		}

		claimedEnvironment[name] = "runtime environment"
	}

	claimedMounts := map[string]mountClaim{}
	for _, name := range slices.Sorted(maps.Keys(runtime.Spec.Credentials)) {
		credential := runtime.Spec.Credentials[name]
		if err := kubevalidate.DNSLabel("runtime credential name", name); err != nil {
			return validationError(runtime.Source, err)
		}

		if err := validateCredentialInjection(
			name,
			credential.Inject,
			claimedEnvironment,
			claimedMounts,
		); err != nil {
			return validationError(runtime.Source, err)
		}
	}

	return nil
}

func validateCredentialInjection(
	credentialName string,
	injection CredentialInjection,
	claimedEnvironment map[string]string,
	claimedMounts map[string]mountClaim,
) error {
	if len(injection.Environment) == 0 &&
		!injection.EnvironmentFrom &&
		len(injection.Files) == 0 &&
		injection.Directory == nil {
		return fmt.Errorf(
			"runtime credential %q must define at least one injection",
			credentialName,
		)
	}

	for _, name := range slices.Sorted(maps.Keys(injection.Environment)) {
		variable := injection.Environment[name]
		if err := validateEnvironmentName(
			fmt.Sprintf("runtime credential %q environment name", credentialName),
			name,
		); err != nil {
			return err
		}
		if err := validateSecretKey(
			fmt.Sprintf("runtime credential %q environment %q key", credentialName, name),
			variable.Key,
		); err != nil {
			return err
		}

		if previous, found := claimedEnvironment[name]; found {
			return fmt.Errorf(
				"runtime credential %q environment name %q conflicts with %s",
				credentialName,
				name,
				previous,
			)
		}
		claimedEnvironment[name] = fmt.Sprintf("runtime credential %q", credentialName)
	}

	for index, file := range injection.Files {
		subject := fmt.Sprintf(
			"runtime credential %q file %d",
			credentialName,
			index+1,
		)
		if err := validateSecretKey(subject+" key", file.Key); err != nil {
			return err
		}
		if err := validateContainerPath(subject+" path", file.Path); err != nil {
			return err
		}
		if err := validateFileMode(subject+" mode", file.Mode); err != nil {
			return err
		}
		if err := claimMount(file.Path, subject, false, claimedMounts); err != nil {
			return err
		}
	}

	if injection.Directory != nil {
		directory := *injection.Directory
		subject := fmt.Sprintf("runtime credential %q directory", credentialName)
		if err := validateContainerPath(subject+" path", directory.Path); err != nil {
			return err
		}
		if err := validateFileMode(subject+" mode", directory.Mode); err != nil {
			return err
		}
		if err := claimMount(
			directory.Path,
			subject,
			true,
			claimedMounts,
		); err != nil {
			return err
		}
	}

	return nil
}
