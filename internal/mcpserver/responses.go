package mcpserver

import (
	"time"

	"github.com/slipramp/slipramp/internal/productidentity"
	"github.com/slipramp/slipramp/internal/run"
)

type targetListOutput struct {
	Items  []targetOutput `json:"items"`
	Limit  int32          `json:"limit"`
	Offset int32          `json:"offset"`
	Total  int64          `json:"total"`
}

type targetOutput struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Title               string   `json:"title,omitempty"`
	RepositoryID        string   `json:"repositoryId"`
	RepositoryName      string   `json:"repositoryName"`
	CatalogRevisionID   string   `json:"catalogRevisionId"`
	CatalogCommitSHA    string   `json:"catalogCommitSha"`
	DesiredCommitSHA    string   `json:"desiredCommitSha"`
	Runtime             string   `json:"runtime,omitempty"`
	Placement           string   `json:"placement,omitempty"`
	Actions             []string `json:"actions"`
	ReconciliationState string   `json:"reconciliationState"`
}

type runOutput struct {
	ID                       string         `json:"id"`
	Phase                    string         `json:"phase"`
	Outcome                  *string        `json:"outcome,omitempty"`
	TargetID                 string         `json:"targetId"`
	RepositoryID             string         `json:"repositoryId"`
	CatalogRevisionID        string         `json:"catalogRevisionId"`
	CommitSHA                string         `json:"commitSha"`
	TargetRelevanceCommitSHA *string        `json:"targetRelevanceCommitSha,omitempty"`
	Target                   string         `json:"target"`
	Action                   string         `json:"action"`
	Parameters               map[string]any `json:"parameters"`
	Placement                string         `json:"placement"`
	Cluster                  string         `json:"cluster"`
	Source                   string         `json:"source"`
	IdempotencyKey           *string        `json:"idempotencyKey,omitempty"`
	CreatedBy                *actorOutput   `json:"createdBy,omitempty"`
	CanceledBy               *actorOutput   `json:"canceledBy,omitempty"`
	ReviewState              string         `json:"reviewState"`
	CreatedAt                string         `json:"createdAt"`
	AssignedAt               *string        `json:"assignedAt,omitempty"`
	PreparingAt              *string        `json:"preparingAt,omitempty"`
	StartedAt                *string        `json:"startedAt,omitempty"`
	FinishedAt               *string        `json:"finishedAt,omitempty"`
	LastObservedAt           *string        `json:"lastObservedAt,omitempty"`
	CancellationRequestedAt  *string        `json:"cancellationRequestedAt,omitempty"`
	OutcomeReason            *string        `json:"outcomeReason,omitempty"`
	OutcomeMessage           *string        `json:"outcomeMessage,omitempty"`
	ExitCode                 *int32         `json:"exitCode,omitempty"`
}

type actorOutput struct {
	PrincipalID string `json:"principalId"`
	Kind        string `json:"kind"`
	DisplayName string `json:"displayName"`
}

func newRunOutput(record run.Run) runOutput {
	return runOutput{
		ID:                       record.ID.String(),
		Phase:                    string(record.Phase),
		Outcome:                  optionalOutcome(record.Outcome),
		TargetID:                 record.TargetID.String(),
		RepositoryID:             record.RepositoryID.String(),
		CatalogRevisionID:        record.CatalogRevisionID.String(),
		CommitSHA:                record.CommitSHA,
		TargetRelevanceCommitSHA: record.TargetRelevanceCommitSHA,
		Target:                   record.TargetName,
		Action:                   record.ActionName,
		Parameters:               record.Parameters,
		Placement:                record.PlacementName,
		Cluster:                  record.ClusterName,
		Source:                   string(record.Source.Type),
		IdempotencyKey:           record.IdempotencyKey,
		CreatedBy:                optionalActor(record.CreatedBy),
		CanceledBy:               optionalActor(record.CanceledBy),
		ReviewState:              string(record.Review.State),
		CreatedAt:                formatTime(record.CreatedAt),
		AssignedAt:               optionalTime(record.AssignedAt),
		PreparingAt:              optionalTime(record.PreparingAt),
		StartedAt:                optionalTime(record.StartedAt),
		FinishedAt:               optionalTime(record.FinishedAt),
		LastObservedAt:           optionalTime(record.LastObservedAt),
		CancellationRequestedAt:  optionalTime(record.CancellationRequestedAt),
		OutcomeReason:            record.OutcomeReason,
		OutcomeMessage:           record.OutcomeMessage,
		ExitCode:                 record.ExitCode,
	}
}

func optionalActor(actor *productidentity.ActorSnapshot) *actorOutput {
	if actor == nil {
		return nil
	}

	return &actorOutput{
		PrincipalID: actor.PrincipalID.String(),
		Kind:        string(actor.Kind),
		DisplayName: actor.DisplayName,
	}
}

func optionalOutcome(outcome *run.Outcome) *string {
	if outcome == nil {
		return nil
	}
	value := string(*outcome)

	return &value
}

func optionalTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := formatTime(*value)

	return &formatted
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}
