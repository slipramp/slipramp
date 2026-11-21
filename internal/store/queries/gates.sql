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

-- name: DeleteInactiveGateObservations :execrows
WITH active_observations AS (
    SELECT
        requirements.repository_id,
        requirements.gate_name,
        repositories.active_catalog_revision_id AS catalog_revision_id,
        catalog_gates.definition_digest
    FROM (
        SELECT
            run_gates.repository_id,
            run_gates.gate_name
        FROM run_gates
        JOIN runs
          ON runs.id = run_gates.run_id
        WHERE run_gates.released_at IS NULL
          AND runs.phase = 'queued'

        UNION

        SELECT
            promotion_gates.repository_id,
            promotion_gates.gate_name
        FROM promotion_gates
        JOIN promotions
          ON promotions.id = promotion_gates.promotion_id
        WHERE promotion_gates.released_at IS NULL
          AND promotions.status IN ('creating', 'preparing', 'pending', 'approved')
          AND promotions.dispatch_failed_at IS NULL
          AND promotions.kargo_outcome_phase IS NULL
    ) AS requirements
    JOIN repositories
      ON repositories.id = requirements.repository_id
    JOIN catalog_revisions
      ON catalog_revisions.id = repositories.active_catalog_revision_id
    JOIN catalog_gates
      ON catalog_gates.catalog_snapshot_id = catalog_revisions.catalog_snapshot_id
     AND catalog_gates.name = requirements.gate_name
    WHERE repositories.removed_at IS NULL
)
DELETE FROM gate_observations
WHERE gate_observations.valid_until < sqlc.arg(inactive_before)
  AND NOT EXISTS (
      SELECT 1
      FROM active_observations
      WHERE active_observations.repository_id = gate_observations.repository_id
        AND active_observations.gate_name = gate_observations.gate_name
        AND active_observations.catalog_revision_id = gate_observations.catalog_revision_id
        AND active_observations.definition_digest = gate_observations.definition_digest
  );

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

-- name: LockPromotionGateRepositories :many
SELECT repositories.id
FROM repositories
WHERE repositories.id IN (
    SELECT promotion_gates.repository_id
    FROM promotion_gates
    WHERE promotion_gates.promotion_id = sqlc.arg(promotion_id)
      AND promotion_gates.released_at IS NULL
)
ORDER BY repositories.id
FOR UPDATE;

-- name: ListGateEvaluationTargets :many
WITH requirements AS (
    SELECT
        run_gates.repository_id,
        run_gates.gate_name
    FROM run_gates
    JOIN runs
      ON runs.id = run_gates.run_id
    WHERE run_gates.released_at IS NULL
      AND runs.phase = 'queued'

    UNION

    SELECT
        promotion_gates.repository_id,
        promotion_gates.gate_name
    FROM promotion_gates
    JOIN promotions
      ON promotions.id = promotion_gates.promotion_id
    WHERE promotion_gates.released_at IS NULL
      AND promotions.status IN ('creating', 'preparing', 'pending', 'approved')
      AND promotions.dispatch_failed_at IS NULL
      AND promotions.kargo_outcome_phase IS NULL
)
SELECT DISTINCT
    requirements.repository_id,
    requirements.gate_name,
    repositories.active_catalog_revision_id AS catalog_revision_id,
    catalog_gates.normalized_definition
FROM requirements
JOIN repositories
  ON repositories.id = requirements.repository_id
JOIN catalog_revisions
  ON catalog_revisions.id = repositories.active_catalog_revision_id
JOIN catalog_gates
  ON catalog_gates.catalog_snapshot_id = catalog_revisions.catalog_snapshot_id
 AND catalog_gates.name = requirements.gate_name
WHERE repositories.removed_at IS NULL
ORDER BY
    requirements.repository_id,
    requirements.gate_name,
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

-- name: ReleasePromotionGates :execrows
WITH evidence AS (
    SELECT
        promotion_gates.promotion_id,
        promotion_gates.gate_name,
        repositories.active_catalog_revision_id AS catalog_revision_id,
        catalog_gates.normalized_definition,
        catalog_gates.definition_digest,
        gate_observations.evaluator_process_id,
        gate_observations.value,
        gate_observations.evaluated_at,
        gate_observations.valid_until
    FROM promotion_gates
    JOIN repositories
      ON repositories.id = promotion_gates.repository_id
    JOIN catalog_revisions
      ON catalog_revisions.id = repositories.active_catalog_revision_id
    JOIN catalog_gates
      ON catalog_gates.catalog_snapshot_id = catalog_revisions.catalog_snapshot_id
     AND catalog_gates.name = promotion_gates.gate_name
    JOIN gate_observations
      ON gate_observations.repository_id = promotion_gates.repository_id
     AND gate_observations.gate_name = promotion_gates.gate_name
     AND gate_observations.catalog_revision_id = repositories.active_catalog_revision_id
     AND gate_observations.definition_digest = catalog_gates.definition_digest
     AND gate_observations.evaluator_process_id = sqlc.arg(evaluator_process_id)
     AND gate_observations.state = 'passing'
     AND gate_observations.evaluated_at <= sqlc.arg(released_at)
     AND gate_observations.valid_until > sqlc.arg(released_at)
    WHERE promotion_gates.promotion_id = sqlc.arg(promotion_id)
      AND promotion_gates.released_at IS NULL
      AND repositories.removed_at IS NULL
)
UPDATE promotion_gates
SET released_at = sqlc.arg(released_at),
    release_catalog_revision_id = evidence.catalog_revision_id,
    release_definition = evidence.normalized_definition,
    release_definition_digest = evidence.definition_digest,
    release_evaluator_process_id = evidence.evaluator_process_id,
    release_observed_value = evidence.value,
    release_evaluated_at = evidence.evaluated_at,
    release_valid_until = evidence.valid_until
FROM evidence
WHERE promotion_gates.promotion_id = evidence.promotion_id
  AND promotion_gates.gate_name = evidence.gate_name;

-- name: CountUnreleasedPromotionGates :one
SELECT count(*)
FROM promotion_gates
WHERE promotion_id = sqlc.arg(promotion_id)
  AND released_at IS NULL;

-- name: CountWaitingGateSubjects :many
SELECT
    'run'::text AS subject_type,
    count(DISTINCT run_gates.run_id) AS subject_count
FROM run_gates
JOIN runs
  ON runs.id = run_gates.run_id
WHERE run_gates.released_at IS NULL
  AND runs.phase = 'queued'

UNION ALL

SELECT
    'promotion'::text AS subject_type,
    count(DISTINCT promotion_gates.promotion_id) AS subject_count
FROM promotion_gates
JOIN promotions
  ON promotions.id = promotion_gates.promotion_id
WHERE promotion_gates.released_at IS NULL
  AND promotions.status IN ('creating', 'preparing', 'pending', 'approved')
  AND promotions.dispatch_failed_at IS NULL
  AND promotions.kargo_outcome_phase IS NULL;

-- name: ListRunGateProjections :many
SELECT
    run_gates.run_id,
    run_gates.repository_id,
    run_gates.gate_name,
    attached_gate.definition AS attached_definition,
    active_revision.id AS active_catalog_revision_id,
    active_gate.definition AS active_gate_definition,
    active_gate.normalized_definition AS active_normalized_definition,
    active_gate.definition_digest AS active_definition_digest,
    gate_observations.evaluator_process_id,
    gate_observations.state AS observation_state,
    gate_observations.value AS observation_value,
    gate_observations.diagnostic AS observation_diagnostic,
    gate_observations.evaluated_at AS observation_evaluated_at,
    gate_observations.valid_until AS observation_valid_until,
    run_gates.released_at,
    run_gates.release_definition,
    run_gates.release_observed_value,
    run_gates.release_evaluated_at
FROM run_gates
JOIN runs
  ON runs.id = run_gates.run_id
JOIN catalog_revisions AS attached_revision
  ON attached_revision.id = runs.catalog_revision_id
JOIN catalog_gates AS attached_gate
  ON attached_gate.catalog_snapshot_id = attached_revision.catalog_snapshot_id
 AND attached_gate.name = run_gates.gate_name
JOIN repositories
  ON repositories.id = run_gates.repository_id
LEFT JOIN catalog_revisions AS active_revision
  ON active_revision.id = repositories.active_catalog_revision_id
LEFT JOIN catalog_gates AS active_gate
  ON active_gate.catalog_snapshot_id = active_revision.catalog_snapshot_id
 AND active_gate.name = run_gates.gate_name
LEFT JOIN gate_observations
  ON gate_observations.repository_id = run_gates.repository_id
 AND gate_observations.gate_name = run_gates.gate_name
 AND gate_observations.catalog_revision_id = active_revision.id
 AND gate_observations.definition_digest = active_gate.definition_digest
WHERE run_gates.run_id = ANY(sqlc.arg(run_ids)::uuid[])
ORDER BY run_gates.run_id, run_gates.attachment_order;

-- name: ListPromotionGateProjections :many
SELECT
    promotion_gates.promotion_id,
    promotion_gates.repository_id,
    promotion_gates.gate_name,
    active_revision.id AS active_catalog_revision_id,
    active_gate.definition AS active_gate_definition,
    active_gate.normalized_definition AS active_normalized_definition,
    active_gate.definition_digest AS active_definition_digest,
    gate_observations.evaluator_process_id,
    gate_observations.state AS observation_state,
    gate_observations.value AS observation_value,
    gate_observations.diagnostic AS observation_diagnostic,
    gate_observations.evaluated_at AS observation_evaluated_at,
    gate_observations.valid_until AS observation_valid_until,
    promotion_gates.released_at,
    release_gate.definition AS release_gate_definition,
    promotion_gates.release_definition,
    promotion_gates.release_observed_value,
    promotion_gates.release_evaluated_at
FROM promotion_gates
JOIN repositories
  ON repositories.id = promotion_gates.repository_id
LEFT JOIN catalog_revisions AS active_revision
  ON active_revision.id = repositories.active_catalog_revision_id
LEFT JOIN catalog_gates AS active_gate
  ON active_gate.catalog_snapshot_id = active_revision.catalog_snapshot_id
 AND active_gate.name = promotion_gates.gate_name
LEFT JOIN gate_observations
  ON gate_observations.repository_id = promotion_gates.repository_id
 AND gate_observations.gate_name = promotion_gates.gate_name
 AND gate_observations.catalog_revision_id = active_revision.id
 AND gate_observations.definition_digest = active_gate.definition_digest
LEFT JOIN catalog_revisions AS release_revision
  ON release_revision.id = promotion_gates.release_catalog_revision_id
LEFT JOIN catalog_gates AS release_gate
  ON release_gate.catalog_snapshot_id = release_revision.catalog_snapshot_id
 AND release_gate.name = promotion_gates.gate_name
WHERE promotion_gates.promotion_id = ANY(sqlc.arg(promotion_ids)::uuid[])
ORDER BY promotion_gates.promotion_id, promotion_gates.attachment_order;
