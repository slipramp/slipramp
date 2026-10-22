-- +goose Up
ALTER TABLE api_keys
DROP CONSTRAINT api_keys_scopes_valid,
ADD CONSTRAINT api_keys_scopes_valid CHECK (
    scopes <@ ARRAY[
        'slipramp:read',
        'slipramp:runs:write',
        'slipramp:promotions:submit',
        'slipramp:promotions:write',
        'slipramp:admin'
    ]::text[]
);

CREATE TABLE promotion_stages (
    id uuid PRIMARY KEY,
    integration text NOT NULL,
    cluster_name text NOT NULL,
    project text NOT NULL,
    name text NOT NULL,
    kubernetes_uid text NOT NULL,
    resource_version text NOT NULL,
    application text NOT NULL,
    environment text NOT NULL,
    repository_name text NOT NULL,
    repository_url text NOT NULL,
    target_branch text NOT NULL,
    source_paths text[] NOT NULL,
    freight_json jsonb NOT NULL,
    successful_render_acknowledgement jsonb,
    first_observed_at timestamptz NOT NULL,
    last_observed_at timestamptz NOT NULL,
    archived_at timestamptz,
    CONSTRAINT promotion_stages_identity_key UNIQUE (integration, project, name),
    CONSTRAINT promotion_stages_integration_valid CHECK (
        integration ~ '^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$'
    ),
    CONSTRAINT promotion_stages_cluster_valid CHECK (
        cluster_name ~ '^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$'
    ),
    CONSTRAINT promotion_stages_project_valid CHECK (
        project ~ '^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$'
    ),
    CONSTRAINT promotion_stages_name_valid CHECK (
        name ~ '^[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?$'
    ),
    CONSTRAINT promotion_stages_observation_values_valid CHECK (
        length(kubernetes_uid) BETWEEN 1 AND 200
        AND length(resource_version) BETWEEN 1 AND 200
        AND length(application) BETWEEN 1 AND 200
        AND length(environment) BETWEEN 1 AND 200
        AND length(repository_url) BETWEEN 1 AND 2048
        AND length(target_branch) BETWEEN 1 AND 255
    ),
    CONSTRAINT promotion_stages_repository_name_valid CHECK (
        repository_name ~ '^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$'
    ),
    CONSTRAINT promotion_stages_source_paths_valid CHECK (
        cardinality(source_paths) > 0
        AND array_position(source_paths, NULL) IS NULL
    ),
    CONSTRAINT promotion_stages_freight_object CHECK (
        jsonb_typeof(freight_json) = 'object'
    ),
    CONSTRAINT promotion_stages_render_ack_object CHECK (
        successful_render_acknowledgement IS NULL
        OR jsonb_typeof(successful_render_acknowledgement) = 'object'
    ),
    CONSTRAINT promotion_stages_observation_times_valid CHECK (
        last_observed_at >= first_observed_at
        AND (archived_at IS NULL OR archived_at >= last_observed_at)
    )
);

CREATE INDEX promotion_stages_inventory
ON promotion_stages (integration, archived_at, project, name);

CREATE TABLE promotion_stage_policies (
    stage_id uuid PRIMARY KEY REFERENCES promotion_stages (id),
    mode text NOT NULL,
    expires_at timestamptz,
    reason text,
    updated_by_principal_id uuid NOT NULL REFERENCES principals (id),
    updated_by_snapshot jsonb NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT promotion_stage_policies_mode_valid CHECK (
        mode IN ('automatic', 'review-required')
    ),
    CONSTRAINT promotion_stage_policies_expiry_valid CHECK (
        (mode = 'automatic' AND expires_at IS NULL)
        OR (mode = 'review-required' AND (expires_at IS NULL OR expires_at > updated_at))
    ),
    CONSTRAINT promotion_stage_policies_reason_valid CHECK (
        reason IS NULL OR length(reason) BETWEEN 1 AND 4096
    ),
    CONSTRAINT promotion_stage_policies_actor_valid CHECK (
        jsonb_typeof(updated_by_snapshot) = 'object'
        AND updated_by_snapshot->>'principalId' = updated_by_principal_id::text
    )
);

CREATE TABLE kargo_stage_snapshots (
    snapshot_id uuid PRIMARY KEY,
    integration text NOT NULL,
    cluster_name text NOT NULL,
    observed_at timestamptz NOT NULL,
    accepted_at timestamptz NOT NULL,
    CONSTRAINT kargo_stage_snapshots_integration_valid CHECK (
        integration ~ '^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$'
    ),
    CONSTRAINT kargo_stage_snapshots_cluster_valid CHECK (
        cluster_name ~ '^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$'
    ),
    CONSTRAINT kargo_stage_snapshots_times_valid CHECK (accepted_at >= observed_at)
);

CREATE INDEX kargo_stage_snapshots_latest
ON kargo_stage_snapshots (integration, accepted_at DESC, snapshot_id DESC);

CREATE TABLE promotions (
    id uuid PRIMARY KEY,
    integration text NOT NULL,
    external_promotion_id text NOT NULL,
    request_digest bytea NOT NULL,
    stage_id uuid NOT NULL REFERENCES promotion_stages (id),
    stage_snapshot jsonb NOT NULL,
    repository_id uuid NOT NULL REFERENCES repositories (id),
    repository_snapshot jsonb NOT NULL,
    base_revision text NOT NULL,
    candidate_revision text NOT NULL,
    candidate_branch text NOT NULL,
    target_branch text NOT NULL,
    policy_snapshot jsonb NOT NULL,
    review_required boolean NOT NULL,
    status text NOT NULL,
    message text,
    preparation_repository_id uuid REFERENCES repositories (id),
    preparation_catalog_revision_id uuid,
    preparation_run_id uuid UNIQUE REFERENCES runs (id),
    git_diff_artifact_id uuid UNIQUE REFERENCES artifacts (id),
    argo_diff_artifact_id uuid UNIQUE REFERENCES artifacts (id),
    requested_by_principal_id uuid NOT NULL REFERENCES principals (id),
    requested_by_snapshot jsonb NOT NULL,
    created_at timestamptz NOT NULL,
    preparation_completed_at timestamptz,
    decided_at timestamptz,
    expires_at timestamptz,
    updated_at timestamptz NOT NULL,
    CONSTRAINT promotions_preparation_catalog_revision_fk FOREIGN KEY (
        preparation_catalog_revision_id,
        preparation_repository_id
    ) REFERENCES catalog_revisions (id, repository_id),
    CONSTRAINT promotions_external_identity_key UNIQUE (integration, external_promotion_id),
    CONSTRAINT promotions_integration_valid CHECK (
        integration ~ '^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$'
    ),
    CONSTRAINT promotions_external_id_valid CHECK (
        length(external_promotion_id) BETWEEN 1 AND 200
    ),
    CONSTRAINT promotions_request_digest_valid CHECK (octet_length(request_digest) = 32),
    CONSTRAINT promotions_snapshots_valid CHECK (
        jsonb_typeof(stage_snapshot) = 'object'
        AND jsonb_typeof(repository_snapshot) = 'object'
        AND jsonb_typeof(policy_snapshot) = 'object'
    ),
    CONSTRAINT promotions_revisions_valid CHECK (
        base_revision ~ '^(?:[0-9a-f]{40}|[0-9a-f]{64})$'
        AND candidate_revision ~ '^(?:[0-9a-f]{40}|[0-9a-f]{64})$'
    ),
    CONSTRAINT promotions_branches_valid CHECK (
        length(candidate_branch) BETWEEN 17 AND 255
        AND candidate_branch LIKE 'kargo/promotion/%'
        AND length(target_branch) BETWEEN 1 AND 255
    ),
    CONSTRAINT promotions_status_valid CHECK (
        status IN ('preparing', 'pending', 'approved', 'rejected', 'expired', 'error')
    ),
    CONSTRAINT promotions_message_valid CHECK (
        message IS NULL OR length(message) BETWEEN 1 AND 4096
    ),
    CONSTRAINT promotions_actor_valid CHECK (
        jsonb_typeof(requested_by_snapshot) = 'object'
        AND requested_by_snapshot->>'principalId' = requested_by_principal_id::text
    ),
    CONSTRAINT promotions_times_valid CHECK (
        updated_at >= created_at
        AND (preparation_completed_at IS NULL OR preparation_completed_at >= created_at)
        AND (decided_at IS NULL OR decided_at >= created_at)
        AND (expires_at IS NULL OR expires_at > created_at)
    ),
    CONSTRAINT promotions_automatic_state_valid CHECK (
        review_required
        OR (
            status = 'approved'
            AND preparation_repository_id IS NULL
            AND preparation_catalog_revision_id IS NULL
            AND preparation_run_id IS NULL
            AND git_diff_artifact_id IS NULL
            AND argo_diff_artifact_id IS NULL
            AND preparation_completed_at IS NULL
            AND decided_at IS NULL
        )
    ),
    CONSTRAINT promotions_preparation_pin_consistent CHECK (
        (preparation_repository_id IS NULL) =
        (preparation_catalog_revision_id IS NULL)
    ),
    CONSTRAINT promotions_preparation_run_pin_consistent CHECK (
        preparation_run_id IS NULL
        OR preparation_catalog_revision_id IS NOT NULL
    ),
    CONSTRAINT promotions_preparation_state_valid CHECK (
        (
            status = 'preparing'
            AND preparation_completed_at IS NULL
            AND git_diff_artifact_id IS NULL
            AND argo_diff_artifact_id IS NULL
            AND decided_at IS NULL
        )
        OR (
            status IN ('pending', 'approved', 'rejected', 'expired')
            AND review_required
            AND preparation_run_id IS NOT NULL
            AND preparation_completed_at IS NOT NULL
            AND git_diff_artifact_id IS NOT NULL
            AND argo_diff_artifact_id IS NOT NULL
            AND ((status IN ('approved', 'rejected')) = (decided_at IS NOT NULL))
        )
        OR (
            status = 'error'
            AND review_required
            AND decided_at IS NULL
        )
        OR (
            status = 'approved'
            AND NOT review_required
        )
    )
);

CREATE INDEX promotions_created_at
ON promotions (created_at DESC, id DESC);

CREATE INDEX promotions_status_created_at
ON promotions (status, created_at DESC, id DESC);

CREATE TABLE promotion_decisions (
    promotion_id uuid PRIMARY KEY REFERENCES promotions (id),
    decision text NOT NULL,
    comment text,
    decided_by_principal_id uuid NOT NULL REFERENCES principals (id),
    decided_by_snapshot jsonb NOT NULL,
    decided_at timestamptz NOT NULL,
    CONSTRAINT promotion_decisions_value_valid CHECK (
        decision IN ('approved', 'rejected')
    ),
    CONSTRAINT promotion_decisions_comment_valid CHECK (
        comment IS NULL OR length(comment) BETWEEN 1 AND 4096
    ),
    CONSTRAINT promotion_decisions_actor_valid CHECK (
        jsonb_typeof(decided_by_snapshot) = 'object'
        AND decided_by_snapshot->>'principalId' = decided_by_principal_id::text
    )
);

-- +goose Down
DROP TABLE promotion_decisions;
DROP TABLE promotions;
DROP TABLE kargo_stage_snapshots;
DROP TABLE promotion_stage_policies;
DROP TABLE promotion_stages;

ALTER TABLE api_keys
DROP CONSTRAINT api_keys_scopes_valid,
ADD CONSTRAINT api_keys_scopes_valid CHECK (
    scopes <@ ARRAY[
        'slipramp:read',
        'slipramp:runs:write',
        'slipramp:admin'
    ]::text[]
);
