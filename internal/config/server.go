package config

import (
	"errors"
	"fmt"
	"time"

	"github.com/slipramp/slipramp/internal/server"
)

const (
	serverAddressEnvironment         = "SLIPRAMP_SERVER_ADDRESS"
	serverShutdownTimeoutEnvironment = "SLIPRAMP_SERVER_SHUTDOWN_TIMEOUT"
)

func LoadServer(
	lookupEnvironment func(string) (string, bool),
) (server.Config, error) {
	if lookupEnvironment == nil {
		return server.Config{}, errors.New("environment lookup is required")
	}

	cfg := server.DefaultConfig()

	if value, ok := lookupEnvironment(serverAddressEnvironment); ok {
		cfg.Address = value
	}

	if value, ok := lookupEnvironment(serverShutdownTimeoutEnvironment); ok {
		timeout, err := time.ParseDuration(value)
		if err != nil {
			return server.Config{}, fmt.Errorf(
				"parse %s: %w",
				serverShutdownTimeoutEnvironment,
				err,
			)
		}

		cfg.ShutdownTimeout = timeout
	}

	if err := cfg.Validate(); err != nil {
		return server.Config{}, fmt.Errorf(
			"validate server configuration: %w",
			err,
		)
	}

	return cfg, nil
}
