-- name: SearchTargets :many
WITH active AS (
    SELECT t.id, t.name, r.name AS repository,
           coalesce(nullif(ct.definition->'metadata'->>'title', ''), t.name)::text AS title
    FROM targets t
    JOIN repositories r ON r.id = t.repository_id
    JOIN catalog_revisions cr ON cr.id = t.catalog_revision_id
    JOIN catalog_targets ct ON ct.catalog_snapshot_id = cr.catalog_snapshot_id AND ct.name = t.name
    WHERE t.archived_at IS NULL AND r.removed_at IS NULL
)
SELECT id, name, repository, title
FROM active
WHERE NOT EXISTS (
    SELECT 1 FROM unnest(sqlc.arg(terms)::text[]) AS term(value)
    WHERE strpos(lower(concat_ws(' ', name, title, repository)), term.value) = 0
)
ORDER BY CASE
    WHEN lower(name) = sqlc.arg(query)::text OR lower(title) = sqlc.arg(query) THEN 0
    WHEN starts_with(lower(name), sqlc.arg(query)) OR starts_with(lower(title), sqlc.arg(query)) THEN 1
    ELSE 2 END,
    lower(title), repository, name, id
LIMIT sqlc.arg(result_limit);

-- name: SearchPromotionInventory :many
WITH scopes AS (
    SELECT unnest(sqlc.arg(integrations)::text[]) AS integration,
           unnest(sqlc.arg(clusters)::text[]) AS cluster_name
), active AS (
    SELECT s.id, s.name, s.project, s.cluster_name, s.integration
    FROM promotion_stages s JOIN scopes USING (integration, cluster_name)
    WHERE s.archived_at IS NULL
), candidates AS (
    SELECT false AS is_pipeline, s.name, s.id AS stage_id, s.name AS stage_name, s.project, s.cluster_name, s.integration
    FROM active s
    UNION ALL
    SELECT true, p.name, s.id, s.name, s.project, s.cluster_name, s.integration
    FROM promotion_pipeline_search p JOIN active s ON s.id = p.stage_id
), ranked AS (
    SELECT c.*, row_number() OVER (PARTITION BY is_pipeline ORDER BY
        CASE WHEN lower(name) = sqlc.arg(query)::text THEN 0
             WHEN starts_with(lower(name), sqlc.arg(query)) THEN 1 ELSE 2 END,
        lower(name) COLLATE "C", project COLLATE "C", cluster_name COLLATE "C", integration COLLATE "C",
        stage_name COLLATE "C", stage_id
    ) AS position
    FROM candidates c
    WHERE NOT EXISTS (
        SELECT 1 FROM unnest(sqlc.arg(terms)::text[]) AS term(value)
        WHERE strpos(lower(concat_ws(' ', c.name, c.stage_name, c.project, c.cluster_name, c.integration)), term.value) = 0
    )
)
SELECT is_pipeline, name, stage_id, stage_name, project, cluster_name, integration
FROM ranked WHERE position <= sqlc.arg(result_limit)::int
ORDER BY is_pipeline, position;

-- name: DeleteSnapshotPipelineSearch :exec
DELETE FROM promotion_pipeline_search p USING promotion_stages s
WHERE p.stage_id = s.id AND s.integration = sqlc.arg(integration) AND s.cluster_name = sqlc.arg(cluster_name);

-- name: InsertSnapshotPipelineSearch :exec
WITH anchors AS (
    SELECT unnest(sqlc.arg(projects)::text[]) AS project,
           unnest(sqlc.arg(stage_names)::text[]) AS stage_name,
           unnest(sqlc.arg(pipeline_names)::text[]) AS pipeline_name
)
INSERT INTO promotion_pipeline_search (stage_id, name)
SELECT s.id, a.pipeline_name
FROM anchors a JOIN promotion_stages s ON s.project = a.project AND s.name = a.stage_name
WHERE s.integration = sqlc.arg(integration) AND s.cluster_name = sqlc.arg(cluster_name) AND s.archived_at IS NULL;
