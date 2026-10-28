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
	} else if !imageIsDigestPinned(runtime.Spec.Image) {
		item := diagnostic(
			input,
			RuntimeKind,
			runtime.Metadata.Name,
			runtime.Source,
			"spec.image",
			"catalog.runtime.image-mutable",
			SeverityWarning,
			fmt.Errorf("runtime spec.image %q is not pinned by digest", runtime.Spec.Image),
		)
		item.Hint = "Use a repository@algorithm:digest reference when reproducible image bytes are required."
		diagnostics = append(diagnostics, item)
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

func imageIsDigestPinned(image string) bool {
	at := strings.LastIndexByte(image, '@')
	if at <= 0 || at == len(image)-1 {
		return false
	}

	algorithm, encoded, found := strings.Cut(image[at+1:], ":")
	if !found || algorithm == "" || len(encoded) < 32 {
		return false
	}
	for index, character := range algorithm {
		if isASCIILetter(character) || index > 0 && character >= '0' && character <= '9' ||
			index > 0 && strings.ContainsRune("+._-", character) {
			continue
		}

		return false
	}
	for _, character := range encoded {
		if isASCIILetter(character) || character >= '0' && character <= '9' ||
			strings.ContainsRune("=_-", character) {
			continue
		}

		return false
	}
	knownHexLength := map[string]int{
		"sha256": 64,
		"sha384": 96,
		"sha512": 128,
	}[strings.ToLower(algorithm)]
	if knownHexLength != 0 && (len(encoded) != knownHexLength || !isHexDigest(encoded)) {
		return false
	}

	return true
}

func isASCIILetter(character rune) bool {
	return character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
}

func isHexDigest(encoded string) bool {
	for _, character := range encoded {
		if character < '0' || character > '9' {
			lower := character | ('a' - 'A')
			if lower < 'a' || lower > 'f' {
				return false
			}
		}
	}

	return true
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
