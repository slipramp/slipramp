package handler

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/repository"
)

func TestCommitResponsesEnrichKnownCommitsAndKeepFallbacks(t *testing.T) {
	t.Parallel()

	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	knownKey := repository.CommitKey{
		RepositoryID: repositoryID,
		SHA:          "0123456789abcdef0123456789abcdef01234567",
	}
	unknownKey := repository.CommitKey{
		RepositoryID: repositoryID,
		SHA:          "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	title := "Improve commit references"
	author := "Slipramp Test"
	authoredAt := time.Date(2026, time.October, 22, 18, 0, 0, 0, time.UTC)
	url := "https://github.com/example/ops/commit/" + knownKey.SHA
	handler := Handler{commits: staticCommitService{
		knownKey: {
			RepositoryID: knownKey.RepositoryID,
			SHA:          knownKey.SHA,
			Title:        &title,
			AuthorName:   &author,
			AuthoredAt:   &authoredAt,
			URL:          &url,
		},
	}}

	commits, err := handler.commitResponses(t.Context(), []repository.CommitKey{knownKey, unknownKey})
	if err != nil {
		t.Fatalf("commitResponses() error = %v", err)
	}
	if commits[knownKey].Title == nil || *commits[knownKey].Title != title ||
		commits[knownKey].Author == nil || *commits[knownKey].Author != author ||
		commits[knownKey].AuthoredAt == nil || !commits[knownKey].AuthoredAt.Equal(authoredAt) ||
		commits[knownKey].URL == nil || *commits[knownKey].URL != url {
		t.Errorf("known commit = %#v", commits[knownKey])
	}
	if commits[unknownKey].Sha != unknownKey.SHA || commits[unknownKey].Title != nil {
		t.Errorf("unknown commit = %#v", commits[unknownKey])
	}
}

type staticCommitService map[repository.CommitKey]repository.Commit

func (s staticCommitService) ListCommits(
	_ context.Context,
	_ []repository.CommitKey,
) (map[repository.CommitKey]repository.Commit, error) {
	return s, nil
}
