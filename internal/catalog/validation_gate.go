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
	add(
		"spec.prometheus.source",
		"catalog.gate.source-invalid",
		kubevalidate.DNSLabel("Gate Prometheus source", definition.Spec.Prometheus.Source),
	)
	add(
		"spec.prometheus.query",
		"catalog.gate.query-invalid",
		gate.ValidateQuery(definition.Spec.Prometheus.Query),
	)
	add(
		"spec.prometheus.condition.operator",
		"catalog.gate.operator-invalid",
		gate.ValidateOperator(definition.Spec.Prometheus.Condition.Operator),
	)
	add(
		"spec.prometheus.condition.value",
		"catalog.gate.threshold-invalid",
		gate.ValidateThreshold(definition.Spec.Prometheus.Condition.Value),
	)

	return diagnostics
}

func ValidateGateDeployment(input Catalog, sources gate.SourceSet) Diagnostics {
	diagnostics := Diagnostics{}
	for _, catalogGate := range input.Gates {
		if _, ok := sources[catalogGate.Spec.Prometheus.Source]; ok {
			continue
		}

		diagnostics = append(diagnostics, errorDiagnostic(
			input,
			GateKind,
			catalogGate.Metadata.Name,
			catalogGate.Source,
			"spec.prometheus.source",
			"catalog.gate.source-not-found",
			fmt.Errorf(
				"Gate Prometheus source %q is not configured",
				catalogGate.Spec.Prometheus.Source,
			),
		))
	}
	sortDiagnostics(diagnostics)

	return diagnostics
}
