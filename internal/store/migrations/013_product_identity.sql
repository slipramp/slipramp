-- +goose Up
CREATE TABLE principals (
    id uuid PRIMARY KEY,
    kind text NOT NULL,
    display_name text NOT NULL,
    role text NOT NULL,
    disabled_at timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT principals_kind_valid CHECK (
        kind IN ('user', 'service-account')
    ),
    CONSTRAINT principals_display_name_valid CHECK (
        length(display_name) BETWEEN 1 AND 200
        AND display_name = btrim(display_name)
    ),
    CONSTRAINT principals_role_valid CHECK (
        role IN ('admin', 'operator', 'viewer')
    ),
    CONSTRAINT principals_timestamps_valid CHECK (updated_at >= created_at),
    CONSTRAINT principals_disabled_at_valid CHECK (
        disabled_at IS NULL OR disabled_at >= created_at
    )
);

CREATE TABLE oidc_identities (
    id uuid PRIMARY KEY,
    principal_id uuid NOT NULL REFERENCES principals (id),
    issuer text NOT NULL,
    subject text NOT NULL,
    email text,
    preferred_username text,
    display_name text,
    last_authenticated_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT oidc_identities_issuer_subject_key UNIQUE (issuer, subject),
    CONSTRAINT oidc_identities_issuer_valid CHECK (
        length(issuer) BETWEEN 1 AND 2048
    ),
    CONSTRAINT oidc_identities_subject_valid CHECK (
        length(subject) BETWEEN 1 AND 1024
    ),
    CONSTRAINT oidc_identities_timestamps_valid CHECK (
        updated_at >= created_at
        AND last_authenticated_at >= created_at
    )
);

CREATE TABLE api_keys (
    id uuid PRIMARY KEY,
    principal_id uuid NOT NULL REFERENCES principals (id),
    created_by_principal_id uuid NOT NULL REFERENCES principals (id),
    name text NOT NULL,
    token_prefix text NOT NULL,
    token_digest bytea NOT NULL,
    scopes text[] NOT NULL,
    created_at timestamptz NOT NULL,
    expires_at timestamptz,
    last_used_at timestamptz,
    revoked_at timestamptz,
    CONSTRAINT api_keys_token_prefix_key UNIQUE (token_prefix),
    CONSTRAINT api_keys_token_digest_key UNIQUE (token_digest),
    CONSTRAINT api_keys_name_valid CHECK (
        length(name) BETWEEN 1 AND 200 AND name = btrim(name)
    ),
    CONSTRAINT api_keys_token_prefix_valid CHECK (
        length(token_prefix) BETWEEN 1 AND 64
    ),
    CONSTRAINT api_keys_token_digest_valid CHECK (
        octet_length(token_digest) = 32
    ),
    CONSTRAINT api_keys_scopes_valid CHECK (
        scopes <@ ARRAY[
            'slipramp:read',
            'slipramp:runs:write',
            'slipramp:admin'
        ]::text[]
    ),
    CONSTRAINT api_keys_expiry_valid CHECK (
        expires_at IS NULL OR expires_at > created_at
    ),
    CONSTRAINT api_keys_last_used_valid CHECK (
        last_used_at IS NULL OR last_used_at >= created_at
    ),
    CONSTRAINT api_keys_revoked_valid CHECK (
        revoked_at IS NULL OR revoked_at >= created_at
    )
);

CREATE TABLE web_sessions (
    id uuid PRIMARY KEY,
    principal_id uuid NOT NULL REFERENCES principals (id),
    token_digest bytea NOT NULL,
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL,
    CONSTRAINT web_sessions_token_digest_key UNIQUE (token_digest),
    CONSTRAINT web_sessions_token_digest_valid CHECK (
        octet_length(token_digest) = 32
    ),
    CONSTRAINT web_sessions_expiry_valid CHECK (expires_at > created_at),
    CONSTRAINT web_sessions_last_seen_valid CHECK (
        last_seen_at >= created_at AND last_seen_at <= expires_at
    )
);

ALTER TABLE runs
ADD COLUMN created_by_principal_id uuid REFERENCES principals (id),
ADD COLUMN created_by_snapshot jsonb,
ADD COLUMN canceled_by_principal_id uuid REFERENCES principals (id),
ADD COLUMN canceled_by_snapshot jsonb,
ADD CONSTRAINT runs_creator_consistent CHECK (
    (created_by_principal_id IS NULL) = (created_by_snapshot IS NULL)
),
ADD CONSTRAINT runs_creator_snapshot_object CHECK (
    created_by_snapshot IS NULL OR jsonb_typeof(created_by_snapshot) = 'object'
),
ADD CONSTRAINT runs_git_source_has_no_creator CHECK (
    source->>'type' <> 'git' OR created_by_principal_id IS NULL
),
ADD CONSTRAINT runs_canceler_consistent CHECK (
    (canceled_by_principal_id IS NULL) = (canceled_by_snapshot IS NULL)
),
ADD CONSTRAINT runs_canceler_snapshot_object CHECK (
    canceled_by_snapshot IS NULL OR jsonb_typeof(canceled_by_snapshot) = 'object'
);

DROP INDEX runs_source_idempotency_key;

CREATE UNIQUE INDEX runs_authenticated_idempotency_key
ON runs (created_by_principal_id, idempotency_key)
WHERE created_by_principal_id IS NOT NULL AND idempotency_key IS NOT NULL;

CREATE UNIQUE INDEX runs_unauthenticated_idempotency_key
ON runs ((source->>'type'), idempotency_key)
WHERE created_by_principal_id IS NULL AND idempotency_key IS NOT NULL;

-- +goose Down
DROP INDEX runs_unauthenticated_idempotency_key;
DROP INDEX runs_authenticated_idempotency_key;

CREATE UNIQUE INDEX runs_source_idempotency_key
ON runs ((source->>'type'), idempotency_key)
WHERE idempotency_key IS NOT NULL;

ALTER TABLE runs
DROP CONSTRAINT runs_canceler_snapshot_object,
DROP CONSTRAINT runs_canceler_consistent,
DROP CONSTRAINT runs_git_source_has_no_creator,
DROP CONSTRAINT runs_creator_snapshot_object,
DROP CONSTRAINT runs_creator_consistent,
DROP COLUMN canceled_by_snapshot,
DROP COLUMN canceled_by_principal_id,
DROP COLUMN created_by_snapshot,
DROP COLUMN created_by_principal_id;

DROP TABLE web_sessions;
DROP TABLE api_keys;
DROP TABLE oidc_identities;
DROP TABLE principals;
