-- +goose Up
CREATE VIEW waiting_gate_requirements AS
SELECT
    'run'::text AS subject_type,
    runs.id AS subject_id,
    run_gates.repository_id,
    run_gates.gate_name,
    GREATEST(runs.created_at, runs.review_decided_at) AS requested_at
FROM run_gates
JOIN runs ON runs.id = run_gates.run_id
WHERE run_gates.released_at IS NULL
  AND runs.phase = 'queued'

UNION ALL

SELECT
    'promotion'::text AS subject_type,
    promotions.id AS subject_id,
    promotion_gates.repository_id,
    promotion_gates.gate_name,
    GREATEST(promotions.created_at, promotions.decided_at) AS requested_at
FROM promotion_gates
JOIN promotions ON promotions.id = promotion_gates.promotion_id
WHERE promotion_gates.released_at IS NULL
  AND promotions.status IN ('creating', 'preparing', 'pending', 'approved')
  AND promotions.dispatch_failed_at IS NULL
  AND promotions.kargo_outcome_phase IS NULL;

CREATE OR REPLACE VIEW gate_evaluation_demand AS
WITH demand AS (
    SELECT repository_id, gate_name, count(*) AS waiter_count, max(requested_at) AS last_requested_at
    FROM waiting_gate_requirements
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
CREATE OR REPLACE VIEW gate_evaluation_demand AS
WITH requirements AS (
    SELECT
        run_gates.repository_id,
        run_gates.gate_name,
        GREATEST(runs.created_at, runs.review_decided_at) AS requested_at
    FROM run_gates
    JOIN runs ON runs.id = run_gates.run_id
    WHERE run_gates.released_at IS NULL
      AND runs.phase = 'queued'

    UNION ALL

    SELECT
        promotion_gates.repository_id,
        promotion_gates.gate_name,
        GREATEST(promotions.created_at, promotions.decided_at) AS requested_at
    FROM promotion_gates
    JOIN promotions ON promotions.id = promotion_gates.promotion_id
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

DROP VIEW waiting_gate_requirements;
