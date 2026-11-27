-- +goose Up
CREATE TABLE promotion_pipeline_search (
    stage_id uuid PRIMARY KEY REFERENCES promotion_stages (id) ON DELETE CASCADE,
    name text NOT NULL
);

-- Match the connected components, anchors and names in promotion.ProjectPipelines.
WITH RECURSIVE active AS (
    SELECT id, integration, cluster_name, project, name COLLATE "C" AS name,
           application COLLATE "C" AS application, requested_freight_json
    FROM promotion_stages WHERE archived_at IS NULL
), links AS (
    SELECT downstream.id AS downstream_id, upstream.id AS upstream_id
    FROM active downstream
    CROSS JOIN LATERAL jsonb_array_elements(downstream.requested_freight_json) AS request(value)
    CROSS JOIN LATERAL jsonb_array_elements_text(nullif(request.value->'sourceStages', 'null'::jsonb)) AS source(name)
    JOIN active upstream
      ON (upstream.integration, upstream.cluster_name, upstream.project, upstream.name) =
         (downstream.integration, downstream.cluster_name, downstream.project, source.name)
), edges AS (
    SELECT downstream_id AS src, upstream_id AS dst FROM links
    UNION ALL
    SELECT upstream_id, downstream_id FROM links
), reachable(stage_id, member_id) AS (
    SELECT id, id FROM active
    UNION
    SELECT reachable.stage_id, edges.dst FROM reachable JOIN edges ON edges.src = reachable.member_id
), members AS (
    SELECT reachable.stage_id, min(active.name) AS anchor
    FROM reachable JOIN active ON active.id = reachable.member_id
    GROUP BY reachable.stage_id
), pipelines AS (
    SELECT integration, cluster_name, project, anchor, count(*) AS stage_count,
           count(DISTINCT nullif(application, '')) AS applications, min(nullif(application, '')) AS application,
           row_number() OVER (PARTITION BY integration, cluster_name, project ORDER BY anchor) AS ordinal
    FROM members JOIN active ON active.id = members.stage_id
    GROUP BY integration, cluster_name, project, anchor
)
INSERT INTO promotion_pipeline_search (stage_id, name)
SELECT active.id, CASE
    WHEN applications > 1 THEN format('%s + %s more', pipelines.application, applications - 1)
    WHEN applications = 1 THEN pipelines.application
    WHEN stage_count = 1 THEN anchor
    ELSE format('Pipeline %s', ordinal)
END
FROM pipelines JOIN active USING (integration, cluster_name, project)
WHERE active.name = anchor;

-- +goose Down
DROP TABLE promotion_pipeline_search;
