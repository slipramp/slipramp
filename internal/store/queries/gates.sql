-- name: UpsertGateObservation :exec
INSERT INTO gate_observations (
    repository_id,
    gate_name,
    catalog_revision_id,
    definition_digest,
    evaluator_process_id,
    state,
    value,
    diagnostic,
    evaluated_at,
    valid_until
) VALUES (
    sqlc.arg(repository_id),
    sqlc.arg(gate_name),
    sqlc.arg(catalog_revision_id),
    sqlc.arg(definition_digest),
    sqlc.arg(evaluator_process_id),
    sqlc.arg(state),
    sqlc.narg(value),
    sqlc.narg(diagnostic),
    sqlc.arg(evaluated_at),
    sqlc.arg(valid_until)
)
ON CONFLICT (
    repository_id,
    gate_name,
    catalog_revision_id,
    definition_digest
) DO UPDATE
SET evaluator_process_id = EXCLUDED.evaluator_process_id,
    state = EXCLUDED.state,
    value = EXCLUDED.value,
    diagnostic = EXCLUDED.diagnostic,
    evaluated_at = EXCLUDED.evaluated_at,
    valid_until = EXCLUDED.valid_until;

-- name: GetGateObservation :one
SELECT *
FROM gate_observations
WHERE repository_id = sqlc.arg(repository_id)
  AND gate_name = sqlc.arg(gate_name)
  AND catalog_revision_id = sqlc.arg(catalog_revision_id)
  AND definition_digest = sqlc.arg(definition_digest);
