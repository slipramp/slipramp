-- +goose Up
CREATE TABLE controller_credentials (
    cluster_name text PRIMARY KEY REFERENCES clusters (name),
    sha256 text NOT NULL,
    enrolled_at timestamptz NOT NULL,
    CONSTRAINT controller_credentials_sha256_key UNIQUE (sha256),
    CONSTRAINT controller_credentials_sha256_valid CHECK (
        sha256 ~ '^[0-9a-f]{64}$'
    )
);

-- +goose Down
DROP TABLE controller_credentials;
