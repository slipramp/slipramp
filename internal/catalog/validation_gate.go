package catalog

import (
	"fmt"

	"github.com/slipramp/slipramp/internal/gate"
	"github.com/slipramp/slipramp/internal/kubevalidate"
)

func validateGatePortable(input Catalog, definition Gate) Diagnostics {
	diagnostics := Diagnostics{}
	add := func(fieldPath, code string, err error) {
		if err != nil {
			diagnostics = append(diagnostics, errorDiagnostic(
				input,
				GateKind,
				definition.Metadata.Name,
				definition.Source,
				fieldPath,
				code,
				err,
			))
		}
	}

	add(
		"metadata.name",
		"catalog.gate.name-invalid",
		kubevalidate.DNSLabel("Gate metadata.name", definition.Metadata.Name),
	)
	add(
		"metadata.title",
		"catalog.gate.title-invalid",
		validatePresentationTitle("Gate metadata.title", definition.Metadata.Title),
	)
	for _, issue := range definition.Spec.Issues() {
		add("spec.evaluator."+issue.Field, issue.Code, issue.Err)
	}

	return diagnostics
}

func ValidateGateDeployment(input Catalog, sources gate.SourceSet) Diagnostics {
	diagnostics := Diagnostics{}
	for _, catalogGate := range input.Gates {
		policy := catalogGate.Spec.Evaluator
		if policy == nil {
			continue
		}
		source := sources[policy.SourceName()]
		var code string
		var err error
		switch {
		case source == nil:
			code = "catalog.gate.source-not-found"
			err = fmt.Errorf("referenced Gate source %q is not configured", policy.SourceName())
		case source.Type() != policy.Type():
			code = "catalog.gate.source-type-mismatch"
			err = fmt.Errorf("configured Gate source %q has type %q, expected %q", policy.SourceName(), source.Type(), policy.Type())
		default:
			continue
		}
		diagnostics = append(diagnostics, errorDiagnostic(
			input, GateKind, catalogGate.Metadata.Name, catalogGate.Source, "spec.evaluator.source", code, err,
		))
	}
	sortDiagnostics(diagnostics)

	return diagnostics
}
