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

-- name: LockQueuedRunGateRepositories :many
SELECT repositories.id
FROM repositories
WHERE repositories.id IN (
    SELECT run_gates.repository_id
    FROM run_gates
    JOIN runs
      ON runs.id = run_gates.run_id
    WHERE runs.cluster_name = sqlc.arg(cluster_name)
      AND runs.phase = 'queued'
      AND run_gates.released_at IS NULL
)
ORDER BY repositories.id
FOR UPDATE;

-- name: ListGateEvaluationTargets :many
SELECT DISTINCT
    run_gates.repository_id,
    run_gates.gate_name,
    repositories.active_catalog_revision_id AS catalog_revision_id,
    catalog_gates.normalized_definition
FROM run_gates
JOIN runs
  ON runs.id = run_gates.run_id
JOIN repositories
  ON repositories.id = run_gates.repository_id
JOIN catalog_revisions
  ON catalog_revisions.id = repositories.active_catalog_revision_id
JOIN catalog_gates
  ON catalog_gates.catalog_snapshot_id = catalog_revisions.catalog_snapshot_id
 AND catalog_gates.name = run_gates.gate_name
WHERE run_gates.released_at IS NULL
  AND runs.phase = 'queued'
  AND repositories.removed_at IS NULL
ORDER BY
    run_gates.repository_id,
    run_gates.gate_name,
    repositories.active_catalog_revision_id;

-- name: ReleaseRunGates :execrows
WITH evidence AS (
    SELECT
        run_gates.run_id,
        run_gates.gate_name,
        repositories.active_catalog_revision_id AS catalog_revision_id,
        catalog_gates.normalized_definition,
        catalog_gates.definition_digest,
        gate_observations.evaluator_process_id,
        gate_observations.value,
        gate_observations.evaluated_at,
        gate_observations.valid_until
    FROM run_gates
    JOIN repositories
      ON repositories.id = run_gates.repository_id
    JOIN catalog_revisions
      ON catalog_revisions.id = repositories.active_catalog_revision_id
    JOIN catalog_gates
      ON catalog_gates.catalog_snapshot_id = catalog_revisions.catalog_snapshot_id
     AND catalog_gates.name = run_gates.gate_name
    JOIN gate_observations
      ON gate_observations.repository_id = run_gates.repository_id
     AND gate_observations.gate_name = run_gates.gate_name
     AND gate_observations.catalog_revision_id = repositories.active_catalog_revision_id
     AND gate_observations.definition_digest = catalog_gates.definition_digest
     AND gate_observations.evaluator_process_id = sqlc.arg(evaluator_process_id)
     AND gate_observations.state = 'passing'
     AND gate_observations.evaluated_at <= sqlc.arg(released_at)
     AND gate_observations.valid_until > sqlc.arg(released_at)
    WHERE run_gates.run_id = sqlc.arg(run_id)
      AND run_gates.released_at IS NULL
      AND repositories.removed_at IS NULL
)
UPDATE run_gates
SET released_at = sqlc.arg(released_at),
    release_catalog_revision_id = evidence.catalog_revision_id,
    release_definition = evidence.normalized_definition,
    release_definition_digest = evidence.definition_digest,
    release_evaluator_process_id = evidence.evaluator_process_id,
    release_observed_value = evidence.value,
    release_evaluated_at = evidence.evaluated_at,
    release_valid_until = evidence.valid_until
FROM evidence
WHERE run_gates.run_id = evidence.run_id
  AND run_gates.gate_name = evidence.gate_name;
