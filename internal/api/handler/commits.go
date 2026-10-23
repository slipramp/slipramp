package handler

import (
	"context"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/repository"
)

type CommitService interface {
	ListCommits(context.Context, []repository.CommitKey) (map[repository.CommitKey]repository.Commit, error)
}

func (h *Handler) commitResponses(
	ctx context.Context,
	keys []repository.CommitKey,
) (map[repository.CommitKey]api.GitCommit, error) {
	commits := map[repository.CommitKey]repository.Commit{}
	if h.commits != nil && len(keys) > 0 {
		loaded, err := h.commits.ListCommits(ctx, keys)
		if err != nil {
			return nil, err
		}
		commits = loaded
	}

	responses := make(map[repository.CommitKey]api.GitCommit, len(keys))
	for _, key := range keys {
		commit := commits[key]
		responses[key] = api.GitCommit{
			Sha:        key.SHA,
			Title:      commit.Title,
			Author:     commit.AuthorName,
			AuthoredAt: commit.AuthoredAt,
			URL:        commit.URL,
		}
	}

	return responses, nil
}

func commitFallbacks(keys []repository.CommitKey) map[repository.CommitKey]api.GitCommit {
	responses := make(map[repository.CommitKey]api.GitCommit, len(keys))
	for _, key := range keys {
		responses[key] = api.GitCommit{Sha: key.SHA}
	}

	return responses
}

func commitKey(repositoryID repository.RepositoryID, sha string) repository.CommitKey {
	return repository.CommitKey{RepositoryID: repositoryID, SHA: sha}
}
