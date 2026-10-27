package config

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/slipramp/slipramp/internal/controller"
	"github.com/slipramp/slipramp/internal/promotion"
)

const (
	controllerClusterEnvironment             = "SLIPRAMP_CONTROLLER_CLUSTER"
	controllerServerEnvironment              = "SLIPRAMP_CONTROLLER_SERVER_URL"
	controllerMetricsAddressEnvironment      = "SLIPRAMP_CONTROLLER_METRICS_ADDRESS"
	controllerEnrollmentTokenEnvironment     = "SLIPRAMP_CONTROLLER_ENROLLMENT_TOKEN"
	controllerTokenEnvironment               = "SLIPRAMP_CONTROLLER_TOKEN"
	controllerCheckoutImageEnvironment       = "SLIPRAMP_CONTROLLER_CHECKOUT_IMAGE"
	controllerHarnessImageEnvironment        = "SLIPRAMP_CONTROLLER_HARNESS_IMAGE"
	controllerGitCredentialsEnvironment      = "SLIPRAMP_CONTROLLER_GIT_CREDENTIALS"
	controllerExecutionNamespacesEnvironment = "SLIPRAMP_CONTROLLER_EXECUTION_NAMESPACES"
	controllerKargoIntegrationsEnvironment   = "SLIPRAMP_CONTROLLER_KARGO_INTEGRATIONS"
)

func LoadController(lookupEnvironment func(string) (string, bool)) (controller.Config, error) {
	if lookupEnvironment == nil {
		return controller.Config{}, errors.New("environment lookup is required")
	}

	cfg := controller.Config{}
	cfg.ClusterName, _ = lookupEnvironment(controllerClusterEnvironment)
	cfg.ServerURL, _ = lookupEnvironment(controllerServerEnvironment)
	cfg.MetricsAddress = controller.DefaultMetricsAddress
	if value, ok := lookupEnvironment(controllerMetricsAddressEnvironment); ok {
		cfg.MetricsAddress = value
	}
	cfg.EnrollmentToken, _ = lookupEnvironment(controllerEnrollmentTokenEnvironment)
	cfg.BearerToken, _ = lookupEnvironment(controllerTokenEnvironment)
	cfg.CheckoutImage, _ = lookupEnvironment(controllerCheckoutImageEnvironment)
	cfg.HarnessImage, _ = lookupEnvironment(controllerHarnessImageEnvironment)
	cfg.GitCredentials = map[string]string{}
	cfg.ExecutionNamespaces = []string{}
	cfg.KargoIntegrations = []promotion.ObserverConfig{}

	if value, ok := lookupEnvironment(controllerExecutionNamespacesEnvironment); ok {
		if err := json.Unmarshal([]byte(value), &cfg.ExecutionNamespaces); err != nil {
			return controller.Config{}, fmt.Errorf("parse %s: %w", controllerExecutionNamespacesEnvironment, err)
		}
		if cfg.ExecutionNamespaces == nil {
			cfg.ExecutionNamespaces = []string{}
		}
	}

	if value, ok := lookupEnvironment(controllerGitCredentialsEnvironment); ok {
		if err := json.Unmarshal([]byte(value), &cfg.GitCredentials); err != nil {
			return controller.Config{}, fmt.Errorf("parse %s: %w", controllerGitCredentialsEnvironment, err)
		}
		if cfg.GitCredentials == nil {
			cfg.GitCredentials = map[string]string{}
		}
	}

	if value, ok := lookupEnvironment(controllerKargoIntegrationsEnvironment); ok {
		if err := json.Unmarshal([]byte(value), &cfg.KargoIntegrations); err != nil {
			return controller.Config{}, fmt.Errorf("parse %s: %w", controllerKargoIntegrationsEnvironment, err)
		}
		if cfg.KargoIntegrations == nil {
			cfg.KargoIntegrations = []promotion.ObserverConfig{}
		}
	}

	if err := cfg.Validate(); err != nil {
		return controller.Config{}, fmt.Errorf("validate controller configuration: %w", err)
	}

	return cfg, nil
}
