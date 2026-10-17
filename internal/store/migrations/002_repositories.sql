-- +goose Up
CREATE TABLE repositories (
    id uuid PRIMARY KEY,
    name text NOT NULL,
    clone_url text NOT NULL,
    branch text NOT NULL,
    refresh_credential_name text,
    checkout_credential_name text,
    created_at timestamptz NOT NULL,
    removed_at timestamptz,
    CONSTRAINT repositories_name_not_empty CHECK (length(name) > 0),
    CONSTRAINT repositories_clone_url_not_empty CHECK (length(clone_url) > 0),
    CONSTRAINT repositories_branch_not_empty CHECK (length(branch) > 0),
    CONSTRAINT repositories_refresh_credential_name_not_empty CHECK (
        refresh_credential_name IS NULL OR length(refresh_credential_name) > 0
    ),
    CONSTRAINT repositories_checkout_credential_name_not_empty CHECK (
        checkout_credential_name IS NULL OR length(checkout_credential_name) > 0
    ),
    CONSTRAINT repositories_removed_after_creation CHECK (
        removed_at IS NULL OR removed_at >= created_at
    )
);

CREATE UNIQUE INDEX repositories_active_name_key
ON repositories (name)
WHERE removed_at IS NULL;

-- +goose Down
DROP TABLE repositories;
