-- +goose Up
CREATE INDEX runs_review_history_idx
ON runs (review_state, created_at DESC, id DESC);

-- +goose Down
DROP INDEX runs_review_history_idx;
