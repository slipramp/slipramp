-- +goose Up
ALTER TABLE targets
ADD COLUMN maximum_active_runs integer NOT NULL DEFAULT 1,
ADD CONSTRAINT targets_maximum_active_runs_positive CHECK (maximum_active_runs > 0);

DROP INDEX runs_one_active_per_target;

-- +goose Down
CREATE UNIQUE INDEX runs_one_active_per_target
ON runs (target_id)
WHERE phase IN ('assigned', 'preparing', 'running');

ALTER TABLE targets
DROP CONSTRAINT targets_maximum_active_runs_positive,
DROP COLUMN maximum_active_runs;
