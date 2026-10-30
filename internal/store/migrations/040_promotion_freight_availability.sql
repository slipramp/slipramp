-- +goose Up
ALTER TABLE promotion_stages
ADD COLUMN auto_promotion_enabled boolean NOT NULL DEFAULT false,
ADD COLUMN direct_freight_json jsonb NOT NULL DEFAULT '{"items":[]}'::jsonb,
ADD CONSTRAINT promotion_stages_direct_freight_object CHECK (
    jsonb_typeof(direct_freight_json) = 'object'
);

ALTER TABLE promotion_stages
ALTER COLUMN auto_promotion_enabled DROP DEFAULT,
ALTER COLUMN direct_freight_json DROP DEFAULT;

-- +goose Down
ALTER TABLE promotion_stages
DROP CONSTRAINT promotion_stages_direct_freight_object,
DROP COLUMN direct_freight_json,
DROP COLUMN auto_promotion_enabled;
