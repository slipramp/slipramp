-- name: RecordClusterObservation :exec
INSERT INTO clusters (
    name,
    first_seen_at,
    last_seen_at
) VALUES (
    sqlc.arg(name),
    sqlc.arg(observed_at),
    sqlc.arg(observed_at)
)
ON CONFLICT (name) DO UPDATE
SET last_seen_at = EXCLUDED.last_seen_at;

-- name: GetCluster :one
SELECT name, first_seen_at, last_seen_at
FROM clusters
WHERE name = sqlc.arg(name);

-- name: ListClusters :many
SELECT name, first_seen_at, last_seen_at
FROM clusters
ORDER BY name;

-- name: GetControllerMetrics :one
SELECT
    count(*) FILTER (WHERE last_seen_at >= sqlc.arg(connected_after))::bigint AS connected_count,
    max(last_seen_at)::timestamptz AS last_seen_at
FROM clusters;
