package catalog

import "github.com/slipramp/slipramp/internal/gate"

func (g Gate) NormalizedDefinition() gate.Definition {
	return gate.Definition{
		Prometheus: gate.PrometheusDefinition{
			Source: g.Spec.Prometheus.Source,
			Query:  g.Spec.Prometheus.Query,
			Condition: gate.Condition{
				Operator: g.Spec.Prometheus.Condition.Operator,
				Value:    g.Spec.Prometheus.Condition.Value,
			},
		},
	}
}
