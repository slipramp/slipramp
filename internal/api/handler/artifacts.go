package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/artifact"
	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/repository"
)

type ArtifactService interface {
	AppendLogChunk(context.Context, artifact.AppendLogChunkRequest) error
	GetLogCursor(context.Context, uuid.UUID, string) (artifact.LogCursor, error)
	FailLog(context.Context, artifact.FailLogRequest) error
	SealLog(context.Context, artifact.SealLogRequest) error
	RunToken(uuid.UUID) string
	AuthenticateRunToken(uuid.UUID, string) bool
	Upload(context.Context, artifact.UploadRequest) error
	List(context.Context, uuid.UUID) ([]artifact.Record, error)
	OpenArtifact(context.Context, uuid.UUID) (artifact.Content, error)
	OpenInput(context.Context, uuid.UUID, string) (artifact.Content, error)
	StreamLog(context.Context, uuid.UUID, int64) (artifact.LogStream, error)
}

func (h *Handler) AppendRunLogChunk(
	ctx context.Context,
	request api.AppendRunLogChunkRequestObject,
) (api.AppendRunLogChunkResponseObject, error) {
	principal, err := controllerPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if request.Body == nil {
		problem := NewProblem(http.StatusBadRequest, "A log chunk body is required.")
		return api.AppendRunLogChunk400ApplicationProblemPlusJSONResponse(problem), nil
	}

	contents, err := io.ReadAll(io.LimitReader(request.Body, artifact.MaxLogChunkSizeBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read log chunk body: %w", err)
	}
	if len(contents) > artifact.MaxLogChunkSizeBytes {
		problem := NewProblem(http.StatusRequestEntityTooLarge, "The log chunk exceeds the 256 KiB limit.")
		return api.AppendRunLogChunk413ApplicationProblemPlusJSONResponse(problem), nil
	}

	err = h.artifacts.AppendLogChunk(ctx, artifact.AppendLogChunkRequest{
		RunID:       request.RunID,
		ClusterName: principal.ClusterName,
		OffsetBytes: request.Params.OffsetBytes,
		Contents:    contents,
	})
	if err != nil {
		return appendLogError(err)
	}
	if err := h.clusters.RecordControllerContact(ctx, principal.ClusterName); err != nil {
		return nil, err
	}

	return api.AppendRunLogChunk204Response{}, nil
}

func appendLogError(err error) (api.AppendRunLogChunkResponseObject, error) {
	switch {
	case errors.Is(err, artifact.ErrInvalid):
		problem := NewProblem(http.StatusBadRequest, err.Error())
		return api.AppendRunLogChunk400ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, artifact.ErrChunkTooLarge):
		problem := NewProblem(http.StatusRequestEntityTooLarge, "The log chunk exceeds the 256 KiB limit.")
		return api.AppendRunLogChunk413ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, artifact.ErrNotFound):
		problem := NewProblem(http.StatusNotFound, "The Run does not exist for this controller.")
		return api.AppendRunLogChunk404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, artifact.ErrChunkConflict), errors.Is(err, artifact.ErrLogClosed):
		problem := NewProblem(http.StatusConflict, "The log chunk conflicts with the durable Run log.")
		return api.AppendRunLogChunk409ApplicationProblemPlusJSONResponse(problem), nil
	default:
		return nil, err
	}
}

func (h *Handler) GetRunLogCursor(
	ctx context.Context,
	request api.GetRunLogCursorRequestObject,
) (api.GetRunLogCursorResponseObject, error) {
	principal, err := controllerPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	cursor, err := h.artifacts.GetLogCursor(ctx, request.RunID, principal.ClusterName)
	if errors.Is(err, artifact.ErrNotFound) {
		problem := NewProblem(http.StatusNotFound, "The Run does not exist for this controller.")
		return api.GetRunLogCursor404ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}

	return api.GetRunLogCursor200JSONResponse{
		NextOffsetBytes: cursor.NextOffsetBytes,
		State:           api.LogCursorState(cursor.State),
		Sha256:          cursor.SHA256,
	}, nil
}

func (h *Handler) SealRunLog(
	ctx context.Context,
	request api.SealRunLogRequestObject,
) (api.SealRunLogResponseObject, error) {
	principal, err := controllerPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if request.Body == nil {
		problem := NewProblem(http.StatusBadRequest, "A log seal is required.")
		return api.SealRunLog400ApplicationProblemPlusJSONResponse(problem), nil
	}

	err = h.artifacts.SealLog(ctx, artifact.SealLogRequest{
		RunID:       request.RunID,
		ClusterName: principal.ClusterName,
		SizeBytes:   request.Body.SizeBytes,
		SHA256:      request.Body.Sha256,
	})
	switch {
	case err == nil:
		return api.SealRunLog204Response{}, nil
	case errors.Is(err, artifact.ErrInvalid):
		problem := NewProblem(http.StatusBadRequest, err.Error())
		return api.SealRunLog400ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, artifact.ErrNotFound):
		problem := NewProblem(http.StatusNotFound, "The Run does not exist for this controller.")
		return api.SealRunLog404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, artifact.ErrSealConflict):
		problem := NewProblem(http.StatusConflict, "The log seal conflicts with durable bytes.")
		return api.SealRunLog409ApplicationProblemPlusJSONResponse(problem), nil
	default:
		return nil, err
	}
}

func (h *Handler) FailRunLog(
	ctx context.Context,
	request api.FailRunLogRequestObject,
) (api.FailRunLogResponseObject, error) {
	principal, err := controllerPrincipal(ctx)
	if err != nil {
		return nil, err
	}

	err = h.artifacts.FailLog(ctx, artifact.FailLogRequest{
		RunID:       request.RunID,
		ClusterName: principal.ClusterName,
	})
	switch {
	case err == nil:
		return api.FailRunLog204Response{}, nil
	case errors.Is(err, artifact.ErrInvalid):
		problem := NewProblem(http.StatusBadRequest, err.Error())
		return api.FailRunLog400ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, artifact.ErrNotFound):
		problem := NewProblem(http.StatusNotFound, "The Run does not exist for this controller.")
		return api.FailRunLog404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, artifact.ErrSealConflict):
		problem := NewProblem(http.StatusConflict, "The Run log is already sealed.")
		return api.FailRunLog409ApplicationProblemPlusJSONResponse(problem), nil
	default:
		return nil, err
	}
}

func (h *Handler) UploadRunArtifact(
	ctx context.Context,
	request api.UploadRunArtifactRequestObject,
) (api.UploadRunArtifactResponseObject, error) {
	execution, err := currentExecutionRequest(ctx)
	if err != nil {
		return nil, err
	}
	if !h.artifacts.AuthenticateRunToken(request.RunID, execution.BearerToken) {
		problem := NewProblem(http.StatusUnauthorized, "The Run artifact credential is invalid.")
		return api.UploadRunArtifact401ApplicationProblemPlusJSONResponse(problem), nil
	}
	if request.Body == nil || execution.ContentLength < 0 {
		problem := NewProblem(http.StatusBadRequest, "A body with Content-Length is required.")
		return api.UploadRunArtifact400ApplicationProblemPlusJSONResponse(problem), nil
	}

	err = h.artifacts.Upload(ctx, artifact.UploadRequest{
		RunID:     request.RunID,
		Name:      string(request.ArtifactName),
		Body:      request.Body,
		SizeBytes: execution.ContentLength,
		SHA256:    request.Params.XSliprampContentSHA256,
	})
	switch {
	case err == nil:
		return api.UploadRunArtifact204Response{}, nil
	case errors.Is(err, artifact.ErrInvalid):
		problem := NewProblem(http.StatusBadRequest, err.Error())
		return api.UploadRunArtifact400ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, artifact.ErrNotFound):
		problem := NewProblem(http.StatusNotFound, "The Run or artifact declaration does not exist.")
		return api.UploadRunArtifact404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, artifact.ErrChunkConflict):
		problem := NewProblem(http.StatusConflict, "The artifact already contains different bytes.")
		return api.UploadRunArtifact409ApplicationProblemPlusJSONResponse(problem), nil
	default:
		return nil, err
	}
}

func (h *Handler) ListRunArtifacts(
	ctx context.Context,
	request api.ListRunArtifactsRequestObject,
) (api.ListRunArtifactsResponseObject, error) {
	records, err := h.artifacts.List(ctx, request.RunID)
	if errors.Is(err, artifact.ErrNotFound) {
		problem := NewProblem(http.StatusNotFound, "The Run does not exist.")
		return api.ListRunArtifacts404ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	keys := make([]repository.CommitKey, 0, len(records))
	for _, record := range records {
		keys = append(keys, commitKey(record.Provenance.RepositoryID, record.Provenance.CommitSHA))
	}
	commits, err := h.commitResponses(ctx, keys)
	if err != nil {
		return nil, err
	}

	items := make([]api.Artifact, 0, len(records))
	for _, record := range records {
		commit := commits[commitKey(record.Provenance.RepositoryID, record.Provenance.CommitSHA)]
		items = append(items, artifactResponse(record, commit))
	}

	return api.ListRunArtifacts200JSONResponse{Items: items}, nil
}

func (h *Handler) DownloadArtifact(
	ctx context.Context,
	request api.DownloadArtifactRequestObject,
) (api.DownloadArtifactResponseObject, error) {
	content, err := h.artifacts.OpenArtifact(ctx, request.ArtifactID)
	if errors.Is(err, artifact.ErrNotFound) {
		problem := NewProblem(http.StatusNotFound, "The artifact does not exist.")
		return api.DownloadArtifact404ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, artifact.ErrNotSealed) {
		problem := NewProblem(http.StatusConflict, "The artifact is not sealed.")
		return api.DownloadArtifact409ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	if content.Artifact.Sensitive && !canDownloadSensitiveArtifact(ctx) {
		if closeErr := content.Body.Close(); closeErr != nil {
			return nil, fmt.Errorf("close unauthorized artifact: %w", closeErr)
		}
		problem := NewProblem(
			http.StatusForbidden,
			"The authenticated principal cannot download sensitive artifact contents.",
		)

		return api.DownloadArtifact403ApplicationProblemPlusJSONResponse(problem), nil
	}

	disposition := fmt.Sprintf("attachment; filename=%q", content.Artifact.Name)
	digest := "sha-256=" + *content.Artifact.SHA256
	return api.DownloadArtifact200ApplicationOctetStreamResponse{
		Body:          content.Body,
		ContentLength: content.Artifact.SizeBytes,
		Headers: api.DownloadArtifact200ResponseHeaders{
			ContentDisposition: &disposition,
			Digest:             &digest,
		},
	}, nil
}

func canDownloadSensitiveArtifact(ctx context.Context) bool {
	authentication, authenticated := productAuthenticationFromContext(ctx)
	if !authenticated {
		return false
	}

	return authentication.Authorizes(productidentity.PermissionSensitiveArtifactsRead)
}

func (h *Handler) DownloadRunInput(
	ctx context.Context,
	request api.DownloadRunInputRequestObject,
) (api.DownloadRunInputResponseObject, error) {
	execution, err := currentExecutionRequest(ctx)
	if err != nil {
		return nil, err
	}
	if !h.artifacts.AuthenticateRunToken(request.RunID, execution.BearerToken) {
		problem := NewProblem(http.StatusUnauthorized, "The Run artifact credential is invalid.")
		return api.DownloadRunInput401ApplicationProblemPlusJSONResponse(problem), nil
	}

	content, err := h.artifacts.OpenInput(ctx, request.RunID, string(request.InputName))
	if errors.Is(err, artifact.ErrNotFound) || errors.Is(err, artifact.ErrNotSealed) {
		problem := NewProblem(http.StatusNotFound, "The Run input does not exist.")
		return api.DownloadRunInput404ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}

	digest := "sha-256=" + *content.Artifact.SHA256
	return api.DownloadRunInput200ApplicationOctetStreamResponse{
		Body:          content.Body,
		ContentLength: content.Artifact.SizeBytes,
		Headers:       api.DownloadRunInput200ResponseHeaders{Digest: &digest},
	}, nil
}

func artifactResponse(record artifact.Record, commit api.GitCommit) api.Artifact {
	return api.Artifact{
		ID:             record.ID,
		RunID:          record.RunID,
		Name:           api.CatalogName(record.Name),
		Description:    optionalString(record.Description),
		Kind:           api.ArtifactKind(record.Kind),
		State:          api.ArtifactState(record.State),
		MediaType:      record.MediaType,
		SizeBytes:      record.SizeBytes,
		Sha256:         record.SHA256,
		Sensitive:      record.Sensitive,
		RetentionState: api.ArtifactRetentionState(record.RetentionState),
		CreatedAt:      record.CreatedAt,
		SealedAt:       record.SealedAt,
		Provenance: api.ArtifactProvenance{
			ProducerRunID:     record.Provenance.ProducerRunID,
			RepositoryID:      record.Provenance.RepositoryID,
			CatalogRevisionID: record.Provenance.CatalogRevisionID,
			Commit:            commit,
			Target:            api.CatalogName(record.Provenance.TargetName),
			Action:            api.CatalogName(record.Provenance.ActionName),
		},
	}
}
