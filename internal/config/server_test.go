package config_test

import (
	"testing"
	"time"

	"github.com/slipramp/slipramp/internal/config"
	"github.com/slipramp/slipramp/internal/testcheck"
)

const (
	serverAddressEnvironment                      = "SLIPRAMP_SERVER_ADDRESS"
	serverShutdownTimeoutEnvironment              = "SLIPRAMP_SERVER_SHUTDOWN_TIMEOUT"
	serverControllerEnrollmentTokensEnvironment   = "SLIPRAMP_CONTROLLER_ENROLLMENT_TOKENS"
	serverRepositoryRefreshCredentialsEnvironment = "SLIPRAMP_REPOSITORY_REFRESH_CREDENTIALS"
	serverRepositoryRefreshIntervalEnvironment    = "SLIPRAMP_REPOSITORY_REFRESH_INTERVAL"
	serverRepositoryFetchTimeoutEnvironment       = "SLIPRAMP_REPOSITORY_FETCH_TIMEOUT"
	serverDatabaseURLEnvironment                  = "SLIPRAMP_DATABASE_URL"
	serverDatabaseConnectTimeoutEnvironment       = "SLIPRAMP_DATABASE_CONNECT_TIMEOUT"
	serverS3EndpointEnvironment                   = "SLIPRAMP_S3_ENDPOINT"
	serverS3RegionEnvironment                     = "SLIPRAMP_S3_REGION"
	serverS3BucketEnvironment                     = "SLIPRAMP_S3_BUCKET"
	serverS3AccessKeyIDEnvironment                = "SLIPRAMP_S3_ACCESS_KEY_ID"
	serverS3SecretAccessKeyEnvironment            = "SLIPRAMP_S3_SECRET_ACCESS_KEY"
	serverS3StartupTimeoutEnvironment             = "SLIPRAMP_S3_STARTUP_TIMEOUT"
	serverArtifactRetentionPeriodEnvironment      = "SLIPRAMP_ARTIFACT_RETENTION_PERIOD"
	serverArtifactReconcileIntervalEnvironment    = "SLIPRAMP_ARTIFACT_RECONCILIATION_INTERVAL"
	serverArtifactOrphanGracePeriodEnvironment    = "SLIPRAMP_ARTIFACT_ORPHAN_GRACE_PERIOD"
	serverOIDCIssuerEnvironment                   = "SLIPRAMP_OIDC_ISSUER"
	serverOIDCClientIDEnvironment                 = "SLIPRAMP_OIDC_CLIENT_ID"
	serverOIDCClientSecretEnvironment             = "SLIPRAMP_OIDC_CLIENT_SECRET"
	serverOIDCRedirectURLEnvironment              = "SLIPRAMP_OIDC_REDIRECT_URL"
	serverOIDCSubjectRolesEnvironment             = "SLIPRAMP_OIDC_SUBJECT_ROLES"
	serverProductSessionLifetimeEnvironment       = "SLIPRAMP_PRODUCT_SESSION_LIFETIME"
)

func TestLoadServerFromEnvironment(t *testing.T) {
	t.Parallel()

	environment := map[string]string{
		serverAddressEnvironment:                      "127.0.0.1:9000",
		serverShutdownTimeoutEnvironment:              "25s",
		serverControllerEnrollmentTokensEnvironment:   `{"colo":"enrollment-token"}`,
		serverRepositoryRefreshCredentialsEnvironment: `{"github-server":"/var/run/secrets/slipramp/github.gitconfig"}`,
		serverRepositoryRefreshIntervalEnvironment:    "2m",
		serverRepositoryFetchTimeoutEnvironment:       "45s",
		serverDatabaseURLEnvironment:                  "postgres://user:pass@db/slipramp",
		serverDatabaseConnectTimeoutEnvironment:       "8s",
		serverS3EndpointEnvironment:                   "https://objects.example.com",
		serverS3RegionEnvironment:                     "us-west-2",
		serverS3BucketEnvironment:                     "slipramp-artifacts",
		serverS3AccessKeyIDEnvironment:                "access-key",
		serverS3SecretAccessKeyEnvironment:            "secret-key",
		serverS3StartupTimeoutEnvironment:             "9s",
		serverArtifactRetentionPeriodEnvironment:      "720h",
		serverArtifactReconcileIntervalEnvironment:    "30m",
		serverArtifactOrphanGracePeriodEnvironment:    "48h",
		serverOIDCIssuerEnvironment:                   "https://identity.example.com",
		serverOIDCClientIDEnvironment:                 "slipramp",
		serverOIDCClientSecretEnvironment:             "client-secret",
		serverOIDCRedirectURLEnvironment:              "https://slipramp.example.com/api/v1/auth/callback",
		serverOIDCSubjectRolesEnvironment:             `{"admin-subject":"admin","operator-subject":"operator"}`,
		serverProductSessionLifetimeEnvironment:       "8h",
	}

	cfg, err := config.LoadServer(mapEnvironment(environment))
	if err != nil {
		t.Fatalf("LoadServer() error = %v", err)
	}

	if cfg.Address != "127.0.0.1:9000" {
		t.Errorf("Address = %q, want %q", cfg.Address, "127.0.0.1:9000")
	}

	if cfg.ShutdownTimeout != 25*time.Second {
		t.Errorf("ShutdownTimeout = %s, want %s", cfg.ShutdownTimeout, 25*time.Second)
	}

	if cfg.ControllerEnrollmentTokens["colo"] != "enrollment-token" {
		t.Error("ControllerEnrollmentTokens does not contain the configured credential")
	}

	if cfg.RepositoryRefreshCredentials["github-server"] !=
		"/var/run/secrets/slipramp/github.gitconfig" {
		t.Error("RepositoryRefreshCredentials does not contain github-server")
	}

	if cfg.RepositoryRefreshInterval != 2*time.Minute {
		t.Errorf("RepositoryRefreshInterval = %s, want %s", cfg.RepositoryRefreshInterval, 2*time.Minute)
	}

	if cfg.RepositoryFetchTimeout != 45*time.Second {
		t.Errorf("RepositoryFetchTimeout = %s, want %s", cfg.RepositoryFetchTimeout, 45*time.Second)
	}

	if cfg.Database.URL != environment[serverDatabaseURLEnvironment] {
		t.Errorf("Database.URL = %q, want %q", cfg.Database.URL, environment[serverDatabaseURLEnvironment])
	}

	if cfg.Database.ConnectTimeout != 8*time.Second {
		t.Errorf("Database.ConnectTimeout = %s, want %s", cfg.Database.ConnectTimeout, 8*time.Second)
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
		t.Errorf("ObjectStore.Bucket = %q, want %q", cfg.ObjectStore.Bucket, "slipramp-artifacts")
	}

	if cfg.ObjectStore.AccessKeyID != "access-key" {
		t.Errorf("ObjectStore.AccessKeyID = %q, want %q", cfg.ObjectStore.AccessKeyID, "access-key")
	}

	if cfg.ObjectStore.SecretAccessKey != "secret-key" {
		t.Error("ObjectStore.SecretAccessKey does not match configured value")
	}

	if cfg.ObjectStore.StartupTimeout != 9*time.Second {
		t.Errorf("ObjectStore.StartupTimeout = %s, want %s", cfg.ObjectStore.StartupTimeout, 9*time.Second)
	}

	if cfg.ArtifactRetentionPeriod != 720*time.Hour {
		t.Errorf("ArtifactRetentionPeriod = %s, want 720h", cfg.ArtifactRetentionPeriod)
	}

	if cfg.ArtifactReconcileInterval != 30*time.Minute {
		t.Errorf("ArtifactReconcileInterval = %s, want 30m", cfg.ArtifactReconcileInterval)
	}

	if cfg.ArtifactOrphanGracePeriod != 48*time.Hour {
		t.Errorf("ArtifactOrphanGracePeriod = %s, want 48h", cfg.ArtifactOrphanGracePeriod)
	}

	if cfg.ProductAuthentication.OIDC.Issuer != "https://identity.example.com" ||
		cfg.ProductAuthentication.OIDC.ClientID != "slipramp" ||
		cfg.ProductAuthentication.OIDC.ClientSecret != "client-secret" ||
		cfg.ProductAuthentication.OIDC.RedirectURL !=
			"https://slipramp.example.com/api/v1/auth/callback" {
		t.Errorf("ProductAuthentication.OIDC = %#v", cfg.ProductAuthentication.OIDC)
	}
	if cfg.ProductAuthentication.OIDC.SubjectRoles["admin-subject"] != "admin" ||
		cfg.ProductAuthentication.OIDC.SubjectRoles["operator-subject"] != "operator" {
		t.Errorf("ProductAuthentication.OIDC.SubjectRoles = %v", cfg.ProductAuthentication.OIDC.SubjectRoles)
	}
	if cfg.ProductAuthentication.SessionLifetime != 8*time.Hour {
		t.Errorf("ProductAuthentication.SessionLifetime = %s, want 8h", cfg.ProductAuthentication.SessionLifetime)
	}
}

func TestLoadServerRejectsInvalidEnvironment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		environment map[string]string
		wantError   string
	}{
		{
			name:        "missing OIDC configuration",
			environment: map[string]string{},
			wantError:   "OIDC configuration is required",
		},
		{
			name: "malformed shutdown timeout",
			environment: map[string]string{
				serverShutdownTimeoutEnvironment: "soon",
			},
			wantError: serverShutdownTimeoutEnvironment,
		},
		{
			name: "malformed controller enrollment tokens",
			environment: map[string]string{
				serverControllerEnrollmentTokensEnvironment: "not-json",
			},
			wantError: serverControllerEnrollmentTokensEnvironment,
		},
		{
			name: "malformed repository refresh credentials",
			environment: map[string]string{
				serverRepositoryRefreshCredentialsEnvironment: "not-json",
			},
			wantError: serverRepositoryRefreshCredentialsEnvironment,
		},
		{
			name: "malformed OIDC subject roles",
			environment: map[string]string{
				serverOIDCSubjectRolesEnvironment: "not-json",
			},
			wantError: serverOIDCSubjectRolesEnvironment,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := config.LoadServer(mapEnvironment(test.environment))
			testcheck.ErrorContains(t, err, test.wantError)
		})
	}
}

func TestLoadServerRequiresEnvironmentLookup(t *testing.T) {
	t.Parallel()

	_, err := config.LoadServer(nil)
	testcheck.ErrorContains(t, err, "environment lookup")
}

func mapEnvironment(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}
