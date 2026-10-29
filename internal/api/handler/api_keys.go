package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/productauth"
	"github.com/slipramp/slipramp/internal/productidentity"
)

type AccessService interface {
	AuthenticateAPIKey(context.Context, string) (productidentity.Authentication, error)
	ListOwnAPIKeys(context.Context, productidentity.Authentication) ([]productidentity.APIKey, error)
	CreateUserAPIKey(
		context.Context,
		productidentity.Authentication,
		string,
		[]productidentity.Scope,
		*time.Time,
	) (productauth.APIKeyCredential, error)
	RevokeAPIKey(context.Context, productidentity.Authentication, uuid.UUID) error
	ListPrincipals(context.Context) ([]productidentity.Principal, error)
	GetPrincipal(context.Context, uuid.UUID) (productidentity.Principal, error)
	UpdatePrincipalDisabled(context.Context, uuid.UUID, bool) (productidentity.Principal, error)
	CreateServiceAccount(context.Context, string, productidentity.Role) (productidentity.Principal, error)
	ListPrincipalAPIKeys(context.Context, uuid.UUID) ([]productidentity.APIKey, error)
	CreatePrincipalAPIKey(
		context.Context,
		uuid.UUID,
		uuid.UUID,
		string,
		[]productidentity.Scope,
		*time.Time,
	) (productauth.APIKeyCredential, error)
	RevokePrincipalAPIKeys(context.Context, uuid.UUID) error
	RevokePrincipalSessions(context.Context, uuid.UUID) error
}

func (h *Handler) ListAPIKeys(
	ctx context.Context,
	request api.ListAPIKeysRequestObject,
) (api.ListAPIKeysResponseObject, error) {
	limit, offset, windowErr := listWindow(request.Params.Limit, request.Params.Offset)
	if windowErr != nil {
		return api.ListAPIKeys400ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusBadRequest, windowErr.Error()),
		), nil
	}
	authentication, err := currentProductAuthentication(ctx)
	if err != nil {
		return nil, err
	}

	keys, err := h.productAuth.ListOwnAPIKeys(ctx, authentication)
	if err != nil {
		return nil, err
	}

	total := len(keys)
	keys = listPage(keys, offset, limit)

	return api.ListAPIKeys200JSONResponse{
		Items: apiKeyResponses(keys), Total: total, Limit: limit, Offset: offset,
	}, nil
}

func (h *Handler) CreateAPIKey(
	ctx context.Context,
	request api.CreateAPIKeyRequestObject,
) (api.CreateAPIKeyResponseObject, error) {
	authentication, err := currentProductAuthentication(ctx)
	if err != nil {
		return nil, err
	}
	if request.Body == nil {
		problem := NewProblem(http.StatusBadRequest, "An API key configuration is required.")

		return api.CreateAPIKey400ApplicationProblemPlusJSONResponse(problem), nil
	}

	credential, err := h.productAuth.CreateUserAPIKey(
		ctx,
		authentication,
		request.Body.Name,
		productScopes(request.Body.Scopes),
		request.Body.ExpiresAt,
	)
	if err != nil {
		switch {
		case errors.Is(err, productauth.ErrForbidden):
			problem := NewProblem(
				http.StatusForbidden,
				"A browser-authenticated user is required to create a user API key.",
			)

			return api.CreateAPIKey403ApplicationProblemPlusJSONResponse(problem), nil
		case errors.Is(err, productidentity.ErrInvalid):
			problem := NewProblem(http.StatusBadRequest, err.Error())

			return api.CreateAPIKey400ApplicationProblemPlusJSONResponse(problem), nil
		default:
			return nil, err
		}
	}

	return api.CreateAPIKey201JSONResponse{
		Body: apiKeyCredentialResponse(credential),
		Headers: api.CreateAPIKey201ResponseHeaders{
			Location: fmt.Sprintf("/api/v1/auth/api-keys/%s", credential.Key.ID),
		},
	}, nil
}

func (h *Handler) RevokeAPIKey(
	ctx context.Context,
	request api.RevokeAPIKeyRequestObject,
) (api.RevokeAPIKeyResponseObject, error) {
	authentication, err := currentProductAuthentication(ctx)
	if err != nil {
		return nil, err
	}

	err = h.productAuth.RevokeAPIKey(ctx, authentication, request.APIKeyID)
	if err != nil {
		switch {
		case errors.Is(err, productauth.ErrForbidden):
			problem := NewProblem(http.StatusForbidden, "The principal cannot revoke this API key.")

			return api.RevokeAPIKey403ApplicationProblemPlusJSONResponse(problem), nil
		case errors.Is(err, productidentity.ErrCredentialNotFound):
			problem := NewProblem(http.StatusNotFound, "The API key does not exist.")

			return api.RevokeAPIKey404ApplicationProblemPlusJSONResponse(problem), nil
		case errors.Is(err, productidentity.ErrInvalid):
			problem := NewProblem(http.StatusBadRequest, err.Error())

			return api.RevokeAPIKey400ApplicationProblemPlusJSONResponse(problem), nil
		default:
			return nil, err
		}
	}

	return api.RevokeAPIKey204Response{}, nil
}

func (h *Handler) ListPrincipals(
	ctx context.Context,
	request api.ListPrincipalsRequestObject,
) (api.ListPrincipalsResponseObject, error) {
	limit, offset, windowErr := listWindow(request.Params.Limit, request.Params.Offset)
	if windowErr != nil {
		return api.ListPrincipals400ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusBadRequest, windowErr.Error()),
		), nil
	}
	principals, err := h.productAuth.ListPrincipals(ctx)
	if err != nil {
		return nil, err
	}

	total := len(principals)
	principals = listPage(principals, offset, limit)
	items := make([]api.Principal, 0, len(principals))
	for _, principal := range principals {
		items = append(items, principalResponse(principal))
	}

	return api.ListPrincipals200JSONResponse{Items: items, Total: total, Limit: limit, Offset: offset}, nil
}

func (h *Handler) GetPrincipal(
	ctx context.Context,
	request api.GetPrincipalRequestObject,
) (api.GetPrincipalResponseObject, error) {
	principal, err := h.productAuth.GetPrincipal(ctx, request.PrincipalID)
	if errors.Is(err, productidentity.ErrPrincipalNotFound) {
		return api.GetPrincipal404ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusNotFound, "The principal does not exist."),
		), nil
	}
	if errors.Is(err, productidentity.ErrInvalid) {
		return api.GetPrincipal404ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusNotFound, "The principal does not exist."),
		), nil
	}
	if err != nil {
		return nil, err
	}

	return api.GetPrincipal200JSONResponse(principalResponse(principal)), nil
}

func (h *Handler) UpdatePrincipal(
	ctx context.Context,
	request api.UpdatePrincipalRequestObject,
) (api.UpdatePrincipalResponseObject, error) {
	if request.Body == nil {
		return api.UpdatePrincipal400ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusBadRequest, "A principal state update is required."),
		), nil
	}

	principal, err := h.productAuth.UpdatePrincipalDisabled(ctx, request.PrincipalID, request.Body.Disabled)
	if errors.Is(err, productidentity.ErrPrincipalNotFound) {
		return api.UpdatePrincipal404ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusNotFound, "The principal does not exist."),
		), nil
	}
	if errors.Is(err, productidentity.ErrInvalid) {
		return api.UpdatePrincipal400ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusBadRequest, err.Error()),
		), nil
	}
	if err != nil {
		return nil, err
	}

	return api.UpdatePrincipal200JSONResponse(principalResponse(principal)), nil
}

func (h *Handler) CreateServiceAccount(
	ctx context.Context,
	request api.CreateServiceAccountRequestObject,
) (api.CreateServiceAccountResponseObject, error) {
	if request.Body == nil {
		problem := NewProblem(http.StatusBadRequest, "A service-account configuration is required.")

		return api.CreateServiceAccount400ApplicationProblemPlusJSONResponse(problem), nil
	}

	principal, err := h.productAuth.CreateServiceAccount(
		ctx,
		request.Body.DisplayName,
		productidentity.Role(request.Body.Role),
	)
	if errors.Is(err, productidentity.ErrInvalid) {
		problem := NewProblem(http.StatusBadRequest, err.Error())

		return api.CreateServiceAccount400ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}

	return api.CreateServiceAccount201JSONResponse(principalResponse(principal)), nil
}

func (h *Handler) ListPrincipalAPIKeys(
	ctx context.Context,
	request api.ListPrincipalAPIKeysRequestObject,
) (api.ListPrincipalAPIKeysResponseObject, error) {
	limit, offset, windowErr := listWindow(request.Params.Limit, request.Params.Offset)
	if windowErr != nil {
		return api.ListPrincipalAPIKeys400ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusBadRequest, windowErr.Error()),
		), nil
	}
	keys, err := h.productAuth.ListPrincipalAPIKeys(ctx, request.PrincipalID)
	if errors.Is(err, productidentity.ErrPrincipalNotFound) {
		problem := NewProblem(http.StatusNotFound, "The principal does not exist.")

		return api.ListPrincipalAPIKeys404ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, productidentity.ErrInvalid) {
		problem := NewProblem(http.StatusBadRequest, err.Error())

		return api.ListPrincipalAPIKeys400ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}

	total := len(keys)
	keys = listPage(keys, offset, limit)

	return api.ListPrincipalAPIKeys200JSONResponse{
		Items: apiKeyResponses(keys), Total: total, Limit: limit, Offset: offset,
	}, nil
}

func (h *Handler) CreatePrincipalAPIKey(
	ctx context.Context,
	request api.CreatePrincipalAPIKeyRequestObject,
) (api.CreatePrincipalAPIKeyResponseObject, error) {
	if request.Body == nil {
		problem := NewProblem(http.StatusBadRequest, "An API key configuration is required.")

		return api.CreatePrincipalAPIKey400ApplicationProblemPlusJSONResponse(problem), nil
	}
	authentication, err := currentProductAuthentication(ctx)
	if err != nil {
		return nil, err
	}

	credential, err := h.productAuth.CreatePrincipalAPIKey(
		ctx,
		request.PrincipalID,
		authentication.Principal.ID,
		request.Body.Name,
		productScopes(request.Body.Scopes),
		request.Body.ExpiresAt,
	)
	if errors.Is(err, productidentity.ErrPrincipalNotFound) {
		problem := NewProblem(http.StatusNotFound, "The principal does not exist.")

		return api.CreatePrincipalAPIKey404ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, productauth.ErrForbidden) {
		problem := NewProblem(http.StatusForbidden, "API keys can be created only for service accounts.")

		return api.CreatePrincipalAPIKey403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, productidentity.ErrInvalid) {
		problem := NewProblem(http.StatusBadRequest, err.Error())

		return api.CreatePrincipalAPIKey400ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}

	return api.CreatePrincipalAPIKey201JSONResponse{
		Body: apiKeyCredentialResponse(credential),
		Headers: api.CreatePrincipalAPIKey201ResponseHeaders{
			Location: fmt.Sprintf("/api/v1/auth/api-keys/%s", credential.Key.ID),
		},
	}, nil
}

func (h *Handler) RevokePrincipalAPIKeys(
	ctx context.Context,
	request api.RevokePrincipalAPIKeysRequestObject,
) (api.RevokePrincipalAPIKeysResponseObject, error) {
	err := h.productAuth.RevokePrincipalAPIKeys(ctx, request.PrincipalID)
	if errors.Is(err, productidentity.ErrPrincipalNotFound) {
		return api.RevokePrincipalAPIKeys404ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusNotFound, "The principal does not exist."),
		), nil
	}
	if errors.Is(err, productidentity.ErrInvalid) {
		return api.RevokePrincipalAPIKeys400ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusBadRequest, err.Error()),
		), nil
	}
	if err != nil {
		return nil, err
	}

	return api.RevokePrincipalAPIKeys204Response{}, nil
}

func (h *Handler) RevokePrincipalSessions(
	ctx context.Context,
	request api.RevokePrincipalSessionsRequestObject,
) (api.RevokePrincipalSessionsResponseObject, error) {
	err := h.productAuth.RevokePrincipalSessions(ctx, request.PrincipalID)
	if errors.Is(err, productidentity.ErrPrincipalNotFound) {
		return api.RevokePrincipalSessions404ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusNotFound, "The principal does not exist."),
		), nil
	}
	if errors.Is(err, productidentity.ErrInvalid) {
		return api.RevokePrincipalSessions400ApplicationProblemPlusJSONResponse(
			NewProblem(http.StatusBadRequest, err.Error()),
		), nil
	}
	if err != nil {
		return nil, err
	}

	return api.RevokePrincipalSessions204Response{}, nil
}

func apiKeyResponses(keys []productidentity.APIKey) []api.APIKey {
	items := make([]api.APIKey, 0, len(keys))
	for _, key := range keys {
		items = append(items, apiKeyResponse(key))
	}

	return items
}

func apiKeyResponse(key productidentity.APIKey) api.APIKey {
	return api.APIKey{
		ID:          key.ID,
		PrincipalID: key.PrincipalID,
		Name:        key.Name,
		TokenPrefix: key.TokenPrefix,
		Scopes:      apiScopes(key.Scopes),
		CreatedAt:   key.CreatedAt,
		ExpiresAt:   cloneResponseTime(key.ExpiresAt),
		LastUsedAt:  cloneResponseTime(key.LastUsedAt),
		RevokedAt:   cloneResponseTime(key.RevokedAt),
	}
}

func apiKeyCredentialResponse(credential productauth.APIKeyCredential) api.APIKeyCredential {
	key := apiKeyResponse(credential.Key)

	return api.APIKeyCredential{
		ID:          key.ID,
		PrincipalID: key.PrincipalID,
		Name:        key.Name,
		TokenPrefix: key.TokenPrefix,
		Token:       credential.Token,
		Scopes:      key.Scopes,
		CreatedAt:   key.CreatedAt,
		ExpiresAt:   key.ExpiresAt,
		LastUsedAt:  key.LastUsedAt,
		RevokedAt:   key.RevokedAt,
	}
}

func principalResponse(principal productidentity.Principal) api.Principal {
	return api.Principal{
		PrincipalID: principal.ID,
		Kind:        string(principal.Kind),
		DisplayName: principal.DisplayName,
		Role:        string(principal.Role),
		DisabledAt:  cloneResponseTime(principal.DisabledAt),
		CreatedAt:   principal.CreatedAt,
		UpdatedAt:   principal.UpdatedAt,
	}
}

func productScopes(scopes []api.APIKeyScope) []productidentity.Scope {
	values := make([]productidentity.Scope, 0, len(scopes))
	for _, scope := range scopes {
		values = append(values, productidentity.Scope(scope))
	}

	return values
}

func apiScopes(scopes []productidentity.Scope) []api.APIKeyScope {
	values := make([]api.APIKeyScope, 0, len(scopes))
	for _, scope := range scopes {
		values = append(values, string(scope))
	}

	return values
}

func cloneResponseTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}

	cloned := value.UTC()

	return &cloned
}
