-- +goose Up
ALTER TABLE repositories
ADD COLUMN commit_url_template text,
ADD CONSTRAINT repositories_commit_url_template_not_empty CHECK (
    commit_url_template IS NULL OR length(commit_url_template) > 0
);

CREATE TABLE repository_commits (
    repository_id uuid NOT NULL REFERENCES repositories (id),
    sha text NOT NULL,
    title text,
    author_name text,
    authored_at timestamptz,
    PRIMARY KEY (repository_id, sha),
    CONSTRAINT repository_commits_sha_valid CHECK (
        sha ~ '^(?:[0-9a-f]{40}|[0-9a-f]{64})$'
    ),
    CONSTRAINT repository_commits_metadata_consistent CHECK (
        (title IS NULL AND author_name IS NULL AND authored_at IS NULL)
        OR (title IS NOT NULL AND author_name IS NOT NULL AND authored_at IS NOT NULL)
    )
);

INSERT INTO repository_commits (repository_id, sha)
SELECT repository_id, commit_sha
FROM catalog_revisions
UNION
SELECT repository_id, commit_sha
FROM catalog_validation_failures
UNION
SELECT id, observed_commit_sha
FROM repositories
WHERE observed_commit_sha IS NOT NULL
UNION
SELECT repository_id, commit_sha
FROM runs
UNION
SELECT repository_id, base_revision
FROM promotions
UNION
SELECT repository_id, candidate_revision
FROM promotions
ON CONFLICT DO NOTHING;

-- +goose Down
DROP TABLE repository_commits;

ALTER TABLE repositories
DROP CONSTRAINT repositories_commit_url_template_not_empty,
DROP COLUMN commit_url_template;
