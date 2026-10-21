package readiness

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestServiceReportsDependencyStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		databaseError        error
		artifactStorageError error
		wantDatabase         DependencyStatus
		wantArtifactStorage  DependencyStatus
		wantReady            bool
	}{
		{
			name:                "ready",
			wantDatabase:        DependencyAvailable,
			wantArtifactStorage: DependencyAvailable,
			wantReady:           true,
		},
		{
			name:                "database unavailable",
			databaseError:       errors.New("database unavailable"),
			wantDatabase:        DependencyUnavailable,
			wantArtifactStorage: DependencyAvailable,
		},
		{
			name:                 "artifact storage unavailable",
			artifactStorageError: errors.New("artifact storage unavailable"),
			wantDatabase:         DependencyAvailable,
			wantArtifactStorage:  DependencyUnavailable,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			service := NewService(
				&recordingDatabase{err: test.databaseError},
				&recordingArtifactStorage{err: test.artifactStorageError},
			)
			report := service.Check(context.Background())

			if report.Database != test.wantDatabase {
				t.Errorf("Database = %q, want %q", report.Database, test.wantDatabase)
			}
			if report.ArtifactStorage != test.wantArtifactStorage {
				t.Errorf("ArtifactStorage = %q, want %q", report.ArtifactStorage, test.wantArtifactStorage)
			}
			if report.Ready() != test.wantReady {
				t.Errorf("Ready() = %t, want %t", report.Ready(), test.wantReady)
			}
		})
	}
}

func TestServiceChecksDependenciesConcurrently(t *testing.T) {
	t.Parallel()

	started := make(chan struct{}, 2)
	release := make(chan struct{})
	service := NewService(
		&recordingDatabase{check: blockingCheck(started, release)},
		&recordingArtifactStorage{check: blockingCheck(started, release)},
	)
	result := make(chan Report, 1)

	go func() {
		result <- service.Check(context.Background())
	}()

	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("dependency checks did not run concurrently")
		}
	}
	close(release)

	if report := <-result; !report.Ready() {
		t.Errorf("report = %#v, want ready", report)
	}
}

func TestServiceBoundsDependencyChecks(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	service := NewService(
		&recordingDatabase{check: blockingCheck(nil, release)},
		&recordingArtifactStorage{check: blockingCheck(nil, release)},
	)
	service.timeout = 10 * time.Millisecond
	report := service.Check(context.Background())
	close(release)

	if report.Database != DependencyUnavailable ||
		report.ArtifactStorage != DependencyUnavailable {
		t.Errorf("report = %#v, want both dependencies unavailable", report)
	}
}

func TestServiceRecoversWhenDependenciesReturn(t *testing.T) {
	t.Parallel()

	database := &recordingDatabase{err: errors.New("database unavailable")}
	artifactStorage := &recordingArtifactStorage{
		err: errors.New("artifact storage unavailable"),
	}
	service := NewService(database, artifactStorage)

	if report := service.Check(context.Background()); report.Ready() {
		t.Errorf("initial report = %#v, want not ready", report)
	}

	database.err = nil
	artifactStorage.err = nil
	if report := service.Check(context.Background()); !report.Ready() {
		t.Errorf("recovered report = %#v, want ready", report)
	}
}

type recordingDatabase struct {
	err   error
	check func(context.Context) error
}

func (d *recordingDatabase) Ping(ctx context.Context) error {
	if d.check != nil {
		return d.check(ctx)
	}

	return d.err
}

type recordingArtifactStorage struct {
	err   error
	check func(context.Context) error
}

func (s *recordingArtifactStorage) Check(ctx context.Context) error {
	if s.check != nil {
		return s.check(ctx)
	}

	return s.err
}

func blockingCheck(started chan<- struct{}, release <-chan struct{}) func(context.Context) error {
	return func(context.Context) error {
		if started != nil {
			started <- struct{}{}
		}
		<-release

		return nil
	}
}
