package store

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/repository"
)

func successfulCatalogRefresh(commitSHA string, attemptedAt time.Time, revisionID uuid.UUID) repository.RefreshResult {
	return repository.RefreshResult{
		Commit:      &repository.Commit{SHA: commitSHA},
		AttemptedAt: attemptedAt,
		Revision: &catalog.Revision{
			ID:        revisionID,
			CommitSHA: commitSHA,
			CreatedAt: attemptedAt,
		},
	}
}

func persistedCatalog(imageVersion string) catalog.Catalog {
	return catalog.Catalog{
		Runtimes: []catalog.Runtime{{
			APIVersion: catalog.APIVersion,
			Kind:       catalog.RuntimeKind,
			Metadata:   catalog.Metadata{Name: "terraform"},
			Spec: catalog.RuntimeSpec{
				Image: "hashicorp/terraform:" + imageVersion,
			},
			Source: catalog.Source{
				Path:     ".slipramp/catalog.yaml",
				Document: 1,
			},
		}},
		Targets: []catalog.Target{{
			APIVersion: catalog.APIVersion,
			Kind:       catalog.TargetKind,
			Metadata:   catalog.Metadata{Name: "network"},
			Spec: catalog.TargetSpec{
				Runtime:   "terraform",
				Placement: "infrastructure",
				Watch:     []string{"infrastructure/network/**"},
				Actions: map[string]catalog.Action{
					"preview": {Command: []string{"terraform", "plan"}},
				},
			},
			Source: catalog.Source{
				Path:     ".slipramp/catalog.yaml",
				Document: 2,
			},
		}},
	}
}

func assertActiveCatalog(
	t *testing.T,
	database *Database,
	repositoryID uuid.UUID,
	wantRevisionID uuid.UUID,
	wantCommitSHA string,
) {
	t.Helper()

	var (
		revisionID pgtype.UUID
		commitSHA  string
	)
	err := database.pool.QueryRow(
		t.Context(),
		`SELECT r.active_catalog_revision_id, c.commit_sha
         FROM repositories r
         JOIN catalog_revisions c ON c.id = r.active_catalog_revision_id
         WHERE r.id = $1`,
		repositoryID,
	).Scan(&revisionID, &commitSHA)
	if err != nil {
		t.Fatalf("load active catalog: %v", err)
	}
	if uuid.UUID(revisionID.Bytes) != wantRevisionID || commitSHA != wantCommitSHA {
		t.Errorf(
			"active catalog = (%s, %q), want (%s, %q)",
			uuid.UUID(revisionID.Bytes),
			commitSHA,
			wantRevisionID,
			wantCommitSHA,
		)
	}
}
