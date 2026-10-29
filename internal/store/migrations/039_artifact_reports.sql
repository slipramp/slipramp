-- +goose Up
CREATE TABLE artifact_reports (
    artifact_id uuid PRIMARY KEY REFERENCES artifacts (id),
    kind text NOT NULL,
    projection_schema_version integer NOT NULL,
    adapter_version integer NOT NULL,
    tool text NOT NULL,
    tool_version text,
    mode text NOT NULL,
    summary jsonb NOT NULL,
    projection_object_key text NOT NULL,
    projection_size_bytes bigint NOT NULL,
    projection_sha256 text NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT artifact_reports_kind_valid CHECK (kind IN ('terraform-plan')),
    CONSTRAINT artifact_reports_projection_schema_version_positive CHECK (
        projection_schema_version > 0
    ),
    CONSTRAINT artifact_reports_adapter_version_positive CHECK (adapter_version > 0),
    CONSTRAINT artifact_reports_tool_valid CHECK (length(tool) BETWEEN 1 AND 64),
    CONSTRAINT artifact_reports_tool_version_valid CHECK (
        tool_version IS NULL OR length(tool_version) BETWEEN 1 AND 128
    ),
    CONSTRAINT artifact_reports_mode_valid CHECK (mode IN ('preview')),
    CONSTRAINT artifact_reports_summary_bounded CHECK (octet_length(summary::text) <= 65536),
    CONSTRAINT artifact_reports_projection_object_key_key UNIQUE (projection_object_key),
    CONSTRAINT artifact_reports_projection_object_key_valid CHECK (
        length(projection_object_key) > 0 AND left(projection_object_key, 1) <> '/'
    ),
    CONSTRAINT artifact_reports_projection_size_valid CHECK (
        projection_size_bytes BETWEEN 1 AND 16777216
    ),
    CONSTRAINT artifact_reports_projection_sha256_valid CHECK (
        projection_sha256 ~ '^[0-9a-f]{64}$'
    )
);

-- +goose Down
DROP TABLE artifact_reports;
