-- name: GetControllerCredentialForUpdate :one
SELECT cluster_name, sha256, enrolled_at
FROM controller_credentials
WHERE cluster_name = sqlc.arg(cluster_name)
FOR UPDATE;

-- name: CreateControllerCredential :execrows
INSERT INTO controller_credentials (
    cluster_name,
    sha256,
    enrolled_at
) VALUES (
    sqlc.arg(cluster_name),
    sqlc.arg(sha256),
    sqlc.arg(enrolled_at)
)
ON CONFLICT DO NOTHING;

-- name: GetControllerClusterByCredentialSHA256 :one
SELECT cluster_name
FROM controller_credentials
WHERE sha256 = sqlc.arg(sha256);
