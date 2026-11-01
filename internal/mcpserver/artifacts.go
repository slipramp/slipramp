package mcpserver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/slipramp/slipramp/internal/artifact"
	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/runreport"
)

const (
	defaultContentPageSizeBytes int64 = 32 * 1024
	maximumContentPageSizeBytes int64 = 64 * 1024
)

type contentPageInput struct {
	OffsetBytes int64 `json:"offsetBytes,omitempty" jsonschema:"Zero-based byte offset. Defaults to 0."`
	LimitBytes  int64 `json:"limitBytes,omitempty" jsonschema:"Maximum source bytes to return, from 1 to 65536. Defaults to 32768."`
}

type readRunLogInput struct {
	RunID string `json:"runId" jsonschema:"Run UUID."`
	contentPageInput
}

type readRunLogOutput struct {
	RunID            string `json:"runId"`
	ArtifactID       string `json:"artifactId"`
	State            string `json:"state"`
	RetentionState   string `json:"retentionState"`
	Encoding         string `json:"encoding"`
	Contents         string `json:"contents"`
	OffsetBytes      int64  `json:"offsetBytes"`
	NextOffsetBytes  int64  `json:"nextOffsetBytes"`
	DurableSizeBytes int64  `json:"durableSizeBytes"`
	HasMore          bool   `json:"hasMore"`
	Complete         bool   `json:"complete"`
	Failed           bool   `json:"failed"`
}

func runLogToolCallFields(input readRunLogInput, _ readRunLogOutput) []any {
	return uuidToolCallField(nil, "run_id", input.RunID)
}

func (s *toolServer) readRunLog(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input readRunLogInput,
) (*mcp.CallToolResult, readRunLogOutput, error) {
	if _, err := authorize(ctx, productidentity.PermissionRead); err != nil {
		return nil, readRunLogOutput{}, err
	}
	runID, err := requiredUUID(input.RunID, "runId")
	if err != nil {
		return nil, readRunLogOutput{}, err
	}
	offset, limit, err := contentPage(input.contentPageInput)
	if err != nil {
		return nil, readRunLogOutput{}, err
	}

	records, err := s.artifacts.List(ctx, runID)
	if errors.Is(err, artifact.ErrNotFound) {
		return nil, readRunLogOutput{}, errors.New("the Run does not exist")
	}
	if err != nil {
		return nil, readRunLogOutput{}, s.internalError("list Run artifacts for log read", err)
	}
	var logRecord *artifact.Record
	for index := range records {
		if records[index].Kind == artifact.KindLog {
			logRecord = &records[index]
			break
		}
	}
	if logRecord == nil {
		return nil, readRunLogOutput{}, errors.New("the Run log does not exist yet")
	}
	if logRecord.RetentionState != artifact.RetentionRetained {
		return nil, readRunLogOutput{}, errors.New("the Run log contents are no longer retained")
	}
	if offset > logRecord.SizeBytes {
		return nil, readRunLogOutput{}, errors.New("offsetBytes exceeds the durable Run log size")
	}

	contents := []byte{}
	nextOffset := offset
	durableSize := logRecord.SizeBytes
	if offset < logRecord.SizeBytes {
		stream, err := s.artifacts.StreamLog(ctx, runID, offset)
		if errors.Is(err, artifact.ErrNotFound) {
			return nil, readRunLogOutput{}, errors.New("the Run log does not exist")
		}
		if errors.Is(err, artifact.ErrInvalid) {
			return nil, readRunLogOutput{}, err
		}
		if err != nil {
			return nil, readRunLogOutput{}, s.internalError("open Run log stream", err)
		}

		chunk, readErr := stream.Next()
		closeErr := stream.Close()
		if readErr != nil || closeErr != nil {
			return nil, readRunLogOutput{}, s.internalError(
				"read Run log stream",
				errors.Join(readErr, closeErr),
			)
		}
		if chunk.OffsetBytes != offset {
			return nil, readRunLogOutput{}, s.internalError(
				"read Run log stream",
				fmt.Errorf("log stream returned byte offset %d, expected %d", chunk.OffsetBytes, offset),
			)
		}
		contents = chunk.Contents
		if int64(len(contents)) > limit {
			contents = contents[:limit]
		}
		nextOffset = offset + int64(len(contents))
		durableSize = max(durableSize, chunk.NextOffsetBytes)
	}

	encoding, encoded := encodeContents(contents, "text/plain; charset=utf-8")

	return nil, readRunLogOutput{
		RunID:            runID.String(),
		ArtifactID:       logRecord.ID.String(),
		State:            string(logRecord.State),
		RetentionState:   string(logRecord.RetentionState),
		Encoding:         encoding,
		Contents:         encoded,
		OffsetBytes:      offset,
		NextOffsetBytes:  nextOffset,
		DurableSizeBytes: durableSize,
		HasMore:          nextOffset < durableSize,
		Complete:         logRecord.State == artifact.StateSealed && nextOffset >= durableSize,
		Failed:           logRecord.State == artifact.StateFailed,
	}, nil
}

type listRunArtifactsInput struct {
	RunID string `json:"runId" jsonschema:"Run UUID."`
}

type artifactListOutput struct {
	Items []artifactOutput `json:"items"`
}

func runArtifactsToolCallFields(input listRunArtifactsInput, _ artifactListOutput) []any {
	return uuidToolCallField(nil, "run_id", input.RunID)
}

type artifactOutput struct {
	ID             string                   `json:"id"`
	RunID          string                   `json:"runId"`
	Name           string                   `json:"name"`
	Description    string                   `json:"description,omitempty"`
	Kind           string                   `json:"kind"`
	State          string                   `json:"state"`
	MediaType      string                   `json:"mediaType"`
	SizeBytes      int64                    `json:"sizeBytes"`
	SHA256         *string                  `json:"sha256,omitempty"`
	Sensitive      bool                     `json:"sensitive"`
	RetentionState string                   `json:"retentionState"`
	CreatedAt      string                   `json:"createdAt"`
	SealedAt       *string                  `json:"sealedAt,omitempty"`
	Provenance     artifactProvenanceOutput `json:"provenance"`
	Report         *artifactReportOutput    `json:"report,omitempty"`
}

type artifactProvenanceOutput struct {
	ProducerRunID     string `json:"producerRunId"`
	RepositoryID      string `json:"repositoryId"`
	CatalogRevisionID string `json:"catalogRevisionId"`
	CommitSHA         string `json:"commitSha"`
	Target            string `json:"target"`
	Action            string `json:"action"`
}

type artifactReportOutput struct {
	Kind           string `json:"kind"`
	SchemaVersion  int    `json:"schemaVersion"`
	AdapterVersion int    `json:"adapterVersion"`
	Tool           string `json:"tool"`
	ToolVersion    string `json:"toolVersion,omitempty"`
	Mode           string `json:"mode"`
	Summary        any    `json:"summary"`
}

func (s *toolServer) listRunArtifacts(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input listRunArtifactsInput,
) (*mcp.CallToolResult, artifactListOutput, error) {
	if _, err := authorize(ctx, productidentity.PermissionRead); err != nil {
		return nil, artifactListOutput{}, err
	}
	runID, err := requiredUUID(input.RunID, "runId")
	if err != nil {
		return nil, artifactListOutput{}, err
	}

	records, err := s.artifacts.List(ctx, runID)
	if errors.Is(err, artifact.ErrNotFound) {
		return nil, artifactListOutput{}, errors.New("the Run does not exist")
	}
	if err != nil {
		return nil, artifactListOutput{}, s.internalError("list Run artifacts", err)
	}
	slices.SortFunc(records, func(left, right artifact.Record) int {
		return strings.Compare(left.Name, right.Name)
	})

	items := make([]artifactOutput, 0, len(records))
	for _, record := range records {
		item, err := newArtifactOutput(record)
		if err != nil {
			return nil, artifactListOutput{}, s.internalError("encode Run artifact metadata", err)
		}
		items = append(items, item)
	}

	return nil, artifactListOutput{Items: items}, nil
}

func newArtifactOutput(record artifact.Record) (artifactOutput, error) {
	var report *artifactReportOutput
	if record.Report != nil {
		encodedSummary, err := runreport.EncodeSummary(record.Report.Summary)
		if err != nil {
			return artifactOutput{}, err
		}
		var summary any
		if err := json.Unmarshal(encodedSummary, &summary); err != nil {
			return artifactOutput{}, fmt.Errorf("decode Run report summary: %w", err)
		}
		report = &artifactReportOutput{
			Kind:           string(record.Report.Kind),
			SchemaVersion:  record.Report.SchemaVersion,
			AdapterVersion: record.Report.AdapterVersion,
			Tool:           record.Report.Tool,
			ToolVersion:    record.Report.ToolVersion,
			Mode:           string(record.Report.Mode),
			Summary:        summary,
		}
	}

	return artifactOutput{
		ID:             record.ID.String(),
		RunID:          record.RunID.String(),
		Name:           record.Name,
		Description:    record.Description,
		Kind:           string(record.Kind),
		State:          string(record.State),
		MediaType:      record.MediaType,
		SizeBytes:      record.SizeBytes,
		SHA256:         record.SHA256,
		Sensitive:      record.Sensitive,
		RetentionState: string(record.RetentionState),
		CreatedAt:      formatTime(record.CreatedAt),
		SealedAt:       optionalTime(record.SealedAt),
		Provenance: artifactProvenanceOutput{
			ProducerRunID:     record.Provenance.ProducerRunID.String(),
			RepositoryID:      record.Provenance.RepositoryID.String(),
			CatalogRevisionID: record.Provenance.CatalogRevisionID.String(),
			CommitSHA:         record.Provenance.CommitSHA,
			Target:            record.Provenance.TargetName,
			Action:            record.Provenance.ActionName,
		},
		Report: report,
	}, nil
}

type readArtifactInput struct {
	ArtifactID string `json:"artifactId" jsonschema:"Artifact UUID from list_run_artifacts."`
	contentPageInput
}

type readArtifactOutput struct {
	ArtifactID      string `json:"artifactId"`
	MediaType       string `json:"mediaType"`
	Sensitive       bool   `json:"sensitive"`
	Encoding        string `json:"encoding"`
	Contents        string `json:"contents"`
	OffsetBytes     int64  `json:"offsetBytes"`
	NextOffsetBytes int64  `json:"nextOffsetBytes"`
	SizeBytes       int64  `json:"sizeBytes"`
	HasMore         bool   `json:"hasMore"`
}

func artifactToolCallFields(input readArtifactInput, _ readArtifactOutput) []any {
	return uuidToolCallField(nil, "artifact_id", input.ArtifactID)
}

func (s *toolServer) readArtifact(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input readArtifactInput,
) (*mcp.CallToolResult, readArtifactOutput, error) {
	authentication, err := authorize(ctx, productidentity.PermissionRead)
	if err != nil {
		return nil, readArtifactOutput{}, err
	}
	artifactID, err := requiredUUID(input.ArtifactID, "artifactId")
	if err != nil {
		return nil, readArtifactOutput{}, err
	}
	offset, limit, err := contentPage(input.contentPageInput)
	if err != nil {
		return nil, readArtifactOutput{}, err
	}

	content, err := s.artifacts.OpenArtifact(ctx, artifactID)
	if errors.Is(err, artifact.ErrNotFound) {
		return nil, readArtifactOutput{}, errors.New("the Artifact does not exist or is no longer retained")
	}
	if errors.Is(err, artifact.ErrNotSealed) {
		return nil, readArtifactOutput{}, errors.New("the Artifact is not sealed")
	}
	if err != nil {
		return nil, readArtifactOutput{}, s.internalError("open Artifact contents", err)
	}
	if content.Artifact.Sensitive && !authentication.Authorizes(productidentity.PermissionSensitiveArtifactsRead) {
		if closeErr := content.Body.Close(); closeErr != nil {
			return nil, readArtifactOutput{}, s.internalError("close unauthorized Artifact contents", closeErr)
		}

		return nil, readArtifactOutput{}, errors.New(
			"the authenticated API key is not authorized to read sensitive Artifact contents",
		)
	}
	if offset > content.Artifact.SizeBytes {
		if closeErr := content.Body.Close(); closeErr != nil {
			return nil, readArtifactOutput{}, s.internalError("close Artifact contents", closeErr)
		}

		return nil, readArtifactOutput{}, errors.New("offsetBytes exceeds the Artifact size")
	}

	contents, err := readContentPage(content.Body, offset, limit, content.Artifact.SizeBytes)
	if err != nil {
		return nil, readArtifactOutput{}, s.internalError("read Artifact contents", err)
	}
	encoding, encoded := encodeContents(contents, content.Artifact.MediaType)
	nextOffset := offset + int64(len(contents))

	return nil, readArtifactOutput{
		ArtifactID:      artifactID.String(),
		MediaType:       content.Artifact.MediaType,
		Sensitive:       content.Artifact.Sensitive,
		Encoding:        encoding,
		Contents:        encoded,
		OffsetBytes:     offset,
		NextOffsetBytes: nextOffset,
		SizeBytes:       content.Artifact.SizeBytes,
		HasMore:         nextOffset < content.Artifact.SizeBytes,
	}, nil
}

func contentPage(input contentPageInput) (int64, int64, error) {
	if input.OffsetBytes < 0 {
		return 0, 0, errors.New("offsetBytes must not be negative")
	}
	limit := input.LimitBytes
	if limit == 0 {
		limit = defaultContentPageSizeBytes
	}
	if limit < 1 || limit > maximumContentPageSizeBytes {
		return 0, 0, fmt.Errorf("limitBytes must be between 1 and %d", maximumContentPageSizeBytes)
	}

	return input.OffsetBytes, limit, nil
}

func readContentPage(body io.ReadCloser, offset, limit, size int64) ([]byte, error) {
	if offset > 0 {
		if _, err := io.CopyN(io.Discard, body, offset); err != nil {
			_ = body.Close()
			return nil, fmt.Errorf("seek to Artifact byte offset: %w", err)
		}
	}
	pageSize := min(limit, size-offset)
	contents, readErr := io.ReadAll(io.LimitReader(body, pageSize))
	closeErr := body.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, err
	}
	if int64(len(contents)) != pageSize {
		return nil, fmt.Errorf("artifact page has size %d, expected %d", len(contents), pageSize)
	}

	return contents, nil
}

func encodeContents(contents []byte, mediaType string) (string, string) {
	parsedMediaType, _, err := mime.ParseMediaType(mediaType)
	textual := err == nil && (strings.HasPrefix(parsedMediaType, "text/") ||
		parsedMediaType == "application/json" || strings.HasSuffix(parsedMediaType, "+json") ||
		parsedMediaType == "application/xml" || strings.HasSuffix(parsedMediaType, "+xml"))
	if textual && utf8.Valid(contents) {
		return "utf-8", string(contents)
	}

	return "base64", base64.StdEncoding.EncodeToString(contents)
}
