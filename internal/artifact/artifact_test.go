package artifact_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/artifact"
)

func TestArtifactValidation(t *testing.T) {
	createdAt := time.Date(2026, time.October, 17, 12, 0, 0, 0, time.UTC)
	digest := strings.Repeat("a", 64)
	sealedAt := createdAt.Add(time.Minute)

	tests := []struct {
		name      string
		configure func(*artifact.Artifact)
	}{
		{
			name: "missing Run",
			configure: func(record *artifact.Artifact) {
				record.RunID = uuid.Nil
			},
		},
		{
			name: "invalid name",
			configure: func(record *artifact.Artifact) {
				record.Name = "Build Log"
			},
		},
		{
			name: "unsupported kind",
			configure: func(record *artifact.Artifact) {
				record.Kind = "archive"
			},
		},
		{
			name: "invalid media type",
			configure: func(record *artifact.Artifact) {
				record.MediaType = "plain text"
			},
		},
		{
			name: "negative size",
			configure: func(record *artifact.Artifact) {
				record.SizeBytes = -1
			},
		},
		{
			name: "absolute object key",
			configure: func(record *artifact.Artifact) {
				key := "/runs/run/artifacts/log"
				record.ObjectKey = &key
			},
		},
		{
			name: "open with digest",
			configure: func(record *artifact.Artifact) {
				record.SHA256 = &digest
			},
		},
		{
			name: "sealed without digest",
			configure: func(record *artifact.Artifact) {
				record.State = artifact.StateSealed
				record.SealedAt = &sealedAt
			},
		},
		{
			name: "seal predates creation",
			configure: func(record *artifact.Artifact) {
				before := createdAt.Add(-time.Second)
				record.State = artifact.StateSealed
				record.SHA256 = &digest
				record.SealedAt = &before
			},
		},
		{
			name: "unsupported retention state",
			configure: func(record *artifact.Artifact) {
				record.RetentionState = "archived"
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record := validArtifact(createdAt)
			test.configure(&record)

			if err := record.Validate(); !errors.Is(err, artifact.ErrInvalid) {
				t.Fatalf("Validate() error = %v, want invalid artifact", err)
			}
		})
	}

	sealed := validArtifact(createdAt)
	sealed.State = artifact.StateSealed
	sealed.SHA256 = &digest
	sealed.SealedAt = &sealedAt
	if err := sealed.Validate(); err != nil {
		t.Errorf("valid sealed Artifact error = %v", err)
	}
}

func TestLogChunkValidation(t *testing.T) {
	createdAt := time.Date(2026, time.October, 17, 12, 0, 0, 0, time.UTC)
	valid := artifact.LogChunk{
		ArtifactID:  uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1300"),
		OffsetBytes: 64,
		SizeBytes:   64,
		SHA256:      strings.Repeat("b", 64),
		ObjectKey:   "runs/run/artifacts/log/chunks/64-b",
		CreatedAt:   createdAt,
	}

	if err := valid.Validate(); err != nil {
		t.Fatalf("valid LogChunk error = %v", err)
	}
	if valid.EndOffset() != 128 {
		t.Errorf("EndOffset() = %d, want 128", valid.EndOffset())
	}

	invalid := valid
	invalid.SizeBytes = 0
	if err := invalid.Validate(); !errors.Is(err, artifact.ErrInvalid) {
		t.Errorf("zero-size LogChunk error = %v, want invalid artifact", err)
	}
}

func validArtifact(createdAt time.Time) artifact.Artifact {
	return artifact.Artifact{
		ID:             uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1301"),
		RunID:          uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1302"),
		Name:           "log",
		Kind:           artifact.KindLog,
		State:          artifact.StateOpen,
		MediaType:      "text/plain; charset=utf-8",
		RetentionState: artifact.RetentionRetained,
		CreatedAt:      createdAt,
	}
}
