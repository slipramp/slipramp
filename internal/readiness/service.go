// Package readiness reports whether the Server can reach the durable
// dependencies required to accept work.
package readiness

import (
	"context"
	"time"
)

const checkTimeout = 2 * time.Second

type DependencyStatus string

const (
	DependencyAvailable   DependencyStatus = "available"
	DependencyUnavailable DependencyStatus = "unavailable"
)

type Report struct {
	Database        DependencyStatus
	ArtifactStorage DependencyStatus
}

func (r Report) Ready() bool {
	return r.Database == DependencyAvailable &&
		r.ArtifactStorage == DependencyAvailable
}

type Database interface {
	Ping(ctx context.Context) error
}

type ArtifactStorage interface {
	Check(ctx context.Context) error
}

type Service struct {
	database        Database
	artifactStorage ArtifactStorage
	timeout         time.Duration
}

func NewService(
	database Database,
	artifactStorage ArtifactStorage,
) *Service {
	return &Service{
		database:        database,
		artifactStorage: artifactStorage,
		timeout:         checkTimeout,
	}
}

func (s *Service) Check(ctx context.Context) Report {
	checkContext, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	databaseResult := checkDependency(checkContext, s.database.Ping)
	artifactStorageResult := checkDependency(
		checkContext,
		s.artifactStorage.Check,
	)

	return Report{
		Database:        dependencyStatus(checkContext, databaseResult),
		ArtifactStorage: dependencyStatus(checkContext, artifactStorageResult),
	}
}

func checkDependency(
	ctx context.Context,
	check func(context.Context) error,
) <-chan DependencyStatus {
	result := make(chan DependencyStatus, 1)

	go func() {
		status := DependencyAvailable
		if err := check(ctx); err != nil {
			status = DependencyUnavailable
		}

		result <- status
	}()

	return result
}

func dependencyStatus(
	ctx context.Context,
	result <-chan DependencyStatus,
) DependencyStatus {
	select {
	case status := <-result:
		return status
	case <-ctx.Done():
		return DependencyUnavailable
	}
}
