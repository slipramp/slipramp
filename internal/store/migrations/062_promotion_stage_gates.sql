-- +goose Up
ALTER TABLE promotion_stages
ADD COLUMN gate_names text[] NOT NULL DEFAULT '{}'::text[],
ADD CONSTRAINT promotion_stages_gate_names_valid CHECK (
    array_position(gate_names, NULL) IS NULL
);

ALTER TABLE promotion_stages
ALTER COLUMN gate_names DROP DEFAULT;

UPDATE promotions
SET stage_snapshot = jsonb_set(stage_snapshot, '{gates}', '[]'::jsonb)
WHERE NOT stage_snapshot ? 'gates';

-- +goose Down
UPDATE promotions
SET stage_snapshot = stage_snapshot - 'gates';

ALTER TABLE promotion_stages
DROP CONSTRAINT promotion_stages_gate_names_valid,
DROP COLUMN gate_names;
