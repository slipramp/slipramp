package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/inbox"
	"github.com/slipramp/slipramp/internal/productidentity"
)

type InboxService interface {
	Load(context.Context, inbox.Query) (inbox.Snapshot, error)
	AcknowledgeProblems(context.Context, uuid.UUID, []string) error
}

var errInboxServiceUnavailable = errors.New("inbox service is not configured")

func (h *Handler) GetInbox(
	ctx context.Context,
	_ api.GetInboxRequestObject,
) (api.GetInboxResponseObject, error) {
	authentication, err := currentProductAuthentication(ctx)
	if err != nil {
		return nil, err
	}
	if h.inbox == nil {
		return nil, errInboxServiceUnavailable
	}

	snapshot, err := h.inbox.Load(ctx, inbox.Query{
		PrincipalID:               authentication.Principal.ID,
		IncludePromotionDecisions: authentication.Authorizes(productidentity.PermissionPromotionsWrite),
		IncludeRunDecisions:       authentication.Authorizes(productidentity.PermissionRunsReview),
	})
	if err != nil {
		return nil, err
	}

	response := api.Inbox{
		GeneratedAt: snapshot.GeneratedAt,
		Capabilities: api.InboxCapabilities{
			AcknowledgeProblems: authentication.Authorizes(productidentity.PermissionInboxWrite),
		},
		Decisions: inboxItemResponses(snapshot.Decisions),
		Problems:  inboxItemResponses(snapshot.Problems),
		Activity: api.InboxActivity{
			Queued:   snapshot.Activity.Queued,
			Starting: snapshot.Activity.Starting,
			Running:  snapshot.Activity.Running,
		},
	}

	return api.GetInbox200JSONResponse(response), nil
}

func (h *Handler) AcknowledgeInboxProblems(
	ctx context.Context,
	request api.AcknowledgeInboxProblemsRequestObject,
) (api.AcknowledgeInboxProblemsResponseObject, error) {
	if request.Body == nil {
		return acknowledgeInboxBadRequest("An acknowledgement batch is required."), nil
	}
	authentication, err := currentProductAuthentication(ctx)
	if err != nil {
		return nil, err
	}
	if h.inbox == nil {
		return nil, errInboxServiceUnavailable
	}

	err = h.inbox.AcknowledgeProblems(ctx, authentication.Principal.ID, request.Body.ProblemIds)
	switch {
	case errors.Is(err, inbox.ErrInvalidAcknowledgement):
		return acknowledgeInboxBadRequest(err.Error()), nil
	case err != nil:
		return nil, err
	default:
		return api.AcknowledgeInboxProblems204Response{}, nil
	}
}

func inboxItemResponses(items []inbox.Item) []api.InboxItem {
	responses := make([]api.InboxItem, 0, len(items))
	for _, item := range items {
		responses = append(responses, api.InboxItem{
			ID:          item.ID,
			Kind:        api.InboxItemKind(item.Kind),
			Title:       item.Title,
			Description: item.Description,
			OccurredAt:  item.OccurredAt,
			ActionLabel: item.ActionLabel,
			Destination: api.InboxDestination{
				Type: api.InboxDestinationType(item.Destination.Type),
				ID:   item.Destination.ID,
			},
		})
	}

	return responses
}

func acknowledgeInboxBadRequest(detail string) api.AcknowledgeInboxProblemsResponseObject {
	problem := NewProblem(http.StatusBadRequest, detail)

	return api.AcknowledgeInboxProblems400ApplicationProblemPlusJSONResponse{
		InboxBadRequestApplicationProblemPlusJSONResponse: api.InboxBadRequestApplicationProblemPlusJSONResponse(problem),
	}
}
