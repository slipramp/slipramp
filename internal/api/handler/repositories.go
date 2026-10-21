package handler

import (
	"context"
	"errors"
	"net/http"

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
	_ api.ListRepositoriesRequestObject,
) (api.ListRepositoriesResponseObject, error) {
	repositories, err := h.repositories.ListRepositories(ctx)
	if err != nil {
		return nil, err
	}

	items := make([]api.Repository, 0, len(repositories))
	for _, item := range repositories {
		items = append(items, repositoryResponse(item))
	}

	return api.ListRepositories200JSONResponse{Items: items}, nil
}

func (h *Handler) CreateRepository(
	ctx context.Context,
	request api.CreateRepositoryRequestObject,
) (api.CreateRepositoryResponseObject, error) {
	cfg := repository.Config{
		Name:                   request.Body.Name,
		CloneURL:               request.Body.CloneURL,
		Branch:                 request.Body.Branch,
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
		Body: repositoryResponse(created),
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

func repositoryResponse(item repository.Repository) api.Repository {
	return api.Repository{
		ID:                     item.ID,
		Name:                   item.Name,
		CloneURL:               item.CloneURL,
		Branch:                 item.Branch,
		RefreshCredentialName:  item.RefreshCredentialName,
		CheckoutCredentialName: item.CheckoutCredentialName,
		CreatedAt:              item.CreatedAt,
		Refresh: api.RepositoryRefresh{
			Status:        api.RepositoryRefreshStatus(item.Refresh.Status),
			CommitSha:     item.Refresh.CommitSHA,
			AttemptedAt:   item.Refresh.AttemptedAt,
			SucceededAt:   item.Refresh.SucceededAt,
			FailureReason: item.Refresh.FailureReason,
		},
	}
}
