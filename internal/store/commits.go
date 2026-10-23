package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/slipramp/slipramp/internal/repository"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

var _ repository.CommitReader = (*Database)(nil)

func (d *Database) ListCommits(
	ctx context.Context,
	keys []repository.CommitKey,
) (map[repository.CommitKey]repository.Commit, error) {
	unique := make(map[repository.CommitKey]struct{}, len(keys))
	repositoryIDs := make([]pgtype.UUID, 0, len(keys))
	shas := make([]string, 0, len(keys))
	for _, key := range keys {
		if _, found := unique[key]; found {
			continue
		}
		unique[key] = struct{}{}
		repositoryIDs = append(repositoryIDs, databaseUUID(key.RepositoryID))
		shas = append(shas, key.SHA)
	}
	if len(repositoryIDs) == 0 {
		return map[repository.CommitKey]repository.Commit{}, nil
	}

	rows, err := d.queries.ListRepositoryCommits(
		ctx,
		storedb.ListRepositoryCommitsParams{
			RepositoryIds: repositoryIDs,
			CommitShas:    shas,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list repository commits: %w", err)
	}

	commits := make(map[repository.CommitKey]repository.Commit, len(rows))
	for _, row := range rows {
		key := repository.CommitKey{
			RepositoryID: row.RepositoryID.Bytes,
			SHA:          row.Sha,
		}
		cfg := repository.Config{
			CloneURL:          row.CloneUrl,
			CommitURLTemplate: row.CommitUrlTemplate,
		}
		commits[key] = repository.Commit{
			RepositoryID: key.RepositoryID,
			SHA:          key.SHA,
			Title:        row.Title,
			AuthorName:   row.AuthorName,
			AuthoredAt:   applicationTime(row.AuthoredAt),
			URL:          repository.CommitURL(cfg, row.Sha),
		}
	}

	return commits, nil
}
