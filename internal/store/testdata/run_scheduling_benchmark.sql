-- Seed 4,096 finished, 16 active and 512 queued Runs across 64 Targets and two Clusters.
WITH samples AS (
    SELECT n,
           CASE WHEN n <= 4096 THEN 'finished' WHEN n <= 4112 THEN 'running' ELSE 'queued' END AS phase,
           (n - 1 + (n - 1) / 64) % 8 AS placement,
           timestamptz '2026-01-02 00:00:00+00' + n * interval '1 second' AS created_at
    FROM generate_series(1, 4624) AS n
)
INSERT INTO runs (
    id, phase, outcome, repository_id, target_id, target_name, catalog_revision_id, commit_sha,
    action_name, parameters, placement_name, cluster_name, source, request_digest, snapshot,
    review_state, created_at, assigned_at, finished_at
)
SELECT md5(n::text)::uuid, phase, CASE WHEN phase = 'finished' THEN 'succeeded' END,
       targets.repository_id, targets.id, targets.name, targets.catalog_revision_id, targets.desired_commit_sha,
       'preview', '{}', 'placement-' || placement, 'cluster-' || (placement % 2), '{"type":"manual"}',
       repeat('a', 64), '{"action":{"Intent":"preview"}}', 'not-required', samples.created_at,
       CASE WHEN phase <> 'queued' THEN samples.created_at END,
       CASE WHEN phase = 'finished' THEN samples.created_at END
FROM samples
JOIN targets ON targets.name = 'target-' || (1 + (n - 1) % 64);

INSERT INTO run_gates (run_id, repository_id, gate_name, attachment_order)
SELECT runs.id, runs.repository_id, 'network-idle', 0
FROM generate_series(4113, 4624) AS n
JOIN runs ON runs.id = md5(n::text)::uuid
WHERE n % 17 = 0;

ANALYZE runs;
ANALYZE targets;
ANALYZE run_gates;
