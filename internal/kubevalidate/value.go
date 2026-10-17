// Package kubevalidate adapts Kubernetes value validation for Slipramp.
package kubevalidate

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/validate/content"
)

func DNSLabel(subject, value string) error {
	return valueError(subject, value, content.IsDNS1123Label(value))
}

func DNSSubdomain(subject, value string) error {
	return valueError(subject, value, content.IsDNS1123Subdomain(value))
}

func LabelKey(subject, value string) error {
	return valueError(subject, value, content.IsLabelKey(value))
}

func LabelValue(subject, value string) error {
	return valueError(subject, value, content.IsLabelValue(value))
}

func valueError(subject, value string, problems []string) error {
	if len(problems) == 0 {
		return nil
	}

	return fmt.Errorf(
		"%s %q is invalid: %s",
		subject,
		value,
		strings.Join(problems, "; "),
	)
}
