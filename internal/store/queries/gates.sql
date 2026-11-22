-- name: UpsertGateObservation :exec
INSERT INTO gate_observations (
    repository_id,
    gate_name,
    catalog_revision_id,
    definition_digest,
    evaluator_process_id,
    state,
    evidence,
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
    sqlc.narg(evidence),
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
    evidence = EXCLUDED.evidence,
    diagnostic = EXCLUDED.diagnostic,
    evaluated_at = EXCLUDED.evaluated_at,
    valid_until = EXCLUDED.valid_until;

-- name: RecordRunGateObservation :exec
UPDATE run_gates SET
    current_catalog_revision_id = sqlc.arg(catalog_revision_id),
    current_definition_digest = sqlc.arg(definition_digest),
    current_evaluator_process_id = sqlc.arg(evaluator_process_id),
    current_state = sqlc.arg(state),
    current_evidence = sqlc.narg(evidence),
    current_diagnostic = sqlc.narg(diagnostic),
    current_evaluated_at = sqlc.arg(evaluated_at),
    current_valid_until = sqlc.arg(valid_until)
-- Serialize observation replacement with release, override, and cancellation on this subject.
WHERE run_gates.run_id = (
    SELECT subject.id FROM runs AS subject WHERE subject.id = sqlc.arg(subject_id) FOR NO KEY UPDATE
)
  AND run_gates.gate_name = sqlc.arg(gate_name)
  AND run_gates.repository_id = sqlc.arg(repository_id) AND released_at IS NULL
  AND (current_catalog_revision_id IS DISTINCT FROM sqlc.arg(catalog_revision_id)
       OR current_evaluated_at < sqlc.arg(evaluated_at))
  AND EXISTS (
      SELECT 1 FROM subject_gate_evaluation_demand AS w
      WHERE w.subject_type = 'run' AND w.subject_id = run_gates.run_id
        AND w.gate_name = run_gates.gate_name AND w.catalog_revision_id = sqlc.arg(catalog_revision_id)
        AND w.definition_digest = sqlc.arg(definition_digest)
  );

-- name: RecordPromotionGateObservation :exec
UPDATE promotion_gates SET
    current_catalog_revision_id = sqlc.arg(catalog_revision_id),
    current_definition_digest = sqlc.arg(definition_digest),
    current_evaluator_process_id = sqlc.arg(evaluator_process_id),
    current_state = sqlc.arg(state),
    current_evidence = sqlc.narg(evidence),
    current_diagnostic = sqlc.narg(diagnostic),
    current_evaluated_at = sqlc.arg(evaluated_at),
    current_valid_until = sqlc.arg(valid_until)
-- Serialize observation replacement with release, override, and cancellation on this subject.
WHERE promotion_gates.promotion_id = (
    SELECT subject.id FROM promotions AS subject WHERE subject.id = sqlc.arg(subject_id) FOR NO KEY UPDATE
)
  AND promotion_gates.gate_name = sqlc.arg(gate_name)
  AND promotion_gates.repository_id = sqlc.arg(repository_id) AND released_at IS NULL
  AND (current_catalog_revision_id IS DISTINCT FROM sqlc.arg(catalog_revision_id)
       OR current_evaluated_at < sqlc.arg(evaluated_at))
  AND EXISTS (
      SELECT 1 FROM subject_gate_evaluation_demand AS w
      WHERE w.subject_type = 'promotion' AND w.subject_id = promotion_gates.promotion_id
        AND w.gate_name = promotion_gates.gate_name AND w.catalog_revision_id = sqlc.arg(catalog_revision_id)
        AND w.definition_digest = sqlc.arg(definition_digest)
  );

-- name: DeleteInactiveGateObservations :one
WITH shared AS (
    DELETE FROM gate_observations
    WHERE gate_observations.valid_until < sqlc.arg(inactive_before)
      AND NOT EXISTS (
          SELECT 1
          FROM gate_evaluation_demand AS active
          WHERE active.repository_id = gate_observations.repository_id
            AND active.gate_name = gate_observations.gate_name
            AND active.catalog_revision_id = gate_observations.catalog_revision_id
            AND active.definition_digest = gate_observations.definition_digest
      )
    RETURNING 1
), cleared_run AS (
    UPDATE run_gates AS g
    SET current_catalog_revision_id = NULL, current_definition_digest = NULL,
        current_evaluator_process_id = NULL, current_state = NULL, current_evidence = NULL,
        current_diagnostic = NULL, current_evaluated_at = NULL, current_valid_until = NULL
    WHERE g.current_valid_until < sqlc.arg(inactive_before)
      AND NOT EXISTS (
          SELECT 1 FROM subject_gate_evaluation_demand AS w
          WHERE w.subject_type = 'run' AND w.subject_id = g.run_id AND w.gate_name = g.gate_name
            AND w.catalog_revision_id = g.current_catalog_revision_id AND w.definition_digest = g.current_definition_digest
      )
    RETURNING 1
), cleared_promotion AS (
    UPDATE promotion_gates AS g
    SET current_catalog_revision_id = NULL, current_definition_digest = NULL,
        current_evaluator_process_id = NULL, current_state = NULL, current_evidence = NULL,
        current_diagnostic = NULL, current_evaluated_at = NULL, current_valid_until = NULL
    WHERE g.current_valid_until < sqlc.arg(inactive_before)
      AND NOT EXISTS (
          SELECT 1 FROM subject_gate_evaluation_demand AS w
          WHERE w.subject_type = 'promotion' AND w.subject_id = g.promotion_id AND w.gate_name = g.gate_name
            AND w.catalog_revision_id = g.current_catalog_revision_id AND w.definition_digest = g.current_definition_digest
      )
    RETURNING 1
)
SELECT ((SELECT count(*) FROM shared) + (SELECT count(*) FROM cleared_run)
    + (SELECT count(*) FROM cleared_promotion))::bigint AS deleted;

-- name: LockRunGateRepository :one
-- A refresh can hold the Repository while updating the locked Target or Run.
-- Defer this claim to the next Controller poll when a writer already holds it.
SELECT id FROM repositories WHERE id = sqlc.arg(id) FOR SHARE SKIP LOCKED;

-- name: ShareRunGateScopes :many
SELECT * FROM server_gate_scopes WHERE kind = 'global' OR target_id = sqlc.arg(target_id)
ORDER BY (kind <> 'global') FOR SHARE;

-- name: SharePromotionGateScopes :many
SELECT * FROM server_gate_scopes WHERE kind = 'global' OR stage_id = sqlc.arg(stage_id)
ORDER BY (kind <> 'global') FOR SHARE;

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
FOR SHARE;

-- name: ListGateEvaluationDemand :many
WITH demand AS (
    SELECT d.repository_id, d.gate_name, d.catalog_revision_id, d.spec, d.definition_digest,
        d.waiter_count, d.last_requested_at, NULL::jsonb AS subject, ''::text AS repository_name, ''::text AS commit_sha
    FROM gate_evaluation_demand AS d
    UNION ALL
    SELECT w.repository_id, w.gate_name, w.catalog_revision_id, w.spec, w.definition_digest, 1, w.requested_at,
        jsonb_build_object('type', w.subject_type, 'id', w.subject_id) ||
        CASE WHEN w.subject_type = 'run' THEN jsonb_build_object('run', jsonb_build_object(
            'targetId', r.target_id, 'targetName', r.target_name, 'actionName', r.action_name,
            'intent', r.snapshot #>> '{action,Intent}', 'catalogRevisionId', r.catalog_revision_id, 'commitSha', r.commit_sha))
        ELSE jsonb_build_object('promotion', jsonb_build_object('stageId', p.stage_id, 'integration', p.integration,
            'cluster', p.stage_snapshot->>'cluster', 'project', p.stage_snapshot->>'project', 'stageName', p.stage_snapshot->>'name'))
        END, w.repository_name, w.commit_sha
    FROM subject_gate_evaluation_demand AS w
    LEFT JOIN runs AS r ON w.subject_type = 'run' AND w.subject_id = r.id
    LEFT JOIN promotions AS p ON w.subject_type = 'promotion' AND w.subject_id = p.id
)
SELECT repository_id, gate_name, catalog_revision_id, spec, definition_digest, waiter_count, last_requested_at,
    subject, repository_name, commit_sha
FROM demand ORDER BY repository_id, gate_name, catalog_revision_id, subject->>'type', subject->>'id';

-- name: ReleaseRunGates :execrows
UPDATE run_gates
SET released_at = sqlc.arg(released_at),
    current_catalog_revision_id = NULL, current_definition_digest = NULL,
    current_evaluator_process_id = NULL, current_state = NULL, current_evidence = NULL,
    current_diagnostic = NULL, current_evaluated_at = NULL, current_valid_until = NULL,
    release_catalog_revision_id = evidence.catalog_revision_id,
    release_definition = evidence.spec,
    release_definition_digest = evidence.definition_digest,
    release_evaluator_process_id = evidence.evaluator_process_id,
    release_evidence = evidence.evidence,
    release_evaluated_at = evidence.evaluated_at,
    release_valid_until = evidence.valid_until
FROM eligible_gate_evidence(sqlc.arg(evaluator_process_id)::uuid, sqlc.arg(released_at)::timestamptz) AS evidence
WHERE run_gates.run_id = sqlc.arg(run_id)
  AND run_gates.released_at IS NULL
  AND evidence.subject_type = 'run'
  AND evidence.subject_id = run_gates.run_id
  AND evidence.gate_name = run_gates.gate_name;

-- name: ReleasePromotionGates :execrows
UPDATE promotion_gates
SET released_at = sqlc.arg(released_at),
    current_catalog_revision_id = NULL, current_definition_digest = NULL,
    current_evaluator_process_id = NULL, current_state = NULL, current_evidence = NULL,
    current_diagnostic = NULL, current_evaluated_at = NULL, current_valid_until = NULL,
    release_catalog_revision_id = evidence.catalog_revision_id,
    release_definition = evidence.spec,
    release_definition_digest = evidence.definition_digest,
    release_evaluator_process_id = evidence.evaluator_process_id,
    release_evidence = evidence.evidence,
    release_evaluated_at = evidence.evaluated_at,
    release_valid_until = evidence.valid_until
FROM eligible_gate_evidence(sqlc.arg(evaluator_process_id)::uuid, sqlc.arg(released_at)::timestamptz) AS evidence
WHERE promotion_gates.promotion_id = sqlc.arg(promotion_id)
  AND promotion_gates.released_at IS NULL
  AND evidence.subject_type = 'promotion'
  AND evidence.subject_id = promotion_gates.promotion_id
  AND evidence.gate_name = promotion_gates.gate_name;

-- name: CountWaitingGateSubjects :many
SELECT subject_type, count(DISTINCT subject_id) AS subject_count
FROM (
    SELECT subject_type, subject_id FROM waiting_gate_requirements
    UNION ALL
    SELECT 'run', run_id FROM run_server_gate_checks(sqlc.arg(checked_at)::timestamptz)
    UNION ALL
    SELECT 'promotion', promotion_id FROM promotion_server_gate_checks(sqlc.arg(checked_at)::timestamptz)
) requirements
GROUP BY subject_type;

-- name: ListGateRequirementEvidence :many
WITH requirements AS (
    SELECT
        'run'::text AS subject_type, run_gates.run_id AS subject_id,
        run_gates.repository_id,
        run_gates.gate_name,
        run_gates.attachment_order,
        runs.catalog_revision_id AS attached_revision_id,
        (runs.cancellation_requested_at IS NOT NULL OR runs.outcome IS NOT DISTINCT FROM 'canceled') AS canceled,
        run_gates.overrides, run_gates.release_definition_digest,
        run_gates.released_at,
        run_gates.release_catalog_revision_id,
        run_gates.release_definition,
        run_gates.release_evidence,
        run_gates.release_evaluated_at,
        run_gates.release_valid_until
    FROM run_gates
    JOIN runs ON runs.id = run_gates.run_id
    WHERE run_gates.run_id = ANY(sqlc.arg(run_ids)::uuid[])

    UNION ALL

    SELECT
        'promotion', pg.promotion_id, pg.repository_id, pg.gate_name, pg.attachment_order, NULL::uuid,
        promotions.cancellation_requested_at IS NOT NULL,
        pg.overrides, pg.release_definition_digest,
        pg.released_at, pg.release_catalog_revision_id, pg.release_definition,
        pg.release_evidence, pg.release_evaluated_at, pg.release_valid_until
    FROM promotion_gates AS pg
    JOIN promotions ON promotions.id = pg.promotion_id
    WHERE pg.promotion_id = ANY(sqlc.arg(promotion_ids)::uuid[])
)
SELECT
    requirements.repository_id,
    requirements.subject_id,
    requirements.gate_name,
    requirements.canceled::boolean AS canceled,
    requirements.overrides,
    COALESCE(requirements.release_definition_digest, active_gate.definition_digest, '')::text AS definition_digest,
    COALESCE(COALESCE(active_gate.definition, historical_gate.definition)->'metadata'->>'title', '')::text AS title,
    COALESCE(requirements.release_definition, active_gate.definition->'spec')::jsonb AS definition,
    requirements.released_at,
    requirements.release_evidence,
    observation.evidence AS observed_evidence,
    COALESCE(requirements.release_evaluated_at, observation.evaluated_at)::timestamptz AS evaluated_at,
    observation.evaluator_process_id,
    observation.state AS observation_state,
    observation.diagnostic AS observation_diagnostic,
    COALESCE(requirements.release_valid_until, observation.valid_until)::timestamptz AS valid_until
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
LEFT JOIN gate_current_observations AS observation
  ON observation.subject_type = CASE WHEN active_gate.subject_scoped THEN requirements.subject_type ELSE 'shared' END
    AND (observation.subject_type = 'shared' OR observation.subject_id = requirements.subject_id)
 AND observation.repository_id = requirements.repository_id
 AND observation.gate_name = requirements.gate_name
 AND observation.catalog_revision_id = active_revision.id
 AND observation.definition_digest = active_gate.definition_digest
ORDER BY requirements.subject_id, requirements.attachment_order;

-- name: RecordRunGateOverride :execrows
UPDATE run_gates
SET overrides = overrides || sqlc.arg(decision)::jsonb
WHERE run_gates.run_id = sqlc.arg(run_id)
  AND run_gates.gate_name = sqlc.arg(gate_name) AND run_gates.released_at IS NULL
  AND EXISTS (
      SELECT 1 FROM waiting_gate_requirements AS waiting
      JOIN subject_gate_evidence AS evidence USING (subject_type, subject_id, gate_name)
      WHERE waiting.subject_type = 'run' AND waiting.subject_id = run_gates.run_id
        AND waiting.gate_name = run_gates.gate_name
        AND evidence.definition_digest = sqlc.arg(definition_digest)
        AND NOT EXISTS (
            SELECT 1 FROM eligible_gate_evidence(sqlc.arg(process_id)::uuid, sqlc.arg(checked_at)::timestamptz) AS eligible
            WHERE eligible.subject_type = evidence.subject_type AND eligible.subject_id = evidence.subject_id
                AND eligible.gate_name = evidence.gate_name
        )
  );

-- name: RecordPromotionGateOverride :execrows
UPDATE promotion_gates
SET overrides = overrides || sqlc.arg(decision)::jsonb
WHERE promotion_gates.promotion_id = sqlc.arg(promotion_id)
  AND promotion_gates.gate_name = sqlc.arg(gate_name) AND promotion_gates.released_at IS NULL
  AND EXISTS (
      SELECT 1 FROM waiting_gate_requirements AS waiting
      JOIN subject_gate_evidence AS evidence USING (subject_type, subject_id, gate_name)
      WHERE waiting.subject_type = 'promotion' AND waiting.subject_id = promotion_gates.promotion_id
        AND waiting.gate_name = promotion_gates.gate_name
        AND evidence.definition_digest = sqlc.arg(definition_digest)
        AND NOT EXISTS (
            SELECT 1 FROM eligible_gate_evidence(sqlc.arg(process_id)::uuid, sqlc.arg(checked_at)::timestamptz) AS eligible
            WHERE eligible.subject_type = evidence.subject_type AND eligible.subject_id = evidence.subject_id
                AND eligible.gate_name = evidence.gate_name
        )
  );

-- name: ListWaitingSubjectGateScopes :many
WITH waiting AS (
    SELECT id, target_id, NULL::uuid AS stage_id FROM runs
    WHERE id = ANY(sqlc.arg(run_ids)::uuid[]) AND phase = 'queued'
      AND snapshot #>> '{action,Intent}' IN ('operation','reconcile')
    UNION ALL
    SELECT id, NULL::uuid, stage_id FROM waiting_promotion_gate_subjects
    WHERE id = ANY(sqlc.arg(promotion_ids)::uuid[])
)
SELECT waiting.id AS subject_id, sqlc.embed(server_gate_scopes)
FROM waiting JOIN server_gate_scopes ON kind = 'global'
    OR server_gate_scopes.target_id = waiting.target_id OR server_gate_scopes.stage_id = waiting.stage_id
WHERE window_definition IS NOT NULL OR
    (locked_at IS NOT NULL AND (lock_expires_at IS NULL OR lock_expires_at > sqlc.arg(checked_at)::timestamptz))
ORDER BY waiting.id, (kind <> 'global');

-- name: ListSubjectServerGateReleases :many
SELECT id, server_gate_release_evidence FROM runs
WHERE id = ANY(sqlc.arg(run_ids)::uuid[]) AND server_gate_release_evidence IS NOT NULL
UNION ALL
SELECT id, server_gate_release_evidence FROM promotions
WHERE id = ANY(sqlc.arg(promotion_ids)::uuid[]) AND server_gate_release_evidence IS NOT NULL;
