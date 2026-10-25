package catalog

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/slipramp/slipramp/internal/kubevalidate"
)

func validateRuntimePortable(input Catalog, runtime Runtime) Diagnostics {
	diagnostics := Diagnostics{}
	add := func(fieldPath, code string, err error) {
		if err != nil {
			diagnostics = append(diagnostics, errorDiagnostic(
				input,
				RuntimeKind,
				runtime.Metadata.Name,
				runtime.Source,
				fieldPath,
				code,
				err,
			))
		}
	}

	add(
		"metadata.name",
		"catalog.runtime.name-invalid",
		kubevalidate.DNSLabel("runtime metadata.name", runtime.Metadata.Name),
	)

	if strings.TrimSpace(runtime.Spec.Image) == "" {
		add("spec.image", "catalog.runtime.image-invalid", errors.New("runtime spec.image is required"))
	} else if strings.ContainsAny(runtime.Spec.Image, "\x00\t\r\n ") {
		add(
			"spec.image",
			"catalog.runtime.image-invalid",
			errors.New("runtime spec.image must not contain whitespace"),
		)
	}

	if runtime.Spec.WorkingDirectory != "" {
		add(
			"spec.workingDirectory",
			"catalog.runtime.working-directory-invalid",
			validateContainerPath("runtime workingDirectory", runtime.Spec.WorkingDirectory),
		)
	}

	claimedEnvironment := make(map[string]string, len(runtime.Spec.Environment))
	for _, name := range slices.Sorted(maps.Keys(runtime.Spec.Environment)) {
		fieldPath := "spec.environment." + name
		if err := validateEnvironmentName("runtime environment name", name); err != nil {
			add(fieldPath, "catalog.environment.name-invalid", err)
			continue
		}
		if strings.ContainsRune(runtime.Spec.Environment[name], '\x00') {
			add(
				fieldPath,
				"catalog.environment.value-invalid",
				fmt.Errorf("runtime environment %q contains a null byte", name),
			)
			continue
		}

		claimedEnvironment[name] = "runtime environment"
	}

	claimedMounts := map[string]mountClaim{}
	for _, name := range slices.Sorted(maps.Keys(runtime.Spec.Credentials)) {
		fieldPath := "spec.credentials." + name
		credential := runtime.Spec.Credentials[name]
		if err := kubevalidate.DNSLabel("runtime credential name", name); err != nil {
			add(fieldPath, "catalog.credential.name-invalid", err)
			continue
		}

		add(
			fieldPath+".inject",
			"catalog.credential.injection-invalid",
			validateCredentialInjection(name, credential.Inject, claimedEnvironment, claimedMounts),
		)
	}

	return diagnostics
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
		return fmt.Errorf("runtime credential %q must define at least one injection", credentialName)
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
		subject := fmt.Sprintf("runtime credential %q file %d", credentialName, index+1)
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
		if err := claimMount(directory.Path, subject, true, claimedMounts); err != nil {
			return err
		}
	}

	return nil
}
