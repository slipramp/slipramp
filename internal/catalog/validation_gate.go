package catalog

import (
	"errors"
	"fmt"
	"math"

	gatepkg "github.com/slipramp/slipramp/internal/gate"
	"github.com/slipramp/slipramp/internal/kubevalidate"
)

func validateGatePortable(input Catalog, gate Gate) Diagnostics {
	diagnostics := Diagnostics{}
	add := func(fieldPath, code string, err error) {
		if err != nil {
			diagnostics = append(diagnostics, errorDiagnostic(
				input,
				GateKind,
				gate.Metadata.Name,
				gate.Source,
				fieldPath,
				code,
				err,
			))
		}
	}

	add(
		"metadata.name",
		"catalog.gate.name-invalid",
		kubevalidate.DNSLabel("Gate metadata.name", gate.Metadata.Name),
	)
	add(
		"metadata.title",
		"catalog.gate.title-invalid",
		validatePresentationTitle("Gate metadata.title", gate.Metadata.Title),
	)
	add(
		"spec.prometheus.source",
		"catalog.gate.source-invalid",
		kubevalidate.DNSLabel("Gate Prometheus source", gate.Spec.Prometheus.Source),
	)
	add(
		"spec.prometheus.query",
		"catalog.gate.query-invalid",
		gatepkg.ValidateQuery(gate.Spec.Prometheus.Query),
	)
	add(
		"spec.prometheus.condition.operator",
		"catalog.gate.operator-invalid",
		gatepkg.ValidateOperator(gate.Spec.Prometheus.Condition.Operator),
	)
	if !isFinite(gate.Spec.Prometheus.Condition.Value) {
		add(
			"spec.prometheus.condition.value",
			"catalog.gate.threshold-invalid",
			errors.New("Gate condition value must be finite"),
		)
	}

	return diagnostics
}

func ValidateGateDeployment(input Catalog, sources gatepkg.SourceSet) Diagnostics {
	diagnostics := Diagnostics{}
	for _, catalogGate := range input.Gates {
		if _, ok := sources.Lookup(catalogGate.Spec.Prometheus.Source); ok {
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

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
