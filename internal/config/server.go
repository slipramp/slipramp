package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/slipramp/slipramp/internal/notification"
	"github.com/slipramp/slipramp/internal/placement"
	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/promotion"
	"github.com/slipramp/slipramp/internal/server"
)

const (
	serverAddressEnvironment                      = "SLIPRAMP_SERVER_ADDRESS"
	serverMetricsAddressEnvironment               = "SLIPRAMP_SERVER_METRICS_ADDRESS"
	serverShutdownTimeoutEnvironment              = "SLIPRAMP_SERVER_SHUTDOWN_TIMEOUT"
	serverTrustedProxyHopsEnvironment             = "SLIPRAMP_SERVER_TRUST_PROXY_HOPS"
	serverControllerEnrollmentTokensEnvironment   = "SLIPRAMP_CONTROLLER_ENROLLMENT_TOKENS"
	serverRepositoryRefreshCredentialsEnvironment = "SLIPRAMP_REPOSITORY_REFRESH_CREDENTIALS"
	serverRepositoryRefreshIntervalEnvironment    = "SLIPRAMP_REPOSITORY_REFRESH_INTERVAL"
	serverRepositoryFetchTimeoutEnvironment       = "SLIPRAMP_REPOSITORY_FETCH_TIMEOUT"
	serverPlacementsFileEnvironment               = "SLIPRAMP_PLACEMENTS_FILE"
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
	serverMCPOAuthIssuerEnvironment               = "SLIPRAMP_MCP_OAUTH_ISSUER"
	serverMCPOAuthResourceEnvironment             = "SLIPRAMP_MCP_OAUTH_RESOURCE"
	serverKargoIntegrationsFileEnvironment        = "SLIPRAMP_KARGO_INTEGRATIONS_FILE"
	serverNotificationPublicURLEnvironment        = "SLIPRAMP_NOTIFICATIONS_PUBLIC_URL"
	serverNotificationDestinationsEnvironment     = "SLIPRAMP_NOTIFICATIONS_DESTINATIONS"
	serverNotificationRoutesEnvironment           = "SLIPRAMP_NOTIFICATIONS_ROUTES"
	serverNotificationMaxAttemptsEnvironment      = "SLIPRAMP_NOTIFICATIONS_MAX_ATTEMPTS"
	serverNotificationMaxBackoffEnvironment       = "SLIPRAMP_NOTIFICATIONS_MAX_BACKOFF"
	serverNotificationRetentionPeriodEnvironment  = "SLIPRAMP_NOTIFICATIONS_RETENTION_PERIOD"
)

type serverNotificationDestination struct {
	Type           notification.DestinationType `json:"type"`
	WebhookURLFile string                       `json:"webhookUrlFile"`
}

type serverNotificationRoute struct {
	Events       []notification.EventType `json:"events"`
	Destinations []string                 `json:"destinations"`
}

const maximumNotificationSecretFileSize = 16 * 1024

func LoadServer(lookupEnvironment func(string) (string, bool)) (server.Config, error) {
	if lookupEnvironment == nil {
		return server.Config{}, errors.New("environment lookup is required")
	}

	cfg := server.DefaultConfig()

	if value, ok := lookupEnvironment(serverAddressEnvironment); ok {
		cfg.Address = value
	}

	if value, ok := lookupEnvironment(serverMetricsAddressEnvironment); ok {
		cfg.MetricsAddress = value
	}

	if value, ok := lookupEnvironment(serverShutdownTimeoutEnvironment); ok {
		timeout, err := parseDurationEnvironment(serverShutdownTimeoutEnvironment, value)
		if err != nil {
			return server.Config{}, err
		}

		cfg.ShutdownTimeout = timeout
	}

	if value, ok := lookupEnvironment(serverTrustedProxyHopsEnvironment); ok {
		trustedProxyHops, err := strconv.Atoi(value)
		if err != nil {
			return server.Config{}, fmt.Errorf("parse %s: %w", serverTrustedProxyHopsEnvironment, err)
		}
		cfg.TrustedProxyHops = trustedProxyHops
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
		placements, err := placement.LoadFile(value)
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
	if value, ok := lookupEnvironment(serverMCPOAuthIssuerEnvironment); ok {
		cfg.MCP.OAuth.Issuer = value
	}
	if value, ok := lookupEnvironment(serverMCPOAuthResourceEnvironment); ok {
		cfg.MCP.OAuth.Resource = value
	}
	if value, ok := lookupEnvironment(serverKargoIntegrationsFileEnvironment); ok {
		integrations, err := promotion.LoadIntegrationFile(value)
		if err != nil {
			return server.Config{}, fmt.Errorf(
				"load %s from %q: %w",
				serverKargoIntegrationsFileEnvironment,
				value,
				err,
			)
		}

		cfg.KargoIntegrations = integrations
	}
	if value, ok := lookupEnvironment(serverNotificationPublicURLEnvironment); ok {
		cfg.Notifications.PublicURL = value
	}
	if value, ok := lookupEnvironment(serverNotificationDestinationsEnvironment); ok {
		destinations, err := parseServerNotificationDestinations(value)
		if err != nil {
			return server.Config{}, fmt.Errorf("parse %s: %w", serverNotificationDestinationsEnvironment, err)
		}

		cfg.Notifications.Destinations = destinations
	}
	if value, ok := lookupEnvironment(serverNotificationRoutesEnvironment); ok {
		var encoded []serverNotificationRoute
		if err := decodeStrictJSON(value, &encoded); err != nil {
			return server.Config{}, fmt.Errorf("parse %s: %w", serverNotificationRoutesEnvironment, err)
		}
		if encoded == nil {
			encoded = []serverNotificationRoute{}
		}
		cfg.Notifications.Routes = make([]notification.Route, 0, len(encoded))
		for _, route := range encoded {
			cfg.Notifications.Routes = append(cfg.Notifications.Routes, notification.Route{
				Events:       route.Events,
				Destinations: route.Destinations,
			})
		}
	}
	if value, ok := lookupEnvironment(serverNotificationMaxAttemptsEnvironment); ok {
		maximumAttempts, err := strconv.Atoi(value)
		if err != nil {
			return server.Config{}, fmt.Errorf("parse %s: %w", serverNotificationMaxAttemptsEnvironment, err)
		}
		cfg.Notifications.Retry.MaximumAttempts = maximumAttempts
	}
	if value, ok := lookupEnvironment(serverNotificationMaxBackoffEnvironment); ok {
		maximumBackoff, err := parseDurationEnvironment(serverNotificationMaxBackoffEnvironment, value)
		if err != nil {
			return server.Config{}, err
		}
		cfg.Notifications.Retry.MaximumBackoff = maximumBackoff
	}
	if value, ok := lookupEnvironment(serverNotificationRetentionPeriodEnvironment); ok {
		retentionPeriod, err := parseDurationEnvironment(serverNotificationRetentionPeriodEnvironment, value)
		if err != nil {
			return server.Config{}, err
		}
		cfg.Notifications.RetentionPeriod = retentionPeriod
	}

	if err := cfg.Validate(); err != nil {
		return server.Config{}, fmt.Errorf("validate server configuration: %w", err)
	}

	return cfg, nil
}

func parseServerNotificationDestinations(value string) (map[string]notification.Destination, error) {
	decoder := json.NewDecoder(strings.NewReader(value))
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return nil, errors.New("notification destinations must be an object")
	}

	destinations := map[string]notification.Destination{}
	for decoder.More() {
		nameToken, tokenErr := decoder.Token()
		if tokenErr != nil {
			return nil, tokenErr
		}
		name, ok := nameToken.(string)
		if !ok {
			return nil, errors.New("notification destination name must be a string")
		}
		if _, duplicate := destinations[name]; duplicate {
			return nil, fmt.Errorf("duplicate notification destination name %q", name)
		}

		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, err
		}
		var encoded serverNotificationDestination
		if err := decodeStrictJSONBytes(raw, &encoded); err != nil {
			return nil, fmt.Errorf("destination %q: %w", name, err)
		}
		if encoded.Type != notification.DestinationTypeSlack {
			return nil, fmt.Errorf("destination %q has unknown type %q", name, encoded.Type)
		}

		webhookURL, err := readNotificationSecretFile(encoded.WebhookURLFile)
		if err != nil {
			return nil, fmt.Errorf("read destination %q Slack webhook URL file", name)
		}
		destination, err := notification.NewSlackDestination(encoded.WebhookURLFile, string(webhookURL))
		if err != nil {
			return nil, fmt.Errorf("destination %q: %w", name, err)
		}
		destinations[name] = destination
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	if token, err := decoder.Token(); err != io.EOF {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("unexpected trailing JSON token %v", token)
	}

	return destinations, nil
}

func readNotificationSecretFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = file.Close()
	}()

	value, err := io.ReadAll(io.LimitReader(file, maximumNotificationSecretFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(value) > maximumNotificationSecretFileSize {
		return nil, fmt.Errorf("secret file exceeds %d bytes", maximumNotificationSecretFileSize)
	}

	return value, nil
}

func decodeStrictJSON(value string, destination any) error {
	return decodeStrictJSONBytes([]byte(value), destination)
}

func decodeStrictJSONBytes(value []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if token, err := decoder.Token(); err != io.EOF {
		if err != nil {
			return err
		}
		return fmt.Errorf("unexpected trailing JSON token %v", token)
	}

	return nil
}

func parseDurationEnvironment(name, value string) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}

	return duration, nil
}
