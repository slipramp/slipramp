-- +goose Up
ALTER TABLE repositories
ADD COLUMN commit_indexing_mode text NOT NULL DEFAULT 'observedHeads',
ADD CONSTRAINT repositories_commit_indexing_mode_valid CHECK (
    commit_indexing_mode IN ('disabled', 'observedHeads', 'trackedBranchHistory')
);

-- +goose Down
ALTER TABLE repositories
DROP CONSTRAINT repositories_commit_indexing_mode_valid,
DROP COLUMN commit_indexing_mode;
