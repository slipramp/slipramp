package server

import (
	"testing"
	"time"

	"github.com/slipramp/slipramp/internal/placement"
	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/testcheck"
)

const testArtifactTokenSecret = "test-artifact-token-secret-32-bytes"

func TestDefaultConfig(t *testing.T) {
	t.Parallel()

	settings := DefaultConfig()

	if settings.Address != ":8080" {
		t.Errorf("Address = %q, want %q", settings.Address, ":8080")
	}
	if settings.MetricsAddress != ":9091" {
		t.Errorf("MetricsAddress = %q, want %q", settings.MetricsAddress, ":9091")
	}

	if settings.ShutdownTimeout != 10*time.Second {
		t.Errorf("ShutdownTimeout = %s, want %s", settings.ShutdownTimeout, 10*time.Second)
	}

	if settings.RepositoryRefreshInterval != time.Minute {
		t.Errorf("RepositoryRefreshInterval = %s, want %s", settings.RepositoryRefreshInterval, time.Minute)
	}

	if settings.RepositoryFetchTimeout != 30*time.Second {
		t.Errorf("RepositoryFetchTimeout = %s, want %s", settings.RepositoryFetchTimeout, 30*time.Second)
	}

	if settings.ArtifactRetentionPeriod != 30*24*time.Hour {
		t.Errorf("ArtifactRetentionPeriod = %s, want %s", settings.ArtifactRetentionPeriod, 30*24*time.Hour)
	}

	if settings.ArtifactReconcileInterval != time.Hour {
		t.Errorf("ArtifactReconcileInterval = %s, want %s", settings.ArtifactReconcileInterval, time.Hour)
	}

	if settings.ArtifactOrphanGracePeriod != 24*time.Hour {
		t.Errorf("ArtifactOrphanGracePeriod = %s, want %s", settings.ArtifactOrphanGracePeriod, 24*time.Hour)
	}

	if settings.ProductAuthentication.SessionLifetime != 12*time.Hour {
		t.Errorf(
			"ProductAuthentication.SessionLifetime = %s, want 12h",
			settings.ProductAuthentication.SessionLifetime,
		)
	}
	if settings.Notifications.Retry.MaximumAttempts != 5 ||
		settings.Notifications.Retry.MaximumBackoff != time.Hour ||
		settings.Notifications.RetentionPeriod != 30*24*time.Hour {
		t.Errorf(
			"notification defaults = attempts %d, backoff %s, retention %s",
			settings.Notifications.Retry.MaximumAttempts,
			settings.Notifications.Retry.MaximumBackoff,
			settings.Notifications.RetentionPeriod,
		)
	}

	if err := settings.Database.Validate(); err != nil {
		t.Errorf("Database.Validate() error = %v", err)
	}

	if err := settings.ObjectStore.Validate(); err != nil {
		t.Errorf("ObjectStore.Validate() error = %v", err)
	}

	if settings.Placements.Len() != 0 {
		t.Errorf("Placements.Len() = %d, want 0", settings.Placements.Len())
	}
}

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	configuredPlacements, err := placement.NewSet(map[string]placement.Config{
		"infrastructure": {
			ClusterName:  "colo",
			JobNamespace: "slipramp-runs",
		},
	})
	if err != nil {
		t.Fatalf("placement.NewSet() error = %v", err)
	}

	tests := []struct {
		name      string
		configure func(*Config)
		wantError string
	}{
		{
			name: "valid",
		},
		{
			name: "missing artifact token secret",
			configure: func(settings *Config) {
				settings.ArtifactTokenSecret = ""
			},
			wantError: "artifact token secret",
		},
		{
			name: "missing OIDC provider",
			configure: func(settings *Config) {
				settings.ProductAuthentication.OIDC.Issuer = ""
				settings.ProductAuthentication.OIDC.ClientID = ""
				settings.ProductAuthentication.OIDC.ClientSecret = ""
				settings.ProductAuthentication.OIDC.RedirectURL = ""
				settings.ProductAuthentication.OIDC.SubjectRoles = nil
			},
			wantError: "OIDC configuration is required",
		},
		{
			name: "empty address",
			configure: func(settings *Config) {
				settings.Address = ""
			},
			wantError: "address",
		},
		{
			name: "missing port",
			configure: func(settings *Config) {
				settings.Address = "localhost"
			},
			wantError: "address",
		},
		{
			name: "zero port",
			configure: func(settings *Config) {
				settings.Address = ":0"
			},
			wantError: "address",
		},
		{
			name: "empty metrics address",
			configure: func(settings *Config) {
				settings.MetricsAddress = ""
			},
			wantError: "metrics address",
		},
		{
			name: "invalid metrics address",
			configure: func(settings *Config) {
				settings.MetricsAddress = "localhost"
			},
			wantError: "metrics address",
		},
		{
			name: "zero metrics port",
			configure: func(settings *Config) {
				settings.MetricsAddress = ":0"
			},
			wantError: "metrics address",
		},
		{
			name: "negative shutdown timeout",
			configure: func(settings *Config) {
				settings.ShutdownTimeout = -1 * time.Second
			},
			wantError: "shutdown timeout",
		},
		{
			name: "negative trusted proxy hops",
			configure: func(settings *Config) {
				settings.TrustedProxyHops = -1
			},
			wantError: "trusted proxy hops",
		},
		{
			name: "invalid repository credential",
			configure: func(settings *Config) {
				settings.RepositoryRefreshCredentials["github"] = "relative"
			},
			wantError: "repository refresh credentials",
		},
		{
			name: "invalid repository refresh interval",
			configure: func(settings *Config) {
				settings.RepositoryRefreshInterval = 0
			},
			wantError: "repository refresh interval",
		},
		{
			name: "invalid repository fetch timeout",
			configure: func(settings *Config) {
				settings.RepositoryFetchTimeout = 0
			},
			wantError: "repository fetch timeout",
		},
		{
			name: "invalid controller cluster name",
			configure: func(settings *Config) {
				settings.ControllerEnrollmentTokens = map[string]string{
					"Invalid Cluster": "enrollment-token",
				}
			},
			wantError: "cluster name",
		},
		{
			name: "empty controller enrollment token",
			configure: func(settings *Config) {
				settings.ControllerEnrollmentTokens = map[string]string{"colo": ""}
			},
			wantError: "controller enrollment token",
		},
		{
			name: "invalid controller enrollment token",
			configure: func(settings *Config) {
				settings.ControllerEnrollmentTokens = map[string]string{
					"colo": "enrollment token",
				}
			},
			wantError: "controller enrollment token",
		},
		{
			name: "valid placement",
			configure: func(settings *Config) {
				settings.ControllerEnrollmentTokens["colo"] = "enrollment-token"
				settings.Placements = configuredPlacements
			},
		},
		{
			name: "unregistered placement cluster",
			configure: func(settings *Config) {
				settings.Placements = configuredPlacements
			},
			wantError: "unregistered cluster",
		},
		{
			name: "valid Kargo integration",
			configure: func(settings *Config) {
				settings.ControllerEnrollmentTokens["colo"] = "enrollment-token"
				settings.KargoIntegrations = validKargoIntegrations()
			},
		},
		{
			name: "unregistered Kargo cluster",
			configure: func(settings *Config) {
				settings.KargoIntegrations = validKargoIntegrations()
			},
			wantError: "kargo integration \"prod\" selects unregistered cluster \"colo\"",
		},
		{
			name: "valid Kargo candidate branch cleanup",
			configure: func(settings *Config) {
				settings.ControllerEnrollmentTokens["colo"] = "enrollment-token"
				settings.RepositoryRefreshCredentials["github-write"] = "/var/run/secrets/github.gitconfig"
				settings.KargoIntegrations = validKargoIntegrations()
				integration := settings.KargoIntegrations["prod"]
				integration.CandidateBranchCleanup = &promotion.CandidateBranchCleanupConfig{
					Retention:   7 * 24 * time.Hour,
					Credentials: map[string]string{"kcloud-ops": "github-write"},
				}
				settings.KargoIntegrations["prod"] = integration
			},
		},
		{
			name: "unknown Kargo candidate branch cleanup credential",
			configure: func(settings *Config) {
				settings.ControllerEnrollmentTokens["colo"] = "enrollment-token"
				settings.KargoIntegrations = validKargoIntegrations()
				integration := settings.KargoIntegrations["prod"]
				integration.CandidateBranchCleanup = &promotion.CandidateBranchCleanupConfig{
					Retention:   7 * 24 * time.Hour,
					Credentials: map[string]string{"kcloud-ops": "missing"},
				}
				settings.KargoIntegrations["prod"] = integration
			},
			wantError: "unknown Server Git credential \"missing\"",
		},
		{
			name: "invalid database",
			configure: func(settings *Config) {
				settings.Database.URL = ""
			},
			wantError: "database",
		},
		{
			name: "invalid object store",
			configure: func(settings *Config) {
				settings.ObjectStore.Endpoint = ""
			},
			wantError: "object store",
		},
		{
			name: "invalid artifact retention period",
			configure: func(settings *Config) {
				settings.ArtifactRetentionPeriod = 0
			},
			wantError: "artifact retention period",
		},
		{
			name: "invalid artifact reconciliation interval",
			configure: func(settings *Config) {
				settings.ArtifactReconcileInterval = 0
			},
			wantError: "artifact reconciliation interval",
		},
		{
			name: "invalid artifact orphan grace period",
			configure: func(settings *Config) {
				settings.ArtifactOrphanGracePeriod = 0
			},
			wantError: "artifact orphan grace period",
		},
		{
			name: "valid OIDC provider",
			configure: func(settings *Config) {
				settings.ProductAuthentication.OIDC.Issuer = "https://identity.example.com"
				settings.ProductAuthentication.OIDC.ClientID = "slipramp"
				settings.ProductAuthentication.OIDC.ClientSecret = "client-secret"
				settings.ProductAuthentication.OIDC.RedirectURL =
					"https://slipramp.example.com/api/v1/auth/callback"
				settings.ProductAuthentication.OIDC.SubjectRoles =
					map[string]productidentity.Role{"admin-subject": productidentity.RoleAdmin}
			},
		},
		{
			name: "OIDC provider without administrator",
			configure: func(settings *Config) {
				settings.ProductAuthentication.OIDC.Issuer = "https://identity.example.com"
				settings.ProductAuthentication.OIDC.ClientID = "slipramp"
				settings.ProductAuthentication.OIDC.ClientSecret = "client-secret"
				settings.ProductAuthentication.OIDC.RedirectURL =
					"https://slipramp.example.com/api/v1/auth/callback"
				settings.ProductAuthentication.OIDC.SubjectRoles =
					map[string]productidentity.Role{"viewer-subject": productidentity.RoleViewer}
			},
			wantError: "administrator",
		},
		{
			name: "OIDC provider with wrong callback path",
			configure: func(settings *Config) {
				settings.ProductAuthentication.OIDC.Issuer = "https://identity.example.com"
				settings.ProductAuthentication.OIDC.ClientID = "slipramp"
				settings.ProductAuthentication.OIDC.ClientSecret = "client-secret"
				settings.ProductAuthentication.OIDC.RedirectURL =
					"https://slipramp.example.com/other-callback"
				settings.ProductAuthentication.OIDC.SubjectRoles =
					map[string]productidentity.Role{"admin-subject": productidentity.RoleAdmin}
			},
			wantError: "/api/v1/auth/callback",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			settings := DefaultConfig()
			settings.ArtifactTokenSecret = testArtifactTokenSecret
			configureRequiredOIDC(&settings)
			if test.configure != nil {
				test.configure(&settings)
			}

			testcheck.ErrorContains(t, settings.Validate(), test.wantError)
		})
	}
}

func validKargoIntegrations() promotion.IntegrationSet {
	return promotion.IntegrationSet{
		"prod": {
			ClusterName:      "colo",
			UIURL:            "https://kargo.example.com",
			Repositories:     []string{"kcloud-ops"},
			EnvironmentOrder: []string{"homelab", "prod"},
			Preparation: promotion.PreparationConfig{
				Repository: "kcloud-ops",
				Target:     "kargo-promotions",
				Action:     "prepare-review",
			},
			MaximumStageObservationAge: 5 * time.Minute,
			PendingReviewRetention:     8 * 24 * time.Hour,
		},
	}
}

func configureRequiredOIDC(settings *Config) {
	settings.ProductAuthentication.OIDC.Issuer = "https://identity.example.com"
	settings.ProductAuthentication.OIDC.ClientID = "slipramp"
	settings.ProductAuthentication.OIDC.ClientSecret = "client-secret"
	settings.ProductAuthentication.OIDC.RedirectURL =
		"https://slipramp.example.com/api/v1/auth/callback"
	settings.ProductAuthentication.OIDC.SubjectRoles =
		map[string]productidentity.Role{"admin-subject": productidentity.RoleAdmin}
}
