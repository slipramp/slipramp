package server

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/slipramp/slipramp/internal/artifact"
	"github.com/slipramp/slipramp/internal/auth"
	"github.com/slipramp/slipramp/internal/cluster"
	"github.com/slipramp/slipramp/internal/objectstore"
	"github.com/slipramp/slipramp/internal/placement"
	"github.com/slipramp/slipramp/internal/repository"
	"github.com/slipramp/slipramp/internal/store"
)

type Config struct {
	Address                      string
	ShutdownTimeout              time.Duration
	ControllerEnrollmentTokens   map[string]string
	Placements                   placement.Set
	RepositoryRefreshCredentials repository.RefreshCredentials
	RepositoryRefreshInterval    time.Duration
	RepositoryFetchTimeout       time.Duration
	Database                     store.Config
	ObjectStore                  objectstore.Config
	ArtifactTokenSecret          string
	ArtifactRetentionPeriod      time.Duration
	ArtifactReconcileInterval    time.Duration
	ArtifactOrphanGracePeriod    time.Duration
}

func DefaultConfig() Config {
	return Config{
		Address:                      ":8080",
		ShutdownTimeout:              10 * time.Second,
		ControllerEnrollmentTokens:   map[string]string{},
		RepositoryRefreshCredentials: repository.RefreshCredentials{},
		RepositoryRefreshInterval:    time.Minute,
		RepositoryFetchTimeout:       30 * time.Second,
		Database:                     store.DefaultConfig(),
		ObjectStore:                  objectstore.DefaultConfig(),
		ArtifactTokenSecret:          "slipramp-development-artifact-token-secret",
		ArtifactRetentionPeriod:      artifact.DefaultRetentionPeriod,
		ArtifactReconcileInterval:    artifact.DefaultReconcileInterval,
		ArtifactOrphanGracePeriod:    artifact.DefaultOrphanGracePeriod,
	}
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.Address) == "" {
		return fmt.Errorf("address must not be empty")
	}

	_, port, err := net.SplitHostPort(c.Address)
	if err != nil {
		return fmt.Errorf("validate address: %w", err)
	}

	portNumber, err := strconv.ParseUint(port, 10, 16)
	if err != nil || portNumber == 0 {
		return fmt.Errorf(
			"address must contain a numeric port between 1 and 65535",
		)
	}

	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("shutdown timeout must be positive")
	}

	if err := c.RepositoryRefreshCredentials.Validate(); err != nil {
		return fmt.Errorf("validate repository refresh credentials: %w", err)
	}

	if c.RepositoryRefreshInterval <= 0 {
		return fmt.Errorf("repository refresh interval must be positive")
	}

	if c.RepositoryFetchTimeout <= 0 {
		return fmt.Errorf("repository fetch timeout must be positive")
	}

	for clusterName, token := range c.ControllerEnrollmentTokens {
		if err := cluster.ValidateName(clusterName); err != nil {
			return fmt.Errorf(
				"validate controller enrollment credential: %w",
				err,
			)
		}

		if err := auth.ValidateBearerToken(token); err != nil {
			return fmt.Errorf(
				"controller enrollment token for cluster %q is invalid: %w",
				clusterName,
				err,
			)
		}
	}

	for _, placementName := range c.Placements.Names() {
		placementConfig, _ := c.Placements.Lookup(placementName)
		if _, ok := c.ControllerEnrollmentTokens[placementConfig.ClusterName]; !ok {
			return fmt.Errorf(
				"placement %q selects unregistered cluster %q",
				placementName,
				placementConfig.ClusterName,
			)
		}
	}

	if err := c.Database.Validate(); err != nil {
		return fmt.Errorf("validate database: %w", err)
	}

	if err := c.ObjectStore.Validate(); err != nil {
		return fmt.Errorf("validate object store: %w", err)
	}

	if len(c.ArtifactTokenSecret) < 32 {
		return fmt.Errorf("artifact token secret must be at least 32 characters")
	}

	if c.ArtifactRetentionPeriod <= 0 {
		return fmt.Errorf("artifact retention period must be positive")
	}

	if c.ArtifactReconcileInterval <= 0 {
		return fmt.Errorf("artifact reconciliation interval must be positive")
	}

	if c.ArtifactOrphanGracePeriod <= 0 {
		return fmt.Errorf("artifact orphan grace period must be positive")
	}

	return nil
}
