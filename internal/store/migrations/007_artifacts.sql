-- +goose Up
CREATE TABLE artifacts (
    id uuid PRIMARY KEY,
    run_id uuid NOT NULL REFERENCES runs (id),
    name text NOT NULL,
    kind text NOT NULL,
    state text NOT NULL,
    media_type text NOT NULL,
    size_bytes bigint NOT NULL,
    sha256 text,
    sensitive boolean NOT NULL,
    content_object_key text,
    retention_state text NOT NULL,
    created_at timestamptz NOT NULL,
    sealed_at timestamptz,
    CONSTRAINT artifacts_run_name_key UNIQUE (run_id, name),
    CONSTRAINT artifacts_content_object_key_key UNIQUE (content_object_key),
    CONSTRAINT artifacts_name_valid CHECK (
        name ~ '^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$'
    ),
    CONSTRAINT artifacts_kind_valid CHECK (kind IN ('log', 'output')),
    CONSTRAINT artifacts_state_valid CHECK (state IN ('open', 'sealed', 'failed')),
    CONSTRAINT artifacts_media_type_not_empty CHECK (length(media_type) > 0),
    CONSTRAINT artifacts_size_nonnegative CHECK (size_bytes >= 0),
    CONSTRAINT artifacts_sha256_valid CHECK (
        sha256 IS NULL OR sha256 ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT artifacts_object_key_valid CHECK (
        content_object_key IS NULL OR (
            length(content_object_key) > 0
            AND left(content_object_key, 1) <> '/'
        )
    ),
    CONSTRAINT artifacts_retention_state_valid CHECK (
        retention_state IN ('retained', 'expired', 'deleted')
    ),
    CONSTRAINT artifacts_sealed_metadata_consistent CHECK (
        (
            state = 'sealed'
            AND sha256 IS NOT NULL
            AND sealed_at IS NOT NULL
            AND sealed_at >= created_at
        ) OR (
            state <> 'sealed'
            AND sha256 IS NULL
            AND sealed_at IS NULL
        )
    )
);

CREATE INDEX artifacts_run_created_at
ON artifacts (run_id, created_at, id);

CREATE TABLE artifact_log_chunks (
    artifact_id uuid NOT NULL REFERENCES artifacts (id),
    offset_bytes bigint NOT NULL,
    size_bytes bigint NOT NULL,
    sha256 text NOT NULL,
    object_key text NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT artifact_log_chunks_pkey PRIMARY KEY (artifact_id, offset_bytes),
    CONSTRAINT artifact_log_chunks_object_key_key UNIQUE (object_key),
    CONSTRAINT artifact_log_chunks_offset_nonnegative CHECK (offset_bytes >= 0),
    CONSTRAINT artifact_log_chunks_size_positive CHECK (size_bytes > 0),
    CONSTRAINT artifact_log_chunks_end_offset_valid CHECK (
        offset_bytes <= 9223372036854775807 - size_bytes
    ),
    CONSTRAINT artifact_log_chunks_sha256_valid CHECK (
        sha256 ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT artifact_log_chunks_object_key_valid CHECK (
        length(object_key) > 0 AND left(object_key, 1) <> '/'
    )
);

-- +goose Down
DROP TABLE artifact_log_chunks;
DROP TABLE artifacts;
