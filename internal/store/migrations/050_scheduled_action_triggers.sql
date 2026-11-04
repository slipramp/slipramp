-- +goose Up
ALTER TABLE runs
DROP CONSTRAINT runs_source_valid;

ALTER TABLE runs
ADD CONSTRAINT runs_source_valid CHECK ((
    (
        source->>'type' IN ('manual', 'api', 'mcp')
        AND source = jsonb_build_object('type', source->>'type')
    )
    OR (
        source->>'type' = 'git'
        AND length(source->'git'->>'triggerName') > 0
        AND source->'git'->>'deliveryId' ~
            '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
        AND idempotency_key = source->'git'->>'deliveryId'
        AND source = jsonb_build_object(
            'type',
            'git',
            'git',
            jsonb_build_object(
                'triggerName',
                source->'git'->>'triggerName',
                'deliveryId',
                source->'git'->>'deliveryId'
            )
        )
    )
    OR (
        source->>'type' = 'schedule'
        AND length(source->'schedule'->>'triggerName') > 0
        AND source->'schedule'->>'scheduledAt' ~
            '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:00Z$'
        AND idempotency_key ~ '^[0-9a-f]{64}$'
        AND source = jsonb_build_object(
            'type',
            'schedule',
            'schedule',
            jsonb_build_object(
                'triggerName',
                source->'schedule'->>'triggerName',
                'scheduledAt',
                source->'schedule'->>'scheduledAt'
            )
        )
    )
    OR (
        source->>'type' = 'promotion'
        AND source->'promotion'->>'promotionId' ~
            '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
        AND idempotency_key = source->'promotion'->>'promotionId'
        AND source = jsonb_build_object(
            'type',
            'promotion',
            'promotion',
            jsonb_build_object(
                'promotionId',
                source->'promotion'->>'promotionId'
            )
        )
    )
) IS TRUE),
ADD CONSTRAINT runs_schedule_source_has_no_creator CHECK (
    source->>'type' <> 'schedule' OR created_by_principal_id IS NULL
);

-- +goose Down
DELETE FROM runs
WHERE source->>'type' = 'schedule';

ALTER TABLE runs
DROP CONSTRAINT runs_schedule_source_has_no_creator,
DROP CONSTRAINT runs_source_valid;

ALTER TABLE runs
ADD CONSTRAINT runs_source_valid CHECK ((
    (
        source->>'type' IN ('manual', 'api', 'mcp')
        AND source = jsonb_build_object('type', source->>'type')
    )
    OR (
        source->>'type' = 'git'
        AND length(source->'git'->>'triggerName') > 0
        AND source->'git'->>'deliveryId' ~
            '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
        AND idempotency_key = source->'git'->>'deliveryId'
        AND source = jsonb_build_object(
            'type',
            'git',
            'git',
            jsonb_build_object(
                'triggerName',
                source->'git'->>'triggerName',
                'deliveryId',
                source->'git'->>'deliveryId'
            )
        )
    )
    OR (
        source->>'type' = 'promotion'
        AND source->'promotion'->>'promotionId' ~
            '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
        AND idempotency_key = source->'promotion'->>'promotionId'
        AND source = jsonb_build_object(
            'type',
            'promotion',
            'promotion',
            jsonb_build_object(
                'promotionId',
                source->'promotion'->>'promotionId'
            )
        )
    )
) IS TRUE);
