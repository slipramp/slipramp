// Package preflight renders and manages Kubernetes Jobs used to validate
// Placement execution policy.
package preflight

import "slices"

// Config contains the Controller settings required to materialize Placement
// preflight Jobs.
type Config struct {
	ClusterName         string
	CheckoutImage       string
	ExecutionNamespaces []string
}

func (c Config) ownsExecutionNamespace(namespace string) bool {
	return slices.Contains(c.ExecutionNamespaces, namespace)
}
