// Package runjob renders and manages Kubernetes Jobs for Run execution.
package runjob

import "slices"

// Config contains the Controller settings required to materialize and manage
// execution Jobs in Kubernetes.
type Config struct {
	ClusterName         string
	ServerURL           string
	CheckoutImage       string
	HarnessImage        string
	GitCredentials      map[string]string
	ExecutionNamespaces []string
}

func (c Config) ownsExecutionNamespace(namespace string) bool {
	return slices.Contains(c.ExecutionNamespaces, namespace)
}
