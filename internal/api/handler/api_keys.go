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
	AuthenticateAPIKey(
		context.Context,
		string,
	) (productidentity.Authentication, error)
	ListOwnAPIKeys(
		context.Context,
		productidentity.Authentication,
	) ([]productidentity.APIKey, error)
	CreateUserAPIKey(
		context.Context,
		productidentity.Authentication,
		string,
		[]productidentity.Scope,
		*time.Time,
	) (productauth.APIKeyCredential, error)
	RevokeAPIKey(
		context.Context,
		productidentity.Authentication,
		uuid.UUID,
	) error
	ListServiceAccounts(context.Context) ([]productidentity.Principal, error)
	CreateServiceAccount(
		context.Context,
		string,
		productidentity.Role,
	) (productidentity.Principal, error)
	ListServiceAccountAPIKeys(context.Context, uuid.UUID) ([]productidentity.APIKey, error)
	CreateServiceAccountAPIKey(
		context.Context,
		uuid.UUID,
		uuid.UUID,
		string,
		[]productidentity.Scope,
		*time.Time,
	) (productauth.APIKeyCredential, error)
}

func (h *Handler) ListAPIKeys(
	ctx context.Context,
	_ api.ListAPIKeysRequestObject,
) (api.ListAPIKeysResponseObject, error) {
	authentication, err := currentProductAuthentication(ctx)
	if err != nil {
		return nil, err
	}

	keys, err := h.productAuth.ListOwnAPIKeys(ctx, authentication)
	if err != nil {
		return nil, err
	}

	return api.ListAPIKeys200JSONResponse{Items: apiKeyResponses(keys)}, nil
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

func (h *Handler) ListServiceAccounts(
	ctx context.Context,
	_ api.ListServiceAccountsRequestObject,
) (api.ListServiceAccountsResponseObject, error) {
	principals, err := h.productAuth.ListServiceAccounts(ctx)
	if err != nil {
		return nil, err
	}

	items := make([]api.ServiceAccount, 0, len(principals))
	for _, principal := range principals {
		items = append(items, serviceAccountResponse(principal))
	}

	return api.ListServiceAccounts200JSONResponse{Items: items}, nil
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

	return api.CreateServiceAccount201JSONResponse(serviceAccountResponse(principal)), nil
}

func (h *Handler) ListServiceAccountAPIKeys(
	ctx context.Context,
	request api.ListServiceAccountAPIKeysRequestObject,
) (api.ListServiceAccountAPIKeysResponseObject, error) {
	keys, err := h.productAuth.ListServiceAccountAPIKeys(ctx, request.PrincipalID)
	if errors.Is(err, productidentity.ErrPrincipalNotFound) {
		problem := NewProblem(http.StatusNotFound, "The service account does not exist.")

		return api.ListServiceAccountAPIKeys404ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, productidentity.ErrInvalid) {
		problem := NewProblem(http.StatusBadRequest, err.Error())

		return api.ListServiceAccountAPIKeys400ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}

	return api.ListServiceAccountAPIKeys200JSONResponse{Items: apiKeyResponses(keys)}, nil
}

func (h *Handler) CreateServiceAccountAPIKey(
	ctx context.Context,
	request api.CreateServiceAccountAPIKeyRequestObject,
) (api.CreateServiceAccountAPIKeyResponseObject, error) {
	if request.Body == nil {
		problem := NewProblem(http.StatusBadRequest, "An API key configuration is required.")

		return api.CreateServiceAccountAPIKey400ApplicationProblemPlusJSONResponse(problem), nil
	}
	authentication, err := currentProductAuthentication(ctx)
	if err != nil {
		return nil, err
	}

	credential, err := h.productAuth.CreateServiceAccountAPIKey(
		ctx,
		request.PrincipalID,
		authentication.Principal.ID,
		request.Body.Name,
		productScopes(request.Body.Scopes),
		request.Body.ExpiresAt,
	)
	if errors.Is(err, productidentity.ErrPrincipalNotFound) {
		problem := NewProblem(http.StatusNotFound, "The service account does not exist.")

		return api.CreateServiceAccountAPIKey404ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, productidentity.ErrInvalid) {
		problem := NewProblem(http.StatusBadRequest, err.Error())

		return api.CreateServiceAccountAPIKey400ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}

	return api.CreateServiceAccountAPIKey201JSONResponse{
		Body: apiKeyCredentialResponse(credential),
		Headers: api.CreateServiceAccountAPIKey201ResponseHeaders{
			Location: fmt.Sprintf("/api/v1/auth/api-keys/%s", credential.Key.ID),
		},
	}, nil
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

func serviceAccountResponse(principal productidentity.Principal) api.ServiceAccount {
	return api.ServiceAccount{
		PrincipalID: principal.ID,
		DisplayName: principal.DisplayName,
		Role:        string(principal.Role),
		CreatedAt:   principal.CreatedAt,
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
