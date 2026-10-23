-- +goose Up
ALTER TABLE runs
ADD COLUMN rerun_of_run_id uuid REFERENCES runs (id),
ADD COLUMN rerun_mode text,
ADD CONSTRAINT runs_rerun_consistent CHECK (
    (rerun_of_run_id IS NULL) = (rerun_mode IS NULL)
),
ADD CONSTRAINT runs_rerun_not_self CHECK (
    rerun_of_run_id IS NULL OR rerun_of_run_id <> id
),
ADD CONSTRAINT runs_rerun_mode_valid CHECK (
    rerun_mode IS NULL OR rerun_mode IN ('exact', 'latest')
);

CREATE INDEX runs_rerun_of_run_id
ON runs (rerun_of_run_id)
WHERE rerun_of_run_id IS NOT NULL;

-- +goose Down
DROP INDEX runs_rerun_of_run_id;

ALTER TABLE runs
DROP CONSTRAINT runs_rerun_mode_valid,
DROP CONSTRAINT runs_rerun_not_self,
DROP CONSTRAINT runs_rerun_consistent,
DROP COLUMN rerun_mode,
DROP COLUMN rerun_of_run_id;
