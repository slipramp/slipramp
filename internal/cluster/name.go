package cluster

import (
	"fmt"

	"github.com/slipramp/slipramp/internal/kubevalidate"
)

// ValidateName requires a lowercase DNS-label-shaped cluster name.
func ValidateName(name string) error {
	if err := kubevalidate.DNSLabel("cluster name", name); err != nil {
		return fmt.Errorf("validate cluster name: %w", err)
	}

	return nil
}
