-- +goose Up
CREATE INDEX artifacts_retention_sealed_at
ON artifacts (sealed_at, id)
WHERE state = 'sealed' AND retention_state = 'retained';

-- +goose Down
DROP INDEX artifacts_retention_sealed_at;
