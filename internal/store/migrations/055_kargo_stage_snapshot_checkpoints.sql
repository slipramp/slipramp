-- +goose Up
DELETE FROM kargo_stage_snapshots
WHERE snapshot_id IN (
    SELECT snapshot_id
    FROM (
        SELECT
            snapshot_id,
            row_number() OVER (
                PARTITION BY integration, cluster_name
                ORDER BY observed_at DESC, accepted_at DESC, snapshot_id DESC
            ) AS checkpoint_order
        FROM kargo_stage_snapshots
    ) ordered_snapshots
    WHERE checkpoint_order > 1
);

DROP INDEX kargo_stage_snapshots_latest;

ALTER TABLE kargo_stage_snapshots
DROP CONSTRAINT kargo_stage_snapshots_pkey,
ADD CONSTRAINT kargo_stage_snapshots_pkey PRIMARY KEY (integration, cluster_name),
ADD CONSTRAINT kargo_stage_snapshots_snapshot_id_key UNIQUE (snapshot_id);

-- +goose Down
ALTER TABLE kargo_stage_snapshots
DROP CONSTRAINT kargo_stage_snapshots_pkey,
DROP CONSTRAINT kargo_stage_snapshots_snapshot_id_key,
ADD CONSTRAINT kargo_stage_snapshots_pkey PRIMARY KEY (snapshot_id);

CREATE INDEX kargo_stage_snapshots_latest
ON kargo_stage_snapshots (integration, accepted_at DESC, snapshot_id DESC);
