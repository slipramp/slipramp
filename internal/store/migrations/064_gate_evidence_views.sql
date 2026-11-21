-- +goose Up
-- Callers match the evaluator process and validity window at the release instant.
CREATE VIEW active_passing_gate_evidence AS
SELECT
    observation.repository_id,
    observation.gate_name,
    observation.catalog_revision_id,
    observation.definition_digest,
    (definition.definition->'spec')::jsonb AS spec,
    observation.evaluator_process_id,
    observation.value,
    observation.evaluated_at,
    observation.valid_until
FROM gate_observations AS observation
JOIN repositories
  ON repositories.id = observation.repository_id
 AND repositories.active_catalog_revision_id = observation.catalog_revision_id
JOIN catalog_revisions ON catalog_revisions.id = observation.catalog_revision_id
JOIN catalog_gates AS definition
  ON definition.catalog_snapshot_id = catalog_revisions.catalog_snapshot_id
 AND definition.name = observation.gate_name
 AND definition.definition_digest = observation.definition_digest
WHERE repositories.removed_at IS NULL
  AND observation.state = 'passing';

CREATE VIEW gate_evaluation_demand AS
WITH requirements AS (
    SELECT
        run_gates.repository_id,
        run_gates.gate_name,
        GREATEST(runs.created_at, runs.review_decided_at) AS requested_at
    FROM run_gates
    JOIN runs
      ON runs.id = run_gates.run_id
    WHERE run_gates.released_at IS NULL
      AND runs.phase = 'queued'

    UNION ALL

    SELECT
        promotion_gates.repository_id,
        promotion_gates.gate_name,
        GREATEST(promotions.created_at, promotions.decided_at) AS requested_at
    FROM promotion_gates
    JOIN promotions
      ON promotions.id = promotion_gates.promotion_id
    WHERE promotion_gates.released_at IS NULL
      AND promotions.status IN ('creating', 'preparing', 'pending', 'approved')
      AND promotions.dispatch_failed_at IS NULL
      AND promotions.kargo_outcome_phase IS NULL
), demand AS (
    SELECT repository_id, gate_name, count(*) AS waiter_count, max(requested_at) AS last_requested_at
    FROM requirements
    GROUP BY repository_id, gate_name
)
SELECT
    demand.repository_id,
    demand.gate_name,
    demand.waiter_count,
    demand.last_requested_at::timestamptz AS last_requested_at,
    repositories.active_catalog_revision_id AS catalog_revision_id,
    (catalog_gates.definition->'spec')::jsonb AS spec,
    catalog_gates.definition_digest
FROM demand
JOIN repositories
  ON repositories.id = demand.repository_id
JOIN catalog_revisions
  ON catalog_revisions.id = repositories.active_catalog_revision_id
JOIN catalog_gates
  ON catalog_gates.catalog_snapshot_id = catalog_revisions.catalog_snapshot_id
 AND catalog_gates.name = demand.gate_name
WHERE repositories.removed_at IS NULL;

-- +goose Down
DROP VIEW gate_evaluation_demand;
DROP VIEW active_passing_gate_evidence;
