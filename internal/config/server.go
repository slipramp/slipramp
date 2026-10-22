package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/server"
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
	serverArtifactTokenSecretEnvironment          = "SLIPRAMP_ARTIFACT_TOKEN_SECRET"
	serverArtifactRetentionPeriodEnvironment      = "SLIPRAMP_ARTIFACT_RETENTION_PERIOD"
	serverArtifactReconcileIntervalEnvironment    = "SLIPRAMP_ARTIFACT_RECONCILIATION_INTERVAL"
	serverArtifactOrphanGracePeriodEnvironment    = "SLIPRAMP_ARTIFACT_ORPHAN_GRACE_PERIOD"
	serverOIDCIssuerEnvironment                   = "SLIPRAMP_OIDC_ISSUER"
	serverOIDCClientIDEnvironment                 = "SLIPRAMP_OIDC_CLIENT_ID"
	serverOIDCClientSecretEnvironment             = "SLIPRAMP_OIDC_CLIENT_SECRET"
	serverOIDCRedirectURLEnvironment              = "SLIPRAMP_OIDC_REDIRECT_URL"
	serverOIDCSubjectRolesEnvironment             = "SLIPRAMP_OIDC_SUBJECT_ROLES"
	serverProductSessionLifetimeEnvironment       = "SLIPRAMP_PRODUCT_SESSION_LIFETIME"
	serverKargoIntegrationsEnvironment            = "SLIPRAMP_KARGO_INTEGRATIONS"
)

type serverKargoIntegration struct {
	Cluster      string   `json:"cluster"`
	Repositories []string `json:"repositories"`
	Preparation  struct {
		Repository string `json:"repository"`
		Target     string `json:"target"`
		Action     string `json:"action"`
	} `json:"preparation"`
	MaximumStageObservationAge string `json:"maximumStageObservationAge"`
	PendingReviewRetention     string `json:"pendingReviewRetention"`
}

func LoadServer(lookupEnvironment func(string) (string, bool)) (server.Config, error) {
	if lookupEnvironment == nil {
		return server.Config{}, errors.New("environment lookup is required")
	}

	cfg := server.DefaultConfig()

	if value, ok := lookupEnvironment(serverAddressEnvironment); ok {
		cfg.Address = value
	}

	if value, ok := lookupEnvironment(serverShutdownTimeoutEnvironment); ok {
		timeout, err := parseDurationEnvironment(serverShutdownTimeoutEnvironment, value)
		if err != nil {
			return server.Config{}, err
		}

		cfg.ShutdownTimeout = timeout
	}

	if value, ok := lookupEnvironment(serverControllerEnrollmentTokensEnvironment); ok {
		var enrollmentTokens map[string]string
		if err := json.Unmarshal([]byte(value), &enrollmentTokens); err != nil {
			return server.Config{}, fmt.Errorf("parse %s: %w", serverControllerEnrollmentTokensEnvironment, err)
		}

		if enrollmentTokens == nil {
			enrollmentTokens = map[string]string{}
		}

		cfg.ControllerEnrollmentTokens = enrollmentTokens
	}

	if value, ok := lookupEnvironment(serverRepositoryRefreshCredentialsEnvironment); ok {
		var credentials map[string]string
		if err := json.Unmarshal([]byte(value), &credentials); err != nil {
			return server.Config{}, fmt.Errorf("parse %s: %w", serverRepositoryRefreshCredentialsEnvironment, err)
		}

		if credentials == nil {
			credentials = map[string]string{}
		}

		cfg.RepositoryRefreshCredentials = credentials
	}

	if value, ok := lookupEnvironment(serverRepositoryRefreshIntervalEnvironment); ok {
		interval, err := parseDurationEnvironment(serverRepositoryRefreshIntervalEnvironment, value)
		if err != nil {
			return server.Config{}, err
		}

		cfg.RepositoryRefreshInterval = interval
	}

	if value, ok := lookupEnvironment(serverRepositoryFetchTimeoutEnvironment); ok {
		timeout, err := parseDurationEnvironment(serverRepositoryFetchTimeoutEnvironment, value)
		if err != nil {
			return server.Config{}, err
		}

		cfg.RepositoryFetchTimeout = timeout
	}

	if value, ok := lookupEnvironment(serverPlacementsFileEnvironment); ok {
		placements, err := loadPlacementsFile(value)
		if err != nil {
			return server.Config{}, fmt.Errorf("load %s from %q: %w", serverPlacementsFileEnvironment, value, err)
		}

		cfg.Placements = placements
	}

	if value, ok := lookupEnvironment(serverDatabaseURLEnvironment); ok {
		cfg.Database.URL = value
	}

	if value, ok := lookupEnvironment(serverDatabaseConnectTimeoutEnvironment); ok {
		timeout, err := parseDurationEnvironment(serverDatabaseConnectTimeoutEnvironment, value)
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
		timeout, err := parseDurationEnvironment(serverS3StartupTimeoutEnvironment, value)
		if err != nil {
			return server.Config{}, err
		}

		cfg.ObjectStore.StartupTimeout = timeout
	}

	if value, ok := lookupEnvironment(serverArtifactTokenSecretEnvironment); ok {
		cfg.ArtifactTokenSecret = value
	}

	if value, ok := lookupEnvironment(serverArtifactRetentionPeriodEnvironment); ok {
		period, err := parseDurationEnvironment(serverArtifactRetentionPeriodEnvironment, value)
		if err != nil {
			return server.Config{}, err
		}

		cfg.ArtifactRetentionPeriod = period
	}

	if value, ok := lookupEnvironment(serverArtifactReconcileIntervalEnvironment); ok {
		period, err := parseDurationEnvironment(serverArtifactReconcileIntervalEnvironment, value)
		if err != nil {
			return server.Config{}, err
		}

		cfg.ArtifactReconcileInterval = period
	}

	if value, ok := lookupEnvironment(serverArtifactOrphanGracePeriodEnvironment); ok {
		period, err := parseDurationEnvironment(serverArtifactOrphanGracePeriodEnvironment, value)
		if err != nil {
			return server.Config{}, err
		}

		cfg.ArtifactOrphanGracePeriod = period
	}

	if value, ok := lookupEnvironment(serverOIDCIssuerEnvironment); ok {
		cfg.ProductAuthentication.OIDC.Issuer = value
	}
	if value, ok := lookupEnvironment(serverOIDCClientIDEnvironment); ok {
		cfg.ProductAuthentication.OIDC.ClientID = value
	}
	if value, ok := lookupEnvironment(serverOIDCClientSecretEnvironment); ok {
		cfg.ProductAuthentication.OIDC.ClientSecret = value
	}
	if value, ok := lookupEnvironment(serverOIDCRedirectURLEnvironment); ok {
		cfg.ProductAuthentication.OIDC.RedirectURL = value
	}
	if value, ok := lookupEnvironment(serverOIDCSubjectRolesEnvironment); ok {
		var subjectRoles map[string]productidentity.Role
		if err := json.Unmarshal([]byte(value), &subjectRoles); err != nil {
			return server.Config{}, fmt.Errorf("parse %s: %w", serverOIDCSubjectRolesEnvironment, err)
		}
		if subjectRoles == nil {
			subjectRoles = map[string]productidentity.Role{}
		}

		cfg.ProductAuthentication.OIDC.SubjectRoles = subjectRoles
	}
	if value, ok := lookupEnvironment(serverProductSessionLifetimeEnvironment); ok {
		lifetime, err := parseDurationEnvironment(serverProductSessionLifetimeEnvironment, value)
		if err != nil {
			return server.Config{}, err
		}

		cfg.ProductAuthentication.SessionLifetime = lifetime
	}
	if value, ok := lookupEnvironment(serverKargoIntegrationsEnvironment); ok {
		integrations, err := parseServerKargoIntegrations(value)
		if err != nil {
			return server.Config{}, fmt.Errorf("parse %s: %w", serverKargoIntegrationsEnvironment, err)
		}

		cfg.KargoIntegrations = integrations
	}

	if err := cfg.Validate(); err != nil {
		return server.Config{}, fmt.Errorf("validate server configuration: %w", err)
	}

	return cfg, nil
}

func parseServerKargoIntegrations(value string) (promotion.IntegrationSet, error) {
	var encoded map[string]serverKargoIntegration
	if err := json.Unmarshal([]byte(value), &encoded); err != nil {
		return nil, err
	}
	if encoded == nil {
		return promotion.IntegrationSet{}, nil
	}

	integrations := make(promotion.IntegrationSet, len(encoded))
	for name, encodedIntegration := range encoded {
		maximumAge, err := time.ParseDuration(encodedIntegration.MaximumStageObservationAge)
		if err != nil {
			return nil, fmt.Errorf("integration %q maximum Stage observation age: %w", name, err)
		}
		retention, err := time.ParseDuration(encodedIntegration.PendingReviewRetention)
		if err != nil {
			return nil, fmt.Errorf("integration %q pending review retention: %w", name, err)
		}

		integrations[name] = promotion.IntegrationConfig{
			ClusterName:  encodedIntegration.Cluster,
			Repositories: encodedIntegration.Repositories,
			Preparation: promotion.PreparationConfig{
				Repository: encodedIntegration.Preparation.Repository,
				Target:     encodedIntegration.Preparation.Target,
				Action:     encodedIntegration.Preparation.Action,
			},
			MaximumStageObservationAge: maximumAge,
			PendingReviewRetention:     retention,
		}
	}

	return integrations, nil
}

func parseDurationEnvironment(name, value string) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}

	return duration, nil
}
