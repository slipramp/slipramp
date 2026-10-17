#!/bin/sh
set -eu

: "${SLIPRAMP_SERVER_ADDRESS:?SLIPRAMP_SERVER_ADDRESS is required}"
: "${SLIPRAMP_CONTROLLER_ENROLLMENT_TOKEN:?SLIPRAMP_CONTROLLER_ENROLLMENT_TOKEN is required}"
: "${SLIPRAMP_ARTIFACT_TOKEN_SECRET:?SLIPRAMP_ARTIFACT_TOKEN_SECRET is required}"

repository_root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)

export SLIPRAMP_CONTROLLER_ENROLLMENT_TOKENS
SLIPRAMP_CONTROLLER_ENROLLMENT_TOKENS=$(jq -cn \
  --arg token "$SLIPRAMP_CONTROLLER_ENROLLMENT_TOKEN" \
  '{homelab: $token}')
export SLIPRAMP_PLACEMENTS_FILE="${SLIPRAMP_PLACEMENTS_FILE:-$repository_root/examples/kcloud-ops/placements.yaml}"

cd "$repository_root"
exec go run ./cmd/slipramp-server
