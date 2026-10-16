package config

import (
	"errors"
	"fmt"

	"github.com/slipramp/slipramp/internal/controller"
)

const (
	controllerClusterEnvironment = "SLIPRAMP_CONTROLLER_CLUSTER"
	controllerServerEnvironment  = "SLIPRAMP_CONTROLLER_SERVER_URL"
	controllerTokenEnvironment   = "SLIPRAMP_CONTROLLER_TOKEN"
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
	cfg.BearerToken, _ = lookupEnvironment(controllerTokenEnvironment)

	if err := cfg.Validate(); err != nil {
		return controller.Config{}, fmt.Errorf(
			"validate controller configuration: %w",
			err,
		)
	}

	return cfg, nil
}
