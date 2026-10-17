package config

import (
	"errors"
	"fmt"
	"time"

	"github.com/slipramp/slipramp/internal/server"
)

const (
	serverAddressEnvironment                = "SLIPRAMP_SERVER_ADDRESS"
	serverShutdownTimeoutEnvironment        = "SLIPRAMP_SERVER_SHUTDOWN_TIMEOUT"
	serverDatabaseURLEnvironment            = "SLIPRAMP_DATABASE_URL"
	serverDatabaseConnectTimeoutEnvironment = "SLIPRAMP_DATABASE_CONNECT_TIMEOUT"
	serverS3EndpointEnvironment             = "SLIPRAMP_S3_ENDPOINT"
	serverS3RegionEnvironment               = "SLIPRAMP_S3_REGION"
	serverS3BucketEnvironment               = "SLIPRAMP_S3_BUCKET"
	serverS3AccessKeyIDEnvironment          = "SLIPRAMP_S3_ACCESS_KEY_ID"
	serverS3SecretAccessKeyEnvironment      = "SLIPRAMP_S3_SECRET_ACCESS_KEY"
	serverS3StartupTimeoutEnvironment       = "SLIPRAMP_S3_STARTUP_TIMEOUT"
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
		timeout, err := parseDurationEnvironment(
			serverShutdownTimeoutEnvironment,
			value,
		)
		if err != nil {
			return server.Config{}, err
		}

		cfg.ShutdownTimeout = timeout
	}

	if value, ok := lookupEnvironment(serverDatabaseURLEnvironment); ok {
		cfg.Database.URL = value
	}

	if value, ok := lookupEnvironment(serverDatabaseConnectTimeoutEnvironment); ok {
		timeout, err := parseDurationEnvironment(
			serverDatabaseConnectTimeoutEnvironment,
			value,
		)
		if err != nil {
			return server.Config{}, err
		}

		cfg.Database.ConnectTimeout = timeout
	}

	if value, ok := lookupEnvironment(serverS3EndpointEnvironment); ok {
		cfg.ObjectStore.Endpoint = value
	}

	if value, ok := lookupEnvironment(serverS3RegionEnvironment); ok {
		cfg.ObjectStore.Region = value
	}

	if value, ok := lookupEnvironment(serverS3BucketEnvironment); ok {
		cfg.ObjectStore.Bucket = value
	}

	if value, ok := lookupEnvironment(serverS3AccessKeyIDEnvironment); ok {
		cfg.ObjectStore.AccessKeyID = value
	}

	if value, ok := lookupEnvironment(serverS3SecretAccessKeyEnvironment); ok {
		cfg.ObjectStore.SecretAccessKey = value
	}

	if value, ok := lookupEnvironment(serverS3StartupTimeoutEnvironment); ok {
		timeout, err := parseDurationEnvironment(
			serverS3StartupTimeoutEnvironment,
			value,
		)
		if err != nil {
			return server.Config{}, err
		}

		cfg.ObjectStore.StartupTimeout = timeout
	}

	if err := cfg.Validate(); err != nil {
		return server.Config{}, fmt.Errorf(
			"validate server configuration: %w",
			err,
		)
	}

	return cfg, nil
}

func parseDurationEnvironment(name, value string) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}

	return duration, nil
}
