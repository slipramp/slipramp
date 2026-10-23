package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/slipramp/slipramp/internal/repository"
	"github.com/slipramp/slipramp/internal/store/storedb"
)

var _ repository.CommitHistoryStore = (*Database)(nil)

func (d *Database) RecordCommitHistory(
	ctx context.Context,
	repositoryID repository.RepositoryID,
	result repository.CommitHistoryResult,
) error {
	if err := repository.ValidateCommitHistoryResult(result); err != nil {
		return fmt.Errorf("validate repository commit history: %w", err)
	}

	transaction, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin repository commit history transaction: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
	}()

	queries := d.queries.WithTx(transaction)
	identifier := databaseUUID(repositoryID)
	state, err := queries.LockRepositoryCommitHistory(ctx, identifier)
	if errors.Is(err, pgx.ErrNoRows) {
		return repository.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock repository commit history: %w", err)
	}
	if repository.CommitIndexingMode(state.CommitIndexingMode) != repository.CommitIndexingTrackedBranchHistory {
		return nil
	}

	commitSHAs := make([]string, 0, len(result.Commits))
	if len(result.Commits) > 0 {
		titles := make([]string, 0, len(result.Commits))
		authorNames := make([]string, 0, len(result.Commits))
		authoredAts := make([]pgtype.Timestamptz, 0, len(result.Commits))
		committedAts := make([]pgtype.Timestamptz, 0, len(result.Commits))
		for _, commit := range result.Commits {
			if commit.RepositoryID != uuid.Nil && commit.RepositoryID != repositoryID {
				return errors.New("commit history repository does not match indexing repository")
			}
			commitSHAs = append(commitSHAs, commit.SHA)
			titles = append(titles, *commit.Title)
			authorNames = append(authorNames, *commit.AuthorName)
			authoredAts = append(authoredAts, databaseTime(*commit.AuthoredAt))
			committedAts = append(committedAts, databaseTime(*commit.CommittedAt))
		}

		err = queries.UpsertRepositoryCommitHistory(
			ctx,
			storedb.UpsertRepositoryCommitHistoryParams{
				RepositoryID: identifier,
				Shas:         commitSHAs,
				Titles:       titles,
				AuthorNames:  authorNames,
				AuthoredAts:  authoredAts,
				CommittedAts: committedAts,
			},
		)
		if err != nil {
			return fmt.Errorf("record repository commit history metadata: %w", err)
		}
	}

	if result.Replace {
		if err := queries.DeleteTrackedBranchCommits(ctx, identifier); err != nil {
			return fmt.Errorf("replace tracked branch commit history: %w", err)
		}
	}
	if len(commitSHAs) > 0 {
		if err := queries.AddTrackedBranchCommits(
			ctx,
			storedb.AddTrackedBranchCommitsParams{
				RepositoryID: identifier,
				CommitShas:   commitSHAs,
			},
		); err != nil {
			return fmt.Errorf("record tracked branch commits: %w", err)
		}
	}

	if err := queries.FinishRepositoryCommitHistory(
		ctx,
		storedb.FinishRepositoryCommitHistoryParams{
			IndexedHeadSha: &result.HeadSHA,
			RepositoryID:   identifier,
			AttemptedAt:    databaseTime(result.AttemptedAt),
		},
	); err != nil {
		return fmt.Errorf("finish repository commit history: %w", err)
	}

	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit repository commit history: %w", err)
	}

	return nil
}

func (d *Database) RecordCommitHistoryFailure(
	ctx context.Context,
	repositoryID repository.RepositoryID,
	attemptedHeadSHA string,
	attemptedAt time.Time,
	failureReason string,
) error {
	if err := repository.ValidateCommitHistoryFailure(attemptedHeadSHA, attemptedAt, failureReason); err != nil {
		return fmt.Errorf("validate repository commit history failure: %w", err)
	}

	err := d.queries.FailRepositoryCommitHistory(
		ctx,
		storedb.FailRepositoryCommitHistoryParams{
			AttemptedAt:      databaseTime(attemptedAt),
			FailureReason:    &failureReason,
			RepositoryID:     databaseUUID(repositoryID),
			AttemptedHeadSha: &attemptedHeadSHA,
		},
	)
	if err != nil {
		return fmt.Errorf("record repository commit history failure: %w", err)
	}

	return nil
}
