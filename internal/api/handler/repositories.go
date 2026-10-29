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
	TestConnection(
		ctx context.Context,
		cfg repository.ConnectionConfig,
	) (repository.ConnectionTestResult, error)
	ListRepositories(ctx context.Context) ([]repository.Repository, error)
	RequestRefresh(ctx context.Context, repositoryID repository.RepositoryID) (repository.RefreshAttempt, error)
	ListRefreshAttempts(
		ctx context.Context,
		repositoryID repository.RepositoryID,
		limit int,
		offset int,
	) (repository.RefreshAttemptPage, error)
	UpdateRepository(
		ctx context.Context,
		repositoryID uuid.UUID,
		cfg repository.Config,
	) (repository.Repository, error)
	RemoveRepository(ctx context.Context, repositoryID uuid.UUID) error
}

func (h *Handler) TestRepositoryConnection(
	ctx context.Context,
	request api.TestRepositoryConnectionRequestObject,
) (api.TestRepositoryConnectionResponseObject, error) {
	result, err := h.repositories.TestConnection(ctx, repository.ConnectionConfig{
		CloneURL:              request.Body.CloneURL,
		Branch:                request.Body.Branch,
		RefreshCredentialName: request.Body.RefreshCredentialName,
	})
	if errors.Is(err, repository.ErrInvalidConfig) {
		return api.TestRepositoryConnection400ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusBadRequest, err.Error()),
		), nil
	}
	if err != nil {
		return nil, err
	}

	return api.TestRepositoryConnection200JSONResponse{
		Status:               api.RepositoryConnectionTestResultStatus(result.Status),
		DurationMilliseconds: result.Duration.Milliseconds(),
		CommitSha:            result.CommitSHA,
		Diagnostic:           result.Diagnostic,
	}, nil
}

func (h *Handler) ListRepositoryRefreshAttempts(
	ctx context.Context,
	request api.ListRepositoryRefreshAttemptsRequestObject,
) (api.ListRepositoryRefreshAttemptsResponseObject, error) {
	limit, offset, windowErr := listWindow(request.Params.Limit, request.Params.Offset)
	if windowErr != nil {
		return api.ListRepositoryRefreshAttempts400ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusBadRequest, windowErr.Error()),
		), nil
	}

	page, err := h.repositories.ListRefreshAttempts(ctx, request.RepositoryID, limit, offset)
	if errors.Is(err, repository.ErrNotFound) {
		return api.ListRepositoryRefreshAttempts404ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusNotFound, "The active repository does not exist."),
		), nil
	}
	if err != nil {
		return nil, err
	}

	items, err := h.repositoryRefreshAttemptResponses(ctx, page.Items)
	if err != nil {
		return nil, err
	}

	return api.ListRepositoryRefreshAttempts200JSONResponse{
		Items:  items,
		Total:  page.Total,
		Limit:  limit,
		Offset: offset,
	}, nil
}

func (h *Handler) RefreshRepository(
	ctx context.Context,
	request api.RefreshRepositoryRequestObject,
) (api.RefreshRepositoryResponseObject, error) {
	attempt, err := h.repositories.RequestRefresh(ctx, request.RepositoryID)
	if errors.Is(err, repository.ErrNotFound) {
		return api.RefreshRepository404ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusNotFound, "The active repository does not exist."),
		), nil
	}
	if err != nil {
		return nil, err
	}

	items, err := h.repositoryRefreshAttemptResponses(ctx, []repository.RefreshAttempt{attempt})
	if err != nil {
		return nil, err
	}
	location := "/api/v1/repositories/" + request.RepositoryID.String() + "/refresh-attempts"

	return api.RefreshRepository201JSONResponse{
		Body: items[0],
		Headers: api.RefreshRepository201ResponseHeaders{
			Location: &location,
		},
	}, nil
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
		CommitIndexing:         repository.CommitIndexingMode(request.Body.CommitIndexing),
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

func (h *Handler) UpdateRepository(
	ctx context.Context,
	request api.UpdateRepositoryRequestObject,
) (api.UpdateRepositoryResponseObject, error) {
	updated, err := h.repositories.UpdateRepository(
		ctx,
		request.RepositoryID,
		repository.Config{
			CommitIndexing:         repository.CommitIndexingMode(request.Body.CommitIndexing),
			CommitURLTemplate:      request.Body.CommitURLTemplate,
			RefreshCredentialName:  request.Body.RefreshCredentialName,
			CheckoutCredentialName: request.Body.CheckoutCredentialName,
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrInvalidConfig):
			return api.UpdateRepository400ApplicationProblemPlusJSONResponse(
				NewProblem(http.StatusBadRequest, err.Error()),
			), nil
		case errors.Is(err, repository.ErrNotFound):
			return api.UpdateRepository404ApplicationProblemPlusJSONResponse(
				NewProblem(http.StatusNotFound, "The active repository does not exist."),
			), nil
		default:
			return nil, err
		}
	}

	keys := make([]repository.CommitKey, 0, 1)
	if updated.Refresh.CommitSHA != nil {
		keys = append(keys, commitKey(updated.ID, *updated.Refresh.CommitSHA))
	}
	commits, err := h.commitResponses(ctx, keys)
	if err != nil {
		return nil, err
	}

	return api.UpdateRepository200JSONResponse(repositoryResponse(
		updated,
		repositoryRefreshCommit(updated, commits),
	)), nil
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
	var historyIndex *api.RepositoryCommitHistoryIndex
	if item.CommitHistoryIndex != nil {
		historyIndex = &api.RepositoryCommitHistoryIndex{
			Status:          api.RepositoryCommitHistoryIndexStatus(item.CommitHistoryIndex.Status),
			DesiredHeadSha:  item.CommitHistoryIndex.DesiredHeadSHA,
			IndexedHeadSha:  item.CommitHistoryIndex.IndexedHeadSHA,
			CommitCount:     item.CommitHistoryIndex.CommitCount,
			LastAttemptedAt: item.CommitHistoryIndex.LastAttemptedAt,
			LastSucceededAt: item.CommitHistoryIndex.LastSucceededAt,
			FailureReason:   item.CommitHistoryIndex.FailureReason,
		}
	}

	return api.Repository{
		ID:                     item.ID,
		Name:                   item.Name,
		CloneURL:               item.CloneURL,
		Branch:                 item.Branch,
		CommitIndexing:         api.RepositoryCommitIndexingMode(item.CommitIndexing),
		CommitURLTemplate:      item.CommitURLTemplate,
		RefreshCredentialName:  item.RefreshCredentialName,
		CheckoutCredentialName: item.CheckoutCredentialName,
		CreatedAt:              item.CreatedAt,
		CommitHistoryIndex:     historyIndex,
		Refresh: api.RepositoryRefresh{
			Status:        api.RepositoryRefreshStatus(item.Refresh.Status),
			Commit:        commit,
			AttemptedAt:   item.Refresh.AttemptedAt,
			SucceededAt:   item.Refresh.SucceededAt,
			FailureReason: item.Refresh.FailureReason,
		},
	}
}

func (h *Handler) repositoryRefreshAttemptResponses(
	ctx context.Context,
	attempts []repository.RefreshAttempt,
) ([]api.RepositoryRefreshAttempt, error) {
	keys := make([]repository.CommitKey, 0, len(attempts))
	for _, attempt := range attempts {
		if attempt.CommitSHA != nil {
			keys = append(keys, commitKey(attempt.RepositoryID, *attempt.CommitSHA))
		}
	}
	commits, err := h.commitResponses(ctx, keys)
	if err != nil {
		return nil, err
	}

	items := make([]api.RepositoryRefreshAttempt, 0, len(attempts))
	for _, attempt := range attempts {
		var commit *api.GitCommit
		if attempt.CommitSHA != nil {
			response := commits[commitKey(attempt.RepositoryID, *attempt.CommitSHA)]
			commit = &response
		}
		items = append(items, api.RepositoryRefreshAttempt{
			ID:                   attempt.ID,
			RepositoryID:         attempt.RepositoryID,
			Source:               api.RepositoryRefreshAttemptSource(attempt.Source),
			Outcome:              api.RepositoryRefreshAttemptOutcome(attempt.Outcome),
			StartedAt:            attempt.StartedAt,
			DurationMilliseconds: attempt.Duration.Milliseconds(),
			Commit:               commit,
			Diagnostic:           attempt.Diagnostic,
		})
	}

	return items, nil
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
