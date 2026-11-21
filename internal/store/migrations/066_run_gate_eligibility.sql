-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION run_gates_allow_dispatch(candidate_id uuid, process_id uuid, checked_at timestamptz)
RETURNS boolean
LANGUAGE sql STABLE
AS $$
    SELECT NOT EXISTS (
        SELECT 1
        FROM run_gates AS requirement
        WHERE requirement.run_id = candidate_id
          AND requirement.released_at IS NULL
          AND NOT EXISTS (
              SELECT 1
              FROM active_passing_gate_evidence AS evidence
              WHERE evidence.repository_id = requirement.repository_id
                AND evidence.gate_name = requirement.gate_name
                AND evidence.evaluator_process_id = process_id
                AND evidence.evaluated_at <= checked_at
                AND evidence.valid_until > checked_at
          )
    );
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION run_gates_allow_dispatch(uuid, uuid, timestamptz);
