#!/bin/sh
set -eu

context="${SLIPRAMP_KUBE_CONTEXT:-homelab}"
controller_namespace=slipramp-system
run_namespace=slipramp-runs

require() {
  name="$1"
  eval "value=\${$name:-}"
  if [ -z "$value" ]; then
    echo "$name is required" >&2
    exit 64
  fi
}

require_digest() {
  name="$1"
  eval "reference=\${$name:-}"
  digest="${reference##*@sha256:}"
  if [ "$digest" = "$reference" ] ||
    ! printf '%s' "$digest" | grep -Eq '^[0-9a-f]{64}$'; then
    echo "$name must be an image reference pinned by sha256 digest" >&2
    exit 64
  fi
}

require SLIPRAMP_SERVER_URL
require SLIPRAMP_CONTROLLER_ENROLLMENT_TOKEN
require SLIPRAMP_CONTROLLER_TOKEN
require SLIPRAMP_GIT_SSH_KEY
require SLIPRAMP_GIT_KNOWN_HOSTS
require SLIPRAMP_HOST_SSH_KEY
require_digest SLIPRAMP_CONTROLLER_IMAGE
require_digest SLIPRAMP_CHECKOUT_IMAGE
require_digest SLIPRAMP_HARNESS_IMAGE

ghcr_docker_config="${SLIPRAMP_GHCR_DOCKER_CONFIG:-}"
ghcr_username="${SLIPRAMP_GHCR_USERNAME:-}"
ghcr_token="${SLIPRAMP_GHCR_TOKEN:-}"

if [ -n "$ghcr_docker_config" ]; then
  if [ ! -r "$ghcr_docker_config" ]; then
    echo "SLIPRAMP_GHCR_DOCKER_CONFIG must be a readable file" >&2
    exit 64
  fi

  if ! jq -e '
    (.auths["ghcr.io"] // .auths["https://ghcr.io"]) as $credential
    | (($credential.auth // "") | length > 0)
      or (($credential.identitytoken // "") | length > 0)
  ' "$ghcr_docker_config" >/dev/null; then
    echo "SLIPRAMP_GHCR_DOCKER_CONFIG must contain inline ghcr.io credentials" >&2
    exit 64
  fi
elif [ -z "$ghcr_username" ] || [ -z "$ghcr_token" ]; then
  echo "set SLIPRAMP_GHCR_DOCKER_CONFIG or both SLIPRAMP_GHCR_USERNAME and SLIPRAMP_GHCR_TOKEN" >&2
  exit 64
fi

case "$SLIPRAMP_SERVER_URL" in
  http://* | https://*) ;;
  *)
    echo "SLIPRAMP_SERVER_URL must be an absolute HTTP or HTTPS URL" >&2
    exit 64
    ;;
esac

repository_root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)

kubectl --context "$context" apply -k "$repository_root/deploy/e2e/homelab"

create_ghcr_pull_secret() {
  namespace="$1"

  if [ -n "$ghcr_docker_config" ]; then
    kubectl --context "$context" -n "$namespace" create secret generic ghcr-pull \
      --type=kubernetes.io/dockerconfigjson \
      --from-file=.dockerconfigjson="$ghcr_docker_config" \
      --dry-run=client \
      -o yaml
  else
    kubectl --context "$context" -n "$namespace" create secret docker-registry \
      ghcr-pull \
      --docker-server=ghcr.io \
      --docker-username="$ghcr_username" \
      --docker-password="$ghcr_token" \
      --dry-run=client \
      -o yaml
  fi
}

for namespace in "$controller_namespace" "$run_namespace"; do
  create_ghcr_pull_secret "$namespace" |
    kubectl --context "$context" apply -f -
done

kubectl --context "$context" -n "$controller_namespace" create secret generic \
  slipramp-controller-credentials \
  --from-literal=SLIPRAMP_CONTROLLER_ENROLLMENT_TOKEN="$SLIPRAMP_CONTROLLER_ENROLLMENT_TOKEN" \
  --from-literal=SLIPRAMP_CONTROLLER_TOKEN="$SLIPRAMP_CONTROLLER_TOKEN" \
  --dry-run=client \
  -o yaml |
  kubectl --context "$context" apply -f -

kubectl --context "$context" -n "$controller_namespace" create configmap \
  slipramp-controller-config \
  --from-literal=SLIPRAMP_CONTROLLER_CLUSTER=homelab \
  --from-literal=SLIPRAMP_CONTROLLER_SERVER_URL="$SLIPRAMP_SERVER_URL" \
  --from-literal=SLIPRAMP_CONTROLLER_CHECKOUT_IMAGE="$SLIPRAMP_CHECKOUT_IMAGE" \
  --from-literal=SLIPRAMP_CONTROLLER_HARNESS_IMAGE="$SLIPRAMP_HARNESS_IMAGE" \
  --from-literal=SLIPRAMP_CONTROLLER_GIT_CREDENTIALS='{"kcloud-ops":"kcloud-ops-git"}' \
  --dry-run=client \
  -o yaml |
  kubectl --context "$context" apply -f -

kubectl --context "$context" -n "$run_namespace" create secret generic \
  kcloud-ops-git \
  --from-file=ssh-privatekey="$SLIPRAMP_GIT_SSH_KEY" \
  --from-file=known_hosts="$SLIPRAMP_GIT_KNOWN_HOSTS" \
  --dry-run=client \
  -o yaml |
  kubectl --context "$context" apply -f -

kubectl --context "$context" -n "$run_namespace" create secret generic \
  kcloud-ops-ssh \
  --from-file=private-key="$SLIPRAMP_HOST_SSH_KEY" \
  --dry-run=client \
  -o yaml |
  kubectl --context "$context" apply -f -

kubectl --context "$context" -n "$controller_namespace" set image \
  deployment/slipramp-controller \
  controller="$SLIPRAMP_CONTROLLER_IMAGE"
kubectl --context "$context" -n "$controller_namespace" scale \
  deployment/slipramp-controller \
  --replicas=1
kubectl --context "$context" -n "$controller_namespace" rollout status \
  deployment/slipramp-controller \
  --timeout=2m
