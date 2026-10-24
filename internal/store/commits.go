package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/slipramp/slipramp/internal/repository"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

var _ repository.CommitReader = (*Database)(nil)

func (d *Database) GetCommit(
	ctx context.Context,
	key repository.CommitKey,
) (repository.IndexedCommit, error) {
	row, err := d.queries.GetRepositoryCommit(
		ctx,
		storedb.GetRepositoryCommitParams{
			RepositoryID: databaseUUID(key.RepositoryID),
			Sha:          key.SHA,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return repository.IndexedCommit{}, repository.ErrCommitNotFound
	}
	if err != nil {
		return repository.IndexedCommit{}, fmt.Errorf("get repository commit: %w", err)
	}

	cfg := repository.Config{
		CloneURL:          row.CloneUrl,
		CommitURLTemplate: row.CommitUrlTemplate,
	}

	return repository.IndexedCommit{
		RepositoryID:   row.RepositoryID.Bytes,
		RepositoryName: row.RepositoryName,
		Commit: repository.Commit{
			RepositoryID: row.RepositoryID.Bytes,
			SHA:          row.Sha,
			Title:        row.Title,
			AuthorName:   row.AuthorName,
			AuthoredAt:   applicationTime(row.AuthoredAt),
			CommittedAt:  applicationTime(row.CommittedAt),
			URL:          repository.CommitURL(cfg, row.Sha),
		},
	}, nil
}

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
			CommittedAt:  applicationTime(row.CommittedAt),
			URL:          repository.CommitURL(cfg, row.Sha),
		}
	}

	return commits, nil
}

func (d *Database) ListCommitHistory(
	ctx context.Context,
	filter repository.CommitHistoryFilter,
) (repository.CommitHistoryPage, error) {
	var repositoryID pgtype.UUID
	if filter.RepositoryID != nil {
		repositoryID = databaseUUID(*filter.RepositoryID)
	}

	params := storedb.ListTrackedBranchCommitsParams{
		RepositoryID: repositoryID,
		Query:        filter.Query,
		ResultLimit:  int32(filter.Limit),
		ResultOffset: int32(filter.Offset),
	}
	rows, err := d.queries.ListTrackedBranchCommits(ctx, params)
	if err != nil {
		return repository.CommitHistoryPage{}, fmt.Errorf("list tracked branch commits: %w", err)
	}
	total, err := d.queries.CountTrackedBranchCommits(
		ctx,
		storedb.CountTrackedBranchCommitsParams{
			RepositoryID: repositoryID,
			Query:        filter.Query,
		},
	)
	if err != nil {
		return repository.CommitHistoryPage{}, fmt.Errorf("count tracked branch commits: %w", err)
	}

	commits := make([]repository.IndexedCommit, 0, len(rows))
	for _, row := range rows {
		cfg := repository.Config{
			CloneURL:          row.CloneUrl,
			CommitURLTemplate: row.CommitUrlTemplate,
		}
		commits = append(commits, repository.IndexedCommit{
			RepositoryID:   row.RepositoryID.Bytes,
			RepositoryName: row.RepositoryName,
			Commit: repository.Commit{
				RepositoryID: row.RepositoryID.Bytes,
				SHA:          row.Sha,
				Title:        row.Title,
				AuthorName:   row.AuthorName,
				AuthoredAt:   applicationTime(row.AuthoredAt),
				CommittedAt:  applicationTime(row.CommittedAt),
				URL:          repository.CommitURL(cfg, row.Sha),
			},
		})
	}

	return repository.CommitHistoryPage{Commits: commits, Total: int(total)}, nil
}
