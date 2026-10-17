package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/slipramp/slipramp/internal/config"
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

func TestLoadServerUsesDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := config.LoadServer(mapEnvironment(nil))
	if err != nil {
		t.Fatalf("LoadServer() error = %v", err)
	}

	if cfg.Address != ":8080" {
		t.Errorf("Address = %q, want %q", cfg.Address, ":8080")
	}

	if cfg.ShutdownTimeout != 10*time.Second {
		t.Errorf(
			"ShutdownTimeout = %s, want %s",
			cfg.ShutdownTimeout,
			10*time.Second,
		)
	}

	if cfg.Database.URL == "" {
		t.Error("Database.URL is empty")
	}

	if cfg.ObjectStore.Endpoint == "" {
		t.Error("ObjectStore.Endpoint is empty")
	}
}

func TestLoadServerFromEnvironment(t *testing.T) {
	t.Parallel()

	environment := map[string]string{
		serverAddressEnvironment:                "127.0.0.1:9000",
		serverShutdownTimeoutEnvironment:        "25s",
		serverDatabaseURLEnvironment:            "postgres://user:pass@db/slipramp",
		serverDatabaseConnectTimeoutEnvironment: "8s",
		serverS3EndpointEnvironment:             "https://objects.example.com",
		serverS3RegionEnvironment:               "us-west-2",
		serverS3BucketEnvironment:               "slipramp-artifacts",
		serverS3AccessKeyIDEnvironment:          "access-key",
		serverS3SecretAccessKeyEnvironment:      "secret-key",
		serverS3StartupTimeoutEnvironment:       "9s",
	}

	cfg, err := config.LoadServer(mapEnvironment(environment))
	if err != nil {
		t.Fatalf("LoadServer() error = %v", err)
	}

	if cfg.Address != "127.0.0.1:9000" {
		t.Errorf(
			"Address = %q, want %q",
			cfg.Address,
			"127.0.0.1:9000",
		)
	}

	if cfg.ShutdownTimeout != 25*time.Second {
		t.Errorf(
			"ShutdownTimeout = %s, want %s",
			cfg.ShutdownTimeout,
			25*time.Second,
		)
	}

	if cfg.Database.URL != environment[serverDatabaseURLEnvironment] {
		t.Errorf(
			"Database.URL = %q, want %q",
			cfg.Database.URL,
			environment[serverDatabaseURLEnvironment],
		)
	}

	if cfg.Database.ConnectTimeout != 8*time.Second {
		t.Errorf(
			"Database.ConnectTimeout = %s, want %s",
			cfg.Database.ConnectTimeout,
			8*time.Second,
		)
	}

	if cfg.ObjectStore.Endpoint != environment[serverS3EndpointEnvironment] {
		t.Errorf(
			"ObjectStore.Endpoint = %q, want %q",
			cfg.ObjectStore.Endpoint,
			environment[serverS3EndpointEnvironment],
		)
	}

	if cfg.ObjectStore.Region != "us-west-2" {
		t.Errorf("ObjectStore.Region = %q, want %q", cfg.ObjectStore.Region, "us-west-2")
	}

	if cfg.ObjectStore.Bucket != "slipramp-artifacts" {
		t.Errorf(
			"ObjectStore.Bucket = %q, want %q",
			cfg.ObjectStore.Bucket,
			"slipramp-artifacts",
		)
	}

	if cfg.ObjectStore.AccessKeyID != "access-key" {
		t.Errorf(
			"ObjectStore.AccessKeyID = %q, want %q",
			cfg.ObjectStore.AccessKeyID,
			"access-key",
		)
	}

	if cfg.ObjectStore.SecretAccessKey != "secret-key" {
		t.Error("ObjectStore.SecretAccessKey does not match configured value")
	}

	if cfg.ObjectStore.StartupTimeout != 9*time.Second {
		t.Errorf(
			"ObjectStore.StartupTimeout = %s, want %s",
			cfg.ObjectStore.StartupTimeout,
			9*time.Second,
		)
	}
}

func TestLoadServerRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		environment map[string]string
		wantError   string
	}{
		{
			name:        "empty address",
			environment: map[string]string{serverAddressEnvironment: ""},
			wantError:   "validate server configuration",
		},
		{
			name:        "missing port",
			environment: map[string]string{serverAddressEnvironment: "localhost"},
			wantError:   "validate server configuration",
		},
		{
			name:        "zero port",
			environment: map[string]string{serverAddressEnvironment: ":0"},
			wantError:   "validate server configuration",
		},
		{
			name: "malformed shutdown timeout",
			environment: map[string]string{
				serverShutdownTimeoutEnvironment: "soon",
			},
			wantError: serverShutdownTimeoutEnvironment,
		},
		{
			name: "negative shutdown timeout",
			environment: map[string]string{
				serverShutdownTimeoutEnvironment: "-1s",
			},
			wantError: "validate server configuration",
		},
		{
			name: "malformed database timeout",
			environment: map[string]string{
				serverDatabaseConnectTimeoutEnvironment: "soon",
			},
			wantError: serverDatabaseConnectTimeoutEnvironment,
		},
		{
			name: "empty database URL",
			environment: map[string]string{
				serverDatabaseURLEnvironment: "",
			},
			wantError: "validate server configuration",
		},
		{
			name: "malformed S3 timeout",
			environment: map[string]string{
				serverS3StartupTimeoutEnvironment: "soon",
			},
			wantError: serverS3StartupTimeoutEnvironment,
		},
		{
			name: "invalid S3 endpoint",
			environment: map[string]string{
				serverS3EndpointEnvironment: "objects.example.com",
			},
			wantError: "validate server configuration",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := config.LoadServer(mapEnvironment(test.environment))
			if err == nil {
				t.Fatal("LoadServer() error = nil, want an error")
			}

			if !strings.Contains(err.Error(), test.wantError) {
				t.Errorf(
					"LoadServer() error = %q, want %q",
					err,
					test.wantError,
				)
			}
		})
	}
}

func TestLoadServerRequiresEnvironmentLookup(t *testing.T) {
	t.Parallel()

	_, err := config.LoadServer(nil)
	if err == nil {
		t.Fatal("LoadServer() error = nil, want an error")
	}

	if !strings.Contains(err.Error(), "environment lookup") {
		t.Errorf("LoadServer() error = %q, want environment lookup", err)
	}
}

func mapEnvironment(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}
