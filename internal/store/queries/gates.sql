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

-- name: DeleteInactiveGateObservations :execrows
DELETE FROM gate_observations
WHERE gate_observations.valid_until < sqlc.arg(inactive_before)
  AND NOT EXISTS (
      SELECT 1
      FROM gate_evaluation_demand AS active
      WHERE active.repository_id = gate_observations.repository_id
        AND active.gate_name = gate_observations.gate_name
        AND active.catalog_revision_id = gate_observations.catalog_revision_id
        AND active.definition_digest = gate_observations.definition_digest
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

-- name: ListGateEvaluationDemand :many
SELECT repository_id, gate_name, catalog_revision_id, spec, definition_digest, waiter_count, last_requested_at
FROM gate_evaluation_demand
ORDER BY repository_id, gate_name, catalog_revision_id;

-- name: ReleaseRunGates :execrows
UPDATE run_gates
SET released_at = sqlc.arg(released_at),
    release_catalog_revision_id = evidence.catalog_revision_id,
    release_definition = evidence.spec,
    release_definition_digest = evidence.definition_digest,
    release_evaluator_process_id = evidence.evaluator_process_id,
    release_observed_value = evidence.value,
    release_evaluated_at = evidence.evaluated_at,
    release_valid_until = evidence.valid_until
FROM active_passing_gate_evidence AS evidence
WHERE run_gates.run_id = sqlc.arg(run_id)
  AND run_gates.released_at IS NULL
  AND evidence.repository_id = run_gates.repository_id
  AND evidence.gate_name = run_gates.gate_name
  AND evidence.evaluator_process_id = sqlc.arg(evaluator_process_id)
  AND evidence.evaluated_at <= sqlc.arg(released_at)
  AND evidence.valid_until > sqlc.arg(released_at);

-- name: ReleasePromotionGates :execrows
UPDATE promotion_gates
SET released_at = sqlc.arg(released_at),
    release_catalog_revision_id = evidence.catalog_revision_id,
    release_definition = evidence.spec,
    release_definition_digest = evidence.definition_digest,
    release_evaluator_process_id = evidence.evaluator_process_id,
    release_observed_value = evidence.value,
    release_evaluated_at = evidence.evaluated_at,
    release_valid_until = evidence.valid_until
FROM active_passing_gate_evidence AS evidence
WHERE promotion_gates.promotion_id = sqlc.arg(promotion_id)
  AND promotion_gates.released_at IS NULL
  AND evidence.repository_id = promotion_gates.repository_id
  AND evidence.gate_name = promotion_gates.gate_name
  AND evidence.evaluator_process_id = sqlc.arg(evaluator_process_id)
  AND evidence.evaluated_at <= sqlc.arg(released_at)
  AND evidence.valid_until > sqlc.arg(released_at);

-- name: CountUnreleasedPromotionGates :one
SELECT count(*)
FROM promotion_gates
WHERE promotion_id = sqlc.arg(promotion_id)
  AND released_at IS NULL;

-- name: CountWaitingGateSubjects :many
SELECT subject_type, count(DISTINCT subject_id) AS subject_count
FROM waiting_gate_requirements
GROUP BY subject_type;

-- name: ListGateRequirementEvidence :many
WITH requirements AS (
    SELECT
        run_gates.run_id AS subject_id,
        run_gates.repository_id,
        run_gates.gate_name,
        run_gates.attachment_order,
        runs.catalog_revision_id AS attached_revision_id,
        (runs.cancellation_requested_at IS NOT NULL OR runs.outcome IS NOT DISTINCT FROM 'canceled') AS canceled,
        run_gates.released_at,
        run_gates.release_catalog_revision_id,
        run_gates.release_definition,
        run_gates.release_observed_value,
        run_gates.release_evaluated_at
    FROM run_gates
    JOIN runs ON runs.id = run_gates.run_id
    WHERE run_gates.run_id = ANY(sqlc.arg(run_ids)::uuid[])

    UNION ALL

    SELECT
        pg.promotion_id, pg.repository_id, pg.gate_name, pg.attachment_order, NULL::uuid,
        promotions.cancellation_requested_at IS NOT NULL,
        pg.released_at, pg.release_catalog_revision_id, pg.release_definition,
        pg.release_observed_value, pg.release_evaluated_at
    FROM promotion_gates AS pg
    JOIN promotions ON promotions.id = pg.promotion_id
    WHERE pg.promotion_id = ANY(sqlc.arg(promotion_ids)::uuid[])
)
SELECT
    requirements.subject_id,
    requirements.gate_name,
    requirements.canceled::boolean AS canceled,
    COALESCE(COALESCE(active_gate.definition, historical_gate.definition)->'metadata'->>'title', '')::text AS title,
    (
        COALESCE(requirements.release_definition, active_gate.definition->'spec')->'prometheus'->'condition'
    )::jsonb AS condition,
    requirements.released_at,
    requirements.release_observed_value,
    observation.value AS observed_value,
    COALESCE(requirements.release_evaluated_at, observation.evaluated_at)::timestamptz AS evaluated_at,
    observation.evaluator_process_id,
    observation.state AS observation_state,
    observation.diagnostic AS observation_diagnostic,
    observation.valid_until
FROM requirements
JOIN repositories ON repositories.id = requirements.repository_id
LEFT JOIN catalog_revisions AS active_revision
  ON active_revision.id = repositories.active_catalog_revision_id
 AND repositories.removed_at IS NULL
 AND requirements.released_at IS NULL
 AND NOT requirements.canceled
LEFT JOIN catalog_gates AS active_gate
  ON active_gate.catalog_snapshot_id = active_revision.catalog_snapshot_id
 AND active_gate.name = requirements.gate_name
LEFT JOIN catalog_revisions AS historical_revision
  ON active_gate.name IS NULL
 AND historical_revision.id = COALESCE(requirements.release_catalog_revision_id, requirements.attached_revision_id)
LEFT JOIN catalog_gates AS historical_gate
  ON historical_gate.catalog_snapshot_id = historical_revision.catalog_snapshot_id
 AND historical_gate.name = requirements.gate_name
LEFT JOIN gate_observations AS observation
  ON observation.repository_id = requirements.repository_id
 AND observation.gate_name = requirements.gate_name
 AND observation.catalog_revision_id = active_revision.id
 AND observation.definition_digest = active_gate.definition_digest
ORDER BY requirements.subject_id, requirements.attachment_order;
