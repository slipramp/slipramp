package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/repository"
)

type CommitService interface {
	ListCommits(context.Context, []repository.CommitKey) (map[repository.CommitKey]repository.Commit, error)
	ListCommitHistory(
		context.Context,
		repository.CommitHistoryFilter,
	) (repository.CommitHistoryPage, error)
}

func (h *Handler) ListCommits(
	ctx context.Context,
	request api.ListCommitsRequestObject,
) (api.ListCommitsResponseObject, error) {
	limit, offset, windowErr := listWindow(request.Params.Limit, request.Params.Offset)
	if windowErr != nil {
		return api.ListCommits400ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusBadRequest, windowErr.Error()),
		), nil
	}

	var query *string
	if request.Params.Q != nil {
		trimmed := strings.TrimSpace(*request.Params.Q)
		if trimmed != "" {
			query = &trimmed
		}
	}
	page, err := h.commits.ListCommitHistory(
		ctx,
		repository.CommitHistoryFilter{
			RepositoryID: request.Params.RepositoryID,
			Query:        query,
			Limit:        limit,
			Offset:       offset,
		},
	)
	if err != nil {
		return nil, err
	}

	items := make([]api.RepositoryCommit, 0, len(page.Commits))
	for _, item := range page.Commits {
		items = append(items, api.RepositoryCommit{
			RepositoryID:   item.RepositoryID,
			RepositoryName: item.RepositoryName,
			Commit:         commitResponse(item.Commit),
		})
	}

	return api.ListCommits200JSONResponse{
		Items:  items,
		Total:  page.Total,
		Limit:  limit,
		Offset: offset,
	}, nil
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
		commit.RepositoryID = key.RepositoryID
		commit.SHA = key.SHA
		responses[key] = commitResponse(commit)
	}

	return responses, nil
}

func commitResponse(commit repository.Commit) api.GitCommit {
	return api.GitCommit{
		Sha:         commit.SHA,
		Title:       commit.Title,
		Author:      commit.AuthorName,
		AuthoredAt:  commit.AuthoredAt,
		CommittedAt: commit.CommittedAt,
		URL:         commit.URL,
	}
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
