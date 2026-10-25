package catalog

import (
	"fmt"
	"io/fs"

	"github.com/slipramp/slipramp/internal/placement"
)

type ValidationLevel string

const (
	ValidationLevelPortable   ValidationLevel = "portable"
	ValidationLevelDeployment ValidationLevel = "deployment"
)

type Options struct {
	// Placements selects deployment validation. Nil explicitly selects portable
	// validation; an empty, non-nil Set rejects every Placement reference.
	Placements *placement.Set
}

type Result struct {
	Catalog     Catalog
	Diagnostics Diagnostics
	Level       ValidationLevel
}

// Process runs the shared catalog decoding and validation pipeline used by the
// Server and CLI.
func Process(files fs.FS, options Options) (Result, error) {
	loaded, diagnostics, err := Decode(files)
	if err != nil {
		return Result{}, err
	}

	diagnostics = append(diagnostics, ValidatePortable(loaded)...)
	repositoryDiagnostics, err := ValidateRepository(files, loaded)
	if err != nil {
		return Result{}, fmt.Errorf("inspect repository: %w", err)
	}
	diagnostics = append(diagnostics, repositoryDiagnostics...)
	level := ValidationLevelPortable
	if options.Placements != nil {
		level = ValidationLevelDeployment
		diagnostics = append(diagnostics, ValidateDeployment(loaded, *options.Placements)...)
	}
	sortDiagnostics(diagnostics)

	return Result{
		Catalog:     loaded,
		Diagnostics: diagnostics,
		Level:       level,
	}, nil
}

func ValidatePortable(input Catalog) Diagnostics {
	diagnostics := Diagnostics{}
	runtimes := make(map[string]Runtime, len(input.Runtimes))
	for _, runtime := range input.Runtimes {
		diagnostics = append(diagnostics, validateRuntimePortable(input, runtime)...)
		runtimes[runtime.Metadata.Name] = runtime
	}

	for _, target := range input.Targets {
		diagnostics = append(diagnostics, validateTargetPortable(input, target, runtimes)...)
	}

	sortDiagnostics(diagnostics)

	return diagnostics
}

func ValidateDeployment(input Catalog, placements placement.Set) Diagnostics {
	diagnostics := Diagnostics{}
	runtimes := make(map[string]Runtime, len(input.Runtimes))
	for _, runtime := range input.Runtimes {
		runtimes[runtime.Metadata.Name] = runtime
	}

	for _, target := range input.Targets {
		diagnostics = append(diagnostics, validateTargetDeployment(input, target, runtimes, placements)...)
	}

	sortDiagnostics(diagnostics)

	return diagnostics
}

func diagnostic(
	input Catalog,
	kind string,
	name string,
	source Source,
	fieldPath string,
	code string,
	severity Severity,
	err error,
) Diagnostic {
	return Diagnostic{
		Code:     code,
		Severity: severity,
		Message:  err.Error(),
		Location: input.location(kind, name, fieldPath, source),
	}
}

func errorDiagnostic(
	input Catalog,
	kind string,
	name string,
	source Source,
	fieldPath string,
	code string,
	err error,
) Diagnostic {
	return diagnostic(input, kind, name, source, fieldPath, code, SeverityError, err)
}
