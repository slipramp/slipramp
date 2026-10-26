-- +goose Up
ALTER TABLE runs
ADD COLUMN review_comment text,
ADD CONSTRAINT runs_review_comment_valid CHECK (
    review_comment IS NULL
    OR (
        review_decided_at IS NOT NULL
        AND length(review_comment) BETWEEN 1 AND 4096
        AND review_comment = btrim(review_comment)
    )
);

-- +goose Down
ALTER TABLE runs
DROP CONSTRAINT runs_review_comment_valid,
DROP COLUMN review_comment;
