-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION runs_awaiting_gates(process_id uuid, checked_at timestamptz)
RETURNS TABLE (run_id uuid)
LANGUAGE sql STABLE AS $$
    SELECT requirement.subject_id
    FROM waiting_gate_requirements AS requirement
    WHERE requirement.subject_type = 'run'
      AND NOT EXISTS (
          SELECT 1 FROM eligible_gate_evidence(process_id, checked_at) AS evidence
          WHERE evidence.subject_type = 'run'
            AND evidence.subject_id = requirement.subject_id
            AND evidence.gate_name = requirement.gate_name
      )
    UNION ALL
    SELECT c.run_id FROM run_server_gate_checks(checked_at) c WHERE NOT (c.passing OR c.overridden);
$$;
-- +goose StatementEnd

-- Candidate selection and the locked assignment both apply the same capacity and Gate checks.
-- +goose StatementBegin
CREATE FUNCTION assignable_cluster_runs(wanted_cluster text, cluster_limit integer, placement_limits jsonb,
    process_id uuid, checked_at timestamptz)
RETURNS TABLE(run_id uuid) LANGUAGE sql STABLE AS $$
WITH active_runs AS (
    SELECT active.placement_name
    FROM runs AS active
    WHERE active.cluster_name = wanted_cluster
      AND active.phase IN ('assigned', 'preparing', 'running')
)
    SELECT queued.id
    FROM runs AS queued
    JOIN targets AS target_state
      ON target_state.id = queued.target_id
    WHERE queued.cluster_name = wanted_cluster
      AND queued.phase = 'queued'
      AND queued.review_state IN ('not-required', 'approved')
      AND queued.id NOT IN (
          SELECT run_id FROM runs_awaiting_gates(process_id, checked_at)
      )
      AND (
          SELECT count(*)
          FROM active_runs
      ) < cluster_limit
      AND (
          NOT (placement_limits ? queued.placement_name)
          OR COALESCE((
              SELECT count(*)
              FROM active_runs
              WHERE placement_name = queued.placement_name
          ), 0) < (placement_limits ->> queued.placement_name)::bigint
      )
      AND (
          SELECT count(*)
          FROM runs AS active
          WHERE active.target_id = queued.target_id
            AND active.phase IN ('assigned', 'preparing', 'running')
      ) < target_state.maximum_active_runs;
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION assignable_cluster_runs(text, integer, jsonb, uuid, timestamptz);
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION runs_awaiting_gates(process_id uuid, checked_at timestamptz)
RETURNS TABLE (run_id uuid)
LANGUAGE sql STABLE AS $$
    SELECT requirement.subject_id
    FROM waiting_gate_requirements AS requirement
    WHERE requirement.subject_type = 'run'
      AND NOT EXISTS (
          SELECT 1 FROM eligible_gate_evidence(process_id, checked_at) AS evidence
          WHERE evidence.subject_type = 'run'
            AND evidence.subject_id = requirement.subject_id
            AND evidence.gate_name = requirement.gate_name
      );
$$;
-- +goose StatementEnd
