-- +goose Up
ALTER TABLE runs
ADD COLUMN action_image_id text,
ADD COLUMN checkout_image_id text,
ADD COLUMN harness_image_id text,
ADD CONSTRAINT runs_action_image_id_valid CHECK (
    action_image_id IS NULL OR length(action_image_id) BETWEEN 1 AND 2048
),
ADD CONSTRAINT runs_checkout_image_id_valid CHECK (
    checkout_image_id IS NULL OR length(checkout_image_id) BETWEEN 1 AND 2048
),
ADD CONSTRAINT runs_harness_image_id_valid CHECK (
    harness_image_id IS NULL OR length(harness_image_id) BETWEEN 1 AND 2048
);

-- +goose Down
ALTER TABLE runs
DROP COLUMN action_image_id,
DROP COLUMN checkout_image_id,
DROP COLUMN harness_image_id;
