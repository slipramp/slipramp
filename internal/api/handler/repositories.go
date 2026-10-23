package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/repository"
)

type RepositoryService interface {
	CreateRepository(ctx context.Context, cfg repository.Config) (repository.Repository, error)
	ListRepositories(ctx context.Context) ([]repository.Repository, error)
	RemoveRepository(ctx context.Context, repositoryID uuid.UUID) error
}

func (h *Handler) ListRepositories(
	ctx context.Context,
	request api.ListRepositoriesRequestObject,
) (api.ListRepositoriesResponseObject, error) {
	limit, offset, windowErr := listWindow(request.Params.Limit, request.Params.Offset)
	if windowErr != nil {
		return api.ListRepositories400ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusBadRequest, windowErr.Error()),
		), nil
	}
	repositories, err := h.repositories.ListRepositories(ctx)
	if err != nil {
		return nil, err
	}

	query := ""
	if request.Params.Q != nil {
		query = strings.ToLower(strings.TrimSpace(*request.Params.Q))
	}
	refreshStatus := ""
	if request.Params.RefreshStatus != nil {
		refreshStatus = string(*request.Params.RefreshStatus)
	}
	filtered := repositories[:0]
	for _, item := range repositories {
		searchable := strings.ToLower(strings.Join([]string{item.Name, item.CloneURL, item.Branch}, "\n"))
		if (query == "" || strings.Contains(searchable, query)) &&
			(refreshStatus == "" || string(item.Refresh.Status) == refreshStatus) {
			filtered = append(filtered, item)
		}
	}
	total := len(filtered)
	filtered = listPage(filtered, offset, limit)

	keys := make([]repository.CommitKey, 0, len(filtered))
	for _, item := range filtered {
		if item.Refresh.CommitSHA != nil {
			keys = append(keys, commitKey(item.ID, *item.Refresh.CommitSHA))
		}
	}
	commits, err := h.commitResponses(ctx, keys)
	if err != nil {
		return nil, err
	}

	items := make([]api.Repository, 0, len(filtered))
	for _, item := range filtered {
		items = append(items, repositoryResponse(item, repositoryRefreshCommit(item, commits)))
	}

	return api.ListRepositories200JSONResponse{Items: items, Total: total, Limit: limit, Offset: offset}, nil
}

func (h *Handler) CreateRepository(
	ctx context.Context,
	request api.CreateRepositoryRequestObject,
) (api.CreateRepositoryResponseObject, error) {
	cfg := repository.Config{
		Name:                   request.Body.Name,
		CloneURL:               request.Body.CloneURL,
		Branch:                 request.Body.Branch,
		CommitURLTemplate:      request.Body.CommitURLTemplate,
		RefreshCredentialName:  request.Body.RefreshCredentialName,
		CheckoutCredentialName: request.Body.CheckoutCredentialName,
	}

	created, err := h.repositories.CreateRepository(ctx, cfg)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrInvalidConfig):
			problem := NewProblem(http.StatusBadRequest, err.Error())

			return api.CreateRepository400ApplicationProblemPlusJSONResponse(problem), nil
		case errors.Is(err, repository.ErrNameConflict):
			problem := NewProblem(http.StatusConflict, "An active repository already uses that name.")

			return api.CreateRepository409ApplicationProblemPlusJSONResponse(problem), nil
		default:
			return nil, err
		}
	}

	location := "/api/v1/repositories/" + created.ID.String()

	return api.CreateRepository201JSONResponse{
		Body: repositoryResponse(created, nil),
		Headers: api.CreateRepository201ResponseHeaders{
			Location: &location,
		},
	}, nil
}

func (h *Handler) RemoveRepository(
	ctx context.Context,
	request api.RemoveRepositoryRequestObject,
) (api.RemoveRepositoryResponseObject, error) {
	err := h.repositories.RemoveRepository(ctx, request.RepositoryID)
	if errors.Is(err, repository.ErrNotFound) {
		problem := NewProblem(http.StatusNotFound, "The active repository does not exist.")

		return api.RemoveRepository404ApplicationProblemPlusJSONResponse(problem), nil
	}

	if err != nil {
		return nil, err
	}

	return api.RemoveRepository204Response{}, nil
}

func repositoryResponse(item repository.Repository, commit *api.GitCommit) api.Repository {
	return api.Repository{
		ID:                     item.ID,
		Name:                   item.Name,
		CloneURL:               item.CloneURL,
		Branch:                 item.Branch,
		CommitURLTemplate:      item.CommitURLTemplate,
		RefreshCredentialName:  item.RefreshCredentialName,
		CheckoutCredentialName: item.CheckoutCredentialName,
		CreatedAt:              item.CreatedAt,
		Refresh: api.RepositoryRefresh{
			Status:        api.RepositoryRefreshStatus(item.Refresh.Status),
			Commit:        commit,
			AttemptedAt:   item.Refresh.AttemptedAt,
			SucceededAt:   item.Refresh.SucceededAt,
			FailureReason: item.Refresh.FailureReason,
		},
	}
}

func repositoryRefreshCommit(
	item repository.Repository,
	commits map[repository.CommitKey]api.GitCommit,
) *api.GitCommit {
	if item.Refresh.CommitSHA == nil {
		return nil
	}

	commit := commits[commitKey(item.ID, *item.Refresh.CommitSHA)]

	return &commit
}
