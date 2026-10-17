package config

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/slipramp/slipramp/internal/controller"
)

const (
	controllerClusterEnvironment         = "SLIPRAMP_CONTROLLER_CLUSTER"
	controllerServerEnvironment          = "SLIPRAMP_CONTROLLER_SERVER_URL"
	controllerEnrollmentTokenEnvironment = "SLIPRAMP_CONTROLLER_ENROLLMENT_TOKEN"
	controllerTokenEnvironment           = "SLIPRAMP_CONTROLLER_TOKEN"
	controllerCheckoutImageEnvironment   = "SLIPRAMP_CONTROLLER_CHECKOUT_IMAGE"
	controllerHarnessImageEnvironment    = "SLIPRAMP_CONTROLLER_HARNESS_IMAGE"
	controllerGitCredentialsEnvironment  = "SLIPRAMP_CONTROLLER_GIT_CREDENTIALS"
)

func LoadController(
	lookupEnvironment func(string) (string, bool),
) (controller.Config, error) {
	if lookupEnvironment == nil {
		return controller.Config{}, errors.New("environment lookup is required")
	}

	cfg := controller.Config{}
	cfg.ClusterName, _ = lookupEnvironment(controllerClusterEnvironment)
	cfg.ServerURL, _ = lookupEnvironment(controllerServerEnvironment)
	cfg.EnrollmentToken, _ = lookupEnvironment(controllerEnrollmentTokenEnvironment)
	cfg.BearerToken, _ = lookupEnvironment(controllerTokenEnvironment)
	cfg.CheckoutImage, _ = lookupEnvironment(controllerCheckoutImageEnvironment)
	cfg.HarnessImage, _ = lookupEnvironment(controllerHarnessImageEnvironment)
	cfg.GitCredentials = map[string]string{}

	if value, ok := lookupEnvironment(controllerGitCredentialsEnvironment); ok {
		if err := json.Unmarshal([]byte(value), &cfg.GitCredentials); err != nil {
			return controller.Config{}, fmt.Errorf(
				"parse %s: %w",
				controllerGitCredentialsEnvironment,
				err,
			)
		}
		if cfg.GitCredentials == nil {
			cfg.GitCredentials = map[string]string{}
		}
	}

	if err := cfg.Validate(); err != nil {
		return controller.Config{}, fmt.Errorf(
			"validate controller configuration: %w",
			err,
		)
	}

	return cfg, nil
}
