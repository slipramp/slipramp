-- +goose Up
ALTER TABLE promotion_stages
RENAME COLUMN direct_freight_json TO candidate_freight_json;

ALTER TABLE promotion_stages
RENAME CONSTRAINT promotion_stages_direct_freight_object TO promotion_stages_candidate_freight_object;

ALTER TABLE promotion_stages
ADD COLUMN stage_status_json jsonb NOT NULL DEFAULT '{}'::jsonb,
DROP CONSTRAINT promotion_stages_observation_values_valid,
DROP CONSTRAINT promotion_stages_repository_name_valid,
DROP CONSTRAINT promotion_stages_source_paths_valid,
ADD CONSTRAINT promotion_stages_observation_values_valid CHECK (
    length(kubernetes_uid) BETWEEN 1 AND 200
    AND length(resource_version) BETWEEN 1 AND 200
    AND length(application) BETWEEN 0 AND 200
    AND length(environment) BETWEEN 0 AND 200
    AND length(repository_url) BETWEEN 0 AND 2048
    AND length(target_branch) BETWEEN 0 AND 255
),
ADD CONSTRAINT promotion_stages_repository_name_valid CHECK (
    repository_name = ''
    OR repository_name ~ '^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$'
),
ADD CONSTRAINT promotion_stages_source_paths_valid CHECK (
    array_position(source_paths, NULL) IS NULL
),
ADD CONSTRAINT promotion_stages_status_object CHECK (
    jsonb_typeof(stage_status_json) = 'object'
);

ALTER TABLE promotion_stages
ALTER COLUMN stage_status_json DROP DEFAULT;

-- +goose Down
ALTER TABLE promotion_stages
DROP CONSTRAINT promotion_stages_status_object,
DROP CONSTRAINT promotion_stages_source_paths_valid,
DROP CONSTRAINT promotion_stages_repository_name_valid,
DROP CONSTRAINT promotion_stages_observation_values_valid,
DROP COLUMN stage_status_json,
ADD CONSTRAINT promotion_stages_observation_values_valid CHECK (
    length(kubernetes_uid) BETWEEN 1 AND 200
    AND length(resource_version) BETWEEN 1 AND 200
    AND length(application) BETWEEN 1 AND 200
    AND length(environment) BETWEEN 1 AND 200
    AND length(repository_url) BETWEEN 1 AND 2048
    AND length(target_branch) BETWEEN 1 AND 255
),
ADD CONSTRAINT promotion_stages_repository_name_valid CHECK (
    repository_name ~ '^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$'
),
ADD CONSTRAINT promotion_stages_source_paths_valid CHECK (
    cardinality(source_paths) > 0
    AND array_position(source_paths, NULL) IS NULL
);

ALTER TABLE promotion_stages
RENAME CONSTRAINT promotion_stages_candidate_freight_object TO promotion_stages_direct_freight_object;

ALTER TABLE promotion_stages
RENAME COLUMN candidate_freight_json TO direct_freight_json;
