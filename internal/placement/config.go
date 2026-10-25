// Package placement defines cluster-specific execution configuration.
package placement

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/slipramp/slipramp/internal/cluster"
	"github.com/slipramp/slipramp/internal/kubevalidate"
	"github.com/slipramp/slipramp/internal/resourcevalidate"
)

type Config struct {
	ClusterName        string
	JobNamespace       string
	ServiceAccountName string
	Timeout            time.Duration
	Resources          ResourceRequirements
	NodeSelector       map[string]string
	Credentials        map[string]CredentialSource
	PodTemplate        *corev1.PodTemplateSpec
}

type ResourceRequirements struct {
	Requests map[string]string
	Limits   map[string]string
}

type CredentialSource struct {
	SecretRef SecretReference
}

type SecretReference struct {
	Name string
}

// ValidateName requires a lowercase DNS-label-shaped placement name.
func ValidateName(name string) error {
	return kubevalidate.DNSLabel("placement name", name)
}

func (c Config) Validate() error {
	if err := cluster.ValidateName(c.ClusterName); err != nil {
		return err
	}

	if err := kubevalidate.DNSLabel("job namespace", c.JobNamespace); err != nil {
		return err
	}

	if c.ServiceAccountName != "" {
		if err := kubevalidate.DNSSubdomain("service account name", c.ServiceAccountName); err != nil {
			return err
		}
	}

	if c.Timeout < 0 {
		return fmt.Errorf("timeout must not be negative")
	}

	if err := validateResources(c.Resources); err != nil {
		return err
	}

	if err := validateNodeSelector(c.NodeSelector); err != nil {
		return err
	}

	if err := validatePodTemplate(c.PodTemplate); err != nil {
		return err
	}

	for _, credentialName := range slices.Sorted(maps.Keys(c.Credentials)) {
		if err := kubevalidate.DNSLabel("credential source name", credentialName); err != nil {
			return err
		}

		secretName := c.Credentials[credentialName].SecretRef.Name
		if err := kubevalidate.DNSSubdomain(
			fmt.Sprintf("credential source %q secret name", credentialName),
			secretName,
		); err != nil {
			return err
		}
	}

	return nil
}

func validateResources(resources ResourceRequirements) error {
	return resourcevalidate.Requirements("", resources.Requests, resources.Limits)
}

func validateNodeSelector(selector map[string]string) error {
	for _, key := range slices.Sorted(maps.Keys(selector)) {
		if err := kubevalidate.LabelKey("node selector key", key); err != nil {
			return err
		}

		if err := kubevalidate.LabelValue(fmt.Sprintf("node selector value for %q", key), selector[key]); err != nil {
			return err
		}
	}

	return nil
}

func validatePodTemplate(template *corev1.PodTemplateSpec) error {
	if template == nil {
		return nil
	}

	metadata := template.ObjectMeta.DeepCopy()
	metadata.Labels = nil
	metadata.Annotations = nil
	if !reflect.DeepEqual(*metadata, metav1.ObjectMeta{}) {
		return fmt.Errorf("pod template metadata may only define labels and annotations")
	}

	for _, key := range slices.Sorted(maps.Keys(template.Labels)) {
		if err := kubevalidate.LabelKey("pod template label key", key); err != nil {
			return err
		}

		if err := kubevalidate.LabelValue(
			fmt.Sprintf("pod template label value for %q", key),
			template.Labels[key],
		); err != nil {
			return err
		}
	}

	for _, key := range slices.Sorted(maps.Keys(template.Annotations)) {
		if err := kubevalidate.LabelKey("pod template annotation key", key); err != nil {
			return err
		}
	}

	if len(template.Spec.Containers) != 0 {
		return fmt.Errorf("pod template must not define containers")
	}

	if len(template.Spec.InitContainers) != 0 {
		return fmt.Errorf("pod template must not define init containers")
	}

	if len(template.Spec.EphemeralContainers) != 0 {
		return fmt.Errorf("pod template must not define ephemeral containers")
	}

	if template.Spec.RestartPolicy != "" {
		return fmt.Errorf("pod template must not define restart policy")
	}

	if template.Spec.ServiceAccountName != "" ||
		template.Spec.DeprecatedServiceAccount != "" {
		return fmt.Errorf("pod template must not define a service account; use serviceAccountName")
	}

	if template.Spec.NodeSelector != nil {
		return fmt.Errorf("pod template must not define a node selector; use nodeSelector")
	}

	if template.Spec.ActiveDeadlineSeconds != nil {
		return fmt.Errorf("pod template must not define an active deadline; use timeout")
	}

	return nil
}
