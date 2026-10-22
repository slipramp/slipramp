package promotion

import (
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/slipramp/slipramp/internal/kubevalidate"
)

const (
	DefaultMaximumStageObservationAge = 5 * time.Minute
	DefaultPendingReviewRetention     = 8 * 24 * time.Hour
)

type PreparationConfig struct {
	Repository string
	Target     string
	Action     string
}

type IntegrationConfig struct {
	ClusterName                string
	Repositories               []string
	Preparation                PreparationConfig
	MaximumStageObservationAge time.Duration
	PendingReviewRetention     time.Duration
}

type IntegrationSet map[string]IntegrationConfig

type ObserverConfig struct {
	Name            string   `json:"name"`
	StageNamespaces []string `json:"stageNamespaces"`
}

func (s IntegrationSet) Names() []string {
	return slices.Sorted(maps.Keys(s))
}

func (s IntegrationSet) Lookup(name string) (IntegrationConfig, bool) {
	integration, ok := s[name]

	return integration, ok
}

func (s IntegrationSet) Validate() error {
	for _, name := range s.Names() {
		if err := kubevalidate.DNSLabel("Kargo integration name", name); err != nil {
			return err
		}

		if err := s[name].Validate(); err != nil {
			return fmt.Errorf("validate Kargo integration %q: %w", name, err)
		}
	}

	return nil
}

func (c IntegrationConfig) Validate() error {
	if err := kubevalidate.DNSLabel("Kargo integration cluster", c.ClusterName); err != nil {
		return err
	}
	if len(c.Repositories) == 0 {
		return fmt.Errorf("at least one Kargo repository is required")
	}

	seenRepositories := make(map[string]struct{}, len(c.Repositories))
	for _, name := range c.Repositories {
		if err := kubevalidate.DNSLabel("Kargo repository name", name); err != nil {
			return err
		}
		if _, ok := seenRepositories[name]; ok {
			return fmt.Errorf("kargo repository %q is configured more than once", name)
		}
		seenRepositories[name] = struct{}{}
	}

	if err := kubevalidate.DNSLabel("Kargo preparation repository", c.Preparation.Repository); err != nil {
		return err
	}
	if _, ok := seenRepositories[c.Preparation.Repository]; !ok {
		return fmt.Errorf(
			"kargo preparation repository %q is not in the repository allowlist",
			c.Preparation.Repository,
		)
	}
	if err := kubevalidate.DNSLabel("Kargo preparation target", c.Preparation.Target); err != nil {
		return err
	}
	if err := kubevalidate.DNSLabel("Kargo preparation action", c.Preparation.Action); err != nil {
		return err
	}
	if c.MaximumStageObservationAge <= 0 {
		return fmt.Errorf("maximum Kargo Stage observation age must be positive")
	}
	if c.PendingReviewRetention <= 0 {
		return fmt.Errorf("pending Kargo review retention must be positive")
	}

	return nil
}

func ValidateObserverConfigs(configs []ObserverConfig) error {
	seenIntegrations := make(map[string]struct{}, len(configs))
	for _, observer := range configs {
		if err := observer.Validate(); err != nil {
			return err
		}
		if _, ok := seenIntegrations[observer.Name]; ok {
			return fmt.Errorf("kargo integration %q is configured more than once", observer.Name)
		}
		seenIntegrations[observer.Name] = struct{}{}
	}

	return nil
}

func (c ObserverConfig) Validate() error {
	if err := kubevalidate.DNSLabel("Kargo integration name", c.Name); err != nil {
		return err
	}
	if len(c.StageNamespaces) == 0 {
		return fmt.Errorf("kargo integration %q requires at least one Stage namespace", c.Name)
	}

	seenNamespaces := make(map[string]struct{}, len(c.StageNamespaces))
	for _, namespace := range c.StageNamespaces {
		if err := kubevalidate.DNSLabel("Kargo Stage namespace", namespace); err != nil {
			return err
		}
		if _, ok := seenNamespaces[namespace]; ok {
			return fmt.Errorf(
				"kargo Stage namespace %q is configured more than once for integration %q",
				namespace,
				c.Name,
			)
		}
		seenNamespaces[namespace] = struct{}{}
	}

	return nil
}
