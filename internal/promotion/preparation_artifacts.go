package promotion

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/artifact"
)

const (
	GitDiffArtifactName       = "git-diff"
	GitDiffArtifactMediaType  = "text/x-diff; charset=utf-8"
	ArgoDiffArtifactName      = "argo-diff"
	ArgoDiffArtifactMediaType = "application/vnd.slipramp.argocd-diff+json"

	MaximumPreparationArtifactSizeBytes int64 = 16 * 1024 * 1024
)

type argoDiffArtifact struct {
	Resources []argoDiffResource `json:"resources"`
}

type argoDiffResource struct {
	Group               string `json:"group,omitempty"`
	Kind                string `json:"kind"`
	Namespace           string `json:"namespace,omitempty"`
	Name                string `json:"name"`
	Diff                string `json:"diff"`
	VersionMetadataOnly bool   `json:"versionMetadataOnly,omitempty"`
}

func (c *PreparationCoordinator) validPreparationArtifact(
	ctx context.Context,
	records []artifact.Record,
	name string,
	mediaType string,
) (artifact.Artifact, error) {
	var record artifact.Artifact
	for _, candidate := range records {
		if candidate.Name == name {
			record = candidate.Artifact
			break
		}
	}
	if record.ID == uuid.Nil {
		return artifact.Artifact{}, fmt.Errorf(
			"%w: required %s artifact is missing",
			ErrInvalidPreparationArtifact,
			name,
		)
	}
	if record.Kind != artifact.KindOutput || record.State != artifact.StateSealed || record.SHA256 == nil ||
		record.MediaType != mediaType || !record.Sensitive || record.RetentionState != artifact.RetentionRetained {
		return artifact.Artifact{}, fmt.Errorf(
			"%w: required %s artifact is not a retained sensitive sealed %s output",
			ErrInvalidPreparationArtifact,
			name,
			mediaType,
		)
	}
	if record.SizeBytes > MaximumPreparationArtifactSizeBytes {
		return artifact.Artifact{}, fmt.Errorf(
			"%w: required %s artifact exceeds the %d-byte limit",
			ErrInvalidPreparationArtifact,
			name,
			MaximumPreparationArtifactSizeBytes,
		)
	}

	content, err := c.artifacts.OpenArtifact(ctx, record.ID)
	if err != nil {
		return artifact.Artifact{}, fmt.Errorf("open required %s artifact: %w", name, err)
	}
	contents, readErr := io.ReadAll(io.LimitReader(content.Body, MaximumPreparationArtifactSizeBytes+1))
	closeErr := content.Body.Close()
	if readErr != nil || closeErr != nil {
		return artifact.Artifact{}, errors.Join(readErr, closeErr)
	}
	if int64(len(contents)) != record.SizeBytes {
		return artifact.Artifact{}, fmt.Errorf(
			"%w: required %s artifact size does not match its metadata",
			ErrInvalidPreparationArtifact,
			name,
		)
	}
	digest := sha256.Sum256(contents)
	if hex.EncodeToString(digest[:]) != *record.SHA256 {
		return artifact.Artifact{}, fmt.Errorf(
			"%w: required %s artifact digest does not match its metadata",
			ErrInvalidPreparationArtifact,
			name,
		)
	}
	if name == GitDiffArtifactName && !utf8.Valid(contents) {
		return artifact.Artifact{}, fmt.Errorf(
			"%w: required %s artifact is not valid UTF-8 text",
			ErrInvalidPreparationArtifact,
			name,
		)
	}
	if name == ArgoDiffArtifactName {
		if err := validateArgoDiffArtifact(contents); err != nil {
			return artifact.Artifact{}, fmt.Errorf(
				"%w: required %s artifact is malformed: %w",
				ErrInvalidPreparationArtifact,
				name,
				err,
			)
		}
	}

	return record, nil
}

func validateArgoDiffArtifact(contents []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()

	var value argoDiffArtifact
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("unexpected data after JSON object")
		}

		return err
	}
	if value.Resources == nil {
		return errors.New("resources must be an array")
	}
	for index, resource := range value.Resources {
		if strings.TrimSpace(resource.Kind) == "" || strings.TrimSpace(resource.Name) == "" ||
			strings.TrimSpace(resource.Diff) == "" {
			return fmt.Errorf("resource %d requires kind, name, and diff", index+1)
		}
	}

	return nil
}

func (c *PreparationCoordinator) failInvalidArtifacts(
	ctx context.Context,
	record Promotion,
	runID uuid.UUID,
	completedAt time.Time,
	cause error,
) (bool, error) {
	message := "Preparation Run artifacts are invalid: " + cause.Error() + "."
	if _, err := c.store.FailPromotionPreparation(
		ctx,
		record.ID,
		runID,
		message,
		completedAt,
	); err != nil {
		return false, err
	}

	return true, nil
}
