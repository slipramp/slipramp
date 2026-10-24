#!/usr/bin/env bash

set -Eeuo pipefail

trap 'status=$?; printf "acceptance command failed at %s:%s: %s\n" "${BASH_SOURCE[0]}" "$LINENO" "$BASH_COMMAND" >&2; exit "$status"' ERR

repository_root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
kind_bin=${SLIPRAMP_KIND_BIN:-"$repository_root/.bin/kind"}
helm_bin=${SLIPRAMP_HELM_BIN:-"$repository_root/.bin/helm"}
kubectl_bin=${SLIPRAMP_KUBECTL_BIN:-kubectl}
cluster_name=${SLIPRAMP_ACCEPTANCE_CLUSTER_NAME:-slipramp-acceptance}
server_port=${SLIPRAMP_ACCEPTANCE_SERVER_PORT:-18080}
web_port=${SLIPRAMP_ACCEPTANCE_WEB_PORT:-13000}
oidc_port=${SLIPRAMP_ACCEPTANCE_OIDC_PORT:-18081}
keep_cluster=${SLIPRAMP_ACCEPTANCE_KEEP_CLUSTER:-false}
kind_node_image=kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5
dex_image=ghcr.io/dexidp/dex:v2.45.1@sha256:8499afd690c437f52301efd2b05b2455da5bd2dfc20332cd697dc9937f808462
timeout_seconds=${SLIPRAMP_ACCEPTANCE_TIMEOUT_SECONDS:-180}

system_namespace=slipramp-system
run_namespace=slipramp-runs
server_url="http://127.0.0.1:$server_port"
web_url="http://127.0.0.1:$web_port"
oidc_host=acceptance-dex.slipramp-system.svc.cluster.local
product_api_key=
temporary_directory=$(mktemp -d "${TMPDIR:-/tmp}/slipramp-kind-acceptance.XXXXXX")
product_cookie_jar="$temporary_directory/oidc-cookies.txt"
cluster_created=false
server_forward_pid=
web_forward_pid=
oidc_forward_pid=

kubectl_command=("$kubectl_bin" --context "kind-$cluster_name")

log() {
  printf '==> %s\n' "$*"
}

stop_forward() {
  local process_id=${1:-}
  if [[ -n "$process_id" ]]; then
    kill "$process_id" 2>/dev/null || true
    wait "$process_id" 2>/dev/null || true
  fi
}

dump_diagnostics() {
  log "acceptance diagnostics"
  "${kubectl_command[@]}" get pods,jobs -A -o wide || true
  "${kubectl_command[@]}" -n "$system_namespace" describe pods || true

  local deployment
  for deployment in postgres rustfs acceptance-git acceptance-dex slipramp-server slipramp-web slipramp-controller; do
    printf '\n--- logs: deployment/%s ---\n' "$deployment"
    "${kubectl_command[@]}" -n "$system_namespace" logs \
      "deployment/$deployment" --all-containers --tail=-1 || true
  done

  local pod
  while IFS= read -r pod; do
    [[ -n "$pod" ]] || continue
    printf '\n--- logs: %s ---\n' "$pod"
    "${kubectl_command[@]}" -n "$run_namespace" logs "$pod" \
      --all-containers --tail=-1 || true
  done < <("${kubectl_command[@]}" -n "$run_namespace" get pods -o name 2>/dev/null)
}

cleanup() {
  local status=$?
  trap - EXIT
  set +e

  stop_forward "$server_forward_pid"
  stop_forward "$web_forward_pid"
  stop_forward "$oidc_forward_pid"

  if [[ "$status" -ne 0 && "$cluster_created" == true ]]; then
    dump_diagnostics
  fi

  if [[ "$cluster_created" == true && "$keep_cluster" != true ]]; then
    log "deleting kind cluster $cluster_name"
    "$kind_bin" delete cluster --name "$cluster_name"
  elif [[ "$cluster_created" == true ]]; then
    log "keeping kind cluster $cluster_name with context kind-$cluster_name"
  fi

  rm -rf "$temporary_directory"
  exit "$status"
}
trap cleanup EXIT

require_command() {
  local command=$1
  if ! command -v "$command" >/dev/null 2>&1; then
    printf 'required command is not available: %s\n' "$command" >&2
    exit 1
  fi
}

for required_command in curl docker git jq ssh-keygen "$kubectl_bin"; do
  require_command "$required_command"
done
if [[ ! -x "$kind_bin" ]]; then
  printf 'kind binary is not executable: %s\n' "$kind_bin" >&2
  exit 1
fi
if [[ ! -x "$helm_bin" ]]; then
  printf 'Helm binary is not executable: %s\n' "$helm_bin" >&2
  exit 1
fi
if "$kind_bin" get clusters | grep -Fxq "$cluster_name"; then
  printf 'kind cluster already exists: %s\n' "$cluster_name" >&2
  exit 1
fi

wait_for_http() {
  local url=$1
  local deadline=$((SECONDS + timeout_seconds))
  until curl --fail --silent --show-error "$url" >/dev/null 2>&1; do
    if ((SECONDS >= deadline)); then
      printf 'timed out waiting for %s\n' "$url" >&2
      return 1
    fi
    sleep 1
  done
}

start_server_forward() {
  stop_forward "$server_forward_pid"
  "${kubectl_command[@]}" -n "$system_namespace" port-forward \
    service/slipramp-server "$server_port:8080" \
    >"$temporary_directory/server-port-forward.log" 2>&1 &
  server_forward_pid=$!
  wait_for_http "$server_url/readyz"
}

start_web_forward() {
  stop_forward "$web_forward_pid"
  "${kubectl_command[@]}" -n "$system_namespace" port-forward \
    service/slipramp-web "$web_port:3000" \
    >"$temporary_directory/web-port-forward.log" 2>&1 &
  web_forward_pid=$!
  wait_for_http "$web_url/"
}

start_oidc_forward() {
  stop_forward "$oidc_forward_pid"
  "${kubectl_command[@]}" -n "$system_namespace" port-forward \
    service/acceptance-dex "$oidc_port:5556" \
    >"$temporary_directory/oidc-port-forward.log" 2>&1 &
  oidc_forward_pid=$!
  wait_for_http \
    "http://127.0.0.1:$oidc_port/dex/.well-known/openid-configuration"
}

create_product_api_key_via_oidc() {
  local headers="$temporary_directory/oidc-login-headers.txt"
  local response_body="$temporary_directory/oidc-response.json"
  local authorization_url
  local oauth_result principal_response key_response

  curl --fail --silent --show-error \
    --cookie "$product_cookie_jar" \
    --cookie-jar "$product_cookie_jar" \
    --dump-header "$headers" \
    --output /dev/null \
    "$web_url/api/v1/auth/login"
  authorization_url=$(awk '
    tolower($1) == "location:" {
      sub(/^[^:]*:[[:space:]]*/, "")
      sub(/\r$/, "")
      print
      exit
    }
  ' "$headers")
  test -n "$authorization_url"

  oauth_result=$(curl --silent --show-error --location \
    --cookie "$product_cookie_jar" \
    --cookie-jar "$product_cookie_jar" \
    --connect-to "$oidc_host:5556:127.0.0.1:$oidc_port" \
    --output "$response_body" \
    --write-out '%{http_code} %{url_effective}' \
    "${authorization_url}&connector_id=mock")
  if ! grep -Fq $'\tslipramp_session\t' "$product_cookie_jar"; then
    printf 'OIDC login ended at %s without a session cookie\n' \
      "$oauth_result" >&2
    jq -c . "$response_body" >&2 2>/dev/null || true
    return 1
  fi

  principal_response=$(curl --fail --silent --show-error \
    --cookie "$product_cookie_jar" \
    "$server_url/api/v1/auth/session")
  jq -e '.kind == "user" and .role == "admin"' \
    <<<"$principal_response" >/dev/null

  key_response=$(curl --fail --silent --show-error \
    --cookie "$product_cookie_jar" \
    -H 'Content-Type: application/json' \
    --data '{
      "name":"acceptance automation",
      "scopes":["slipramp:admin"]
    }' \
    "$server_url/api/v1/auth/api-keys")
  product_api_key=$(jq -er '.token' <<<"$key_response")
}

api_get() {
  curl --fail --silent --show-error \
    -H "Authorization: Bearer $product_api_key" \
    "$server_url$1"
}

api_get_with_key() {
  local api_key=$1
  local path=$2
  curl --fail --silent --show-error \
    -H "Authorization: Bearer $api_key" \
    "$server_url$path"
}

wait_for_cluster_inbox_problem() {
  local api_key=$1
  local deadline=$((SECONDS + timeout_seconds))
  local response problem_id
  while ((SECONDS < deadline)); do
    if response=$(api_get_with_key "$api_key" /api/v1/inbox 2>/dev/null); then
      problem_id=$(jq -r \
        '.problems[] | select(.kind == "cluster-unreachable" and .destination.id == "kind") | .id' \
        <<<"$response")
      if [[ -n "$problem_id" ]]; then
        printf '%s\n' "$problem_id"
        return 0
      fi
    fi
    sleep 1
  done

  printf 'cluster-unreachable Inbox problem did not appear\n' >&2
  printf '%s\n' "${response:-no response}" >&2
  return 1
}

wait_for_cluster_inbox_recovery() {
  local api_key=$1
  local deadline=$((SECONDS + timeout_seconds))
  local response problem_count
  while ((SECONDS < deadline)); do
    if response=$(api_get_with_key "$api_key" /api/v1/inbox 2>/dev/null); then
      problem_count=$(jq \
        '[.problems[] | select(.kind == "cluster-unreachable" and .destination.id == "kind")] | length' \
        <<<"$response")
      if [[ "$problem_count" == 0 ]]; then
        return 0
      fi
    fi
    sleep 1
  done

  printf 'cluster-unreachable Inbox problem did not recover\n' >&2
  printf '%s\n' "${response:-no response}" >&2
  return 1
}

create_run() {
  local action=$1
  local parameters=$2
  local inputs=$3
  local idempotency_key=$4
  local body
  body=$(jq -cn \
    --arg repositoryId "$repository_id" \
    --arg action "$action" \
    --argjson parameters "$parameters" \
    --argjson inputs "$inputs" \
    '{
      repositoryId: $repositoryId,
      target: "acceptance",
      action: $action,
      parameters: $parameters,
      inputs: $inputs
    }')

  curl --fail --silent --show-error \
    -H 'Content-Type: application/json' \
    -H "Authorization: Bearer $product_api_key" \
    -H "Idempotency-Key: $idempotency_key" \
    --data "$body" \
    "$server_url/api/v1/runs" |
    jq -er '.id'
}

wait_for_repository() {
  local deadline=$((SECONDS + timeout_seconds))
  local response status commit
  while ((SECONDS < deadline)); do
    if response=$(api_get /api/v1/repositories 2>/dev/null); then
      status=$(jq -r --arg id "$repository_id" \
        '.items[] | select(.id == $id) | .refresh.status' <<<"$response")
      commit=$(jq -r --arg id "$repository_id" \
        '.items[] | select(.id == $id) | .refresh.commit.sha // ""' <<<"$response")
      if [[ "$status" == succeeded && "$commit" == "$fixture_commit" ]]; then
        return 0
      fi
    fi
    sleep 1
  done

  printf 'repository did not refresh commit %s\n' "$fixture_commit" >&2
  printf '%s\n' "${response:-no response}" >&2
  return 1
}

wait_for_run_phase() {
  local run_id=$1
  local expected_phase=$2
  local deadline=$((SECONDS + timeout_seconds))
  local response phase outcome
  while ((SECONDS < deadline)); do
    if response=$(api_get "/api/v1/runs/$run_id" 2>/dev/null); then
      phase=$(jq -r '.phase' <<<"$response")
      outcome=$(jq -r '.outcome // ""' <<<"$response")
      if [[ "$phase" == "$expected_phase" ]]; then
        return 0
      fi
      if [[ "$phase" == finished ]]; then
        printf 'Run %s finished as %s before reaching %s\n' \
          "$run_id" "$outcome" "$expected_phase" >&2
        return 1
      fi
    fi
    sleep 1
  done

  printf 'Run %s did not reach phase %s\n' "$run_id" "$expected_phase" >&2
  printf '%s\n' "${response:-no response}" >&2
  return 1
}

wait_for_run_outcome() {
  local run_id=$1
  local expected_outcome=$2
  local deadline=$((SECONDS + timeout_seconds))
  local response phase outcome
  while ((SECONDS < deadline)); do
    if response=$(api_get "/api/v1/runs/$run_id" 2>/dev/null); then
      phase=$(jq -r '.phase' <<<"$response")
      outcome=$(jq -r '.outcome // ""' <<<"$response")
      if [[ "$phase" == finished ]]; then
        if [[ "$outcome" != "$expected_outcome" ]]; then
          printf 'Run %s outcome is %s, expected %s\n' \
            "$run_id" "$outcome" "$expected_outcome" >&2
          printf '%s\n' "$response" >&2
          return 1
        fi
        return 0
      fi
    fi
    sleep 1
  done

  printf 'Run %s did not finish as %s\n' "$run_id" "$expected_outcome" >&2
  printf '%s\n' "${response:-no response}" >&2
  return 1
}

cancel_run() {
  local run_id=$1
  curl --fail --silent --show-error -X POST \
    -H "Authorization: Bearer $product_api_key" \
    "$server_url/api/v1/runs/$run_id/cancel" >/dev/null
}

artifact_id() {
  local run_id=$1
  local name=$2
  api_get "/api/v1/runs/$run_id/artifacts" |
    jq -er --arg name "$name" '.items[] | select(.name == $name) | .id'
}

download_artifact() {
  local artifact=$1
  local destination=$2
  curl --fail --silent --show-error \
    -H "Authorization: Bearer $product_api_key" \
    "$server_url/api/v1/artifacts/$artifact/download" \
    --output "$destination"
}

assert_log_contains() {
  local run_id=$1
  local pattern=$2
  local log_id log_file
  log_id=$(artifact_id "$run_id" log)
  log_file="$temporary_directory/$run_id.log"
  download_artifact "$log_id" "$log_file"
  grep -Fq "$pattern" "$log_file"
}

wait_for_job_deletion() {
  local run_id=$1
  local deadline=$((SECONDS + timeout_seconds))
  local jobs
  while ((SECONDS < deadline)); do
    jobs=$("${kubectl_command[@]}" -n "$run_namespace" get jobs \
      -l "slipramp.com/run-id=$run_id" -o name)
    if [[ -z "$jobs" ]]; then
      return 0
    fi
    sleep 1
  done

  printf 'execution Job for Run %s was not deleted\n' "$run_id" >&2
  return 1
}

log "building Slipramp acceptance images"
for target in acceptance-git checkout controller harness kargo-tools server web; do
  docker build --target "$target" \
    --tag "slipramp-$target:acceptance" \
    "$repository_root"
done

dependency_images=(
  postgres:18.6-alpine3.24
  rustfs/rustfs:1.0.1
  rustfs/rc:v0.1.36
)
log "pulling acceptance dependency images"
for image in "${dependency_images[@]}"; do
  docker pull "$image"
done
docker pull "$dex_image"
docker tag "$dex_image" slipramp-acceptance-dex:acceptance

log "creating kind cluster $cluster_name"
"$kind_bin" create cluster \
  --name "$cluster_name" \
  --image "$kind_node_image" \
  --wait 2m
cluster_created=true

local_images=(
  slipramp-acceptance-dex:acceptance
  slipramp-acceptance-git:acceptance
  slipramp-checkout:acceptance
  slipramp-controller:acceptance
  slipramp-harness:acceptance
  slipramp-kargo-tools:acceptance
  slipramp-server:acceptance
  slipramp-web:acceptance
)
log "loading images into kind"
"$kind_bin" load docker-image --name "$cluster_name" \
  "${local_images[@]}" "${dependency_images[@]}"

log "building the local Git fixture"
fixture_directory="$temporary_directory/repository"
mkdir -p "$fixture_directory"
cp -R "$repository_root/deploy/e2e/kind/repository/." "$fixture_directory/"
git -C "$fixture_directory" init --quiet --initial-branch=main
git -C "$fixture_directory" config user.email acceptance@slipramp.test
git -C "$fixture_directory" config user.name 'Slipramp Acceptance'
git -C "$fixture_directory" add .
git -C "$fixture_directory" commit --quiet -m 'Acceptance fixture'
fixture_commit=$(git -C "$fixture_directory" rev-parse HEAD)
git -C "$fixture_directory" branch stage/acceptance/acceptance
git -C "$fixture_directory" branch kargo/promotion/acceptance-empty
git -C "$fixture_directory" checkout --quiet -b kargo/promotion/acceptance
printf 'image: acceptance:new\n' >"$fixture_directory/review/manifest.yaml"
git -C "$fixture_directory" commit --quiet -am 'Promotion candidate'
candidate_commit=$(git -C "$fixture_directory" rev-parse HEAD)
git -C "$fixture_directory" branch kargo/promotion/acceptance-operator
git -C "$fixture_directory" branch kargo/promotion/acceptance-automatic
git -C "$fixture_directory" branch kargo/promotion/acceptance-guarded
git -C "$fixture_directory" checkout --quiet main
git -C "$fixture_directory" bundle create \
  "$temporary_directory/repository.bundle" --all

client_key="$temporary_directory/client-key"
host_key="$temporary_directory/host-key"
ssh-keygen -q -t ed25519 -N '' -C slipramp-acceptance-client \
  -f "$client_key"
ssh-keygen -q -t ed25519 -N '' -C slipramp-acceptance-host \
  -f "$host_key"
git_host=acceptance-git.slipramp-system.svc.cluster.local
awk -v host="[$git_host]:2222" '{print host " " $1 " " $2}' \
  "$host_key.pub" >"$temporary_directory/known_hosts"
cat >"$temporary_directory/gitconfig" <<EOF
[core]
    sshCommand = ssh -i /var/run/slipramp/repository-credentials/acceptance/ssh-privatekey -o BatchMode=yes -o IdentitiesOnly=yes -o UserKnownHostsFile=/var/run/slipramp/repository-credentials/acceptance/known_hosts -o StrictHostKeyChecking=yes
EOF
cat >"$temporary_directory/sshd_config" <<'EOF'
Port 2222
ListenAddress 0.0.0.0
HostKey /etc/ssh/keys/ssh_host_ed25519_key
AuthorizedKeysFile .ssh/authorized_keys
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitRootLogin no
AllowUsers git
AllowTcpForwarding no
X11Forwarding no
PidFile /tmp/sshd.pid
EOF

log "deploying PostgreSQL and RustFS"
"${kubectl_command[@]}" apply -f \
  "$repository_root/deploy/e2e/kind/dependencies.yaml"
"${kubectl_command[@]}" -n "$system_namespace" rollout status \
  deployment/postgres --timeout="${timeout_seconds}s"
"${kubectl_command[@]}" -n "$system_namespace" rollout status \
  deployment/rustfs --timeout="${timeout_seconds}s"
"${kubectl_command[@]}" apply -f \
  "$repository_root/deploy/e2e/kind/rustfs-init.yaml"
"${kubectl_command[@]}" -n "$system_namespace" wait \
  --for=condition=complete job/rustfs-init --timeout="${timeout_seconds}s"

log "deploying the acceptance fixtures"
"${kubectl_command[@]}" apply -f \
  "$repository_root/deploy/e2e/kind/kargo-api.yaml"
"${kubectl_command[@]}" wait --for=condition=Established \
  crd/stages.kargo.akuity.io \
  crd/promotions.kargo.akuity.io \
  --timeout="${timeout_seconds}s"
"${kubectl_command[@]}" apply -f \
  "$repository_root/deploy/e2e/kind/kargo-stage.yaml"
"${kubectl_command[@]}" -n "$system_namespace" create configmap \
  acceptance-repository \
  --from-file="repository.bundle=$temporary_directory/repository.bundle" \
  --dry-run=client -o yaml |
  "${kubectl_command[@]}" apply -f -
"${kubectl_command[@]}" -n "$system_namespace" create configmap \
  acceptance-git-client-public \
  --from-file="authorized_key=$client_key.pub" \
  --dry-run=client -o yaml |
  "${kubectl_command[@]}" apply -f -
"${kubectl_command[@]}" -n "$system_namespace" create configmap \
  acceptance-git-sshd \
  --from-file="sshd_config=$temporary_directory/sshd_config" \
  --dry-run=client -o yaml |
  "${kubectl_command[@]}" apply -f -
"${kubectl_command[@]}" -n "$system_namespace" create secret generic \
  acceptance-git-host-keys \
  --from-file="ssh_host_ed25519_key=$host_key" \
  --from-file="ssh_host_ed25519_key.pub=$host_key.pub" \
  --dry-run=client -o yaml |
  "${kubectl_command[@]}" apply -f -
"${kubectl_command[@]}" -n "$system_namespace" create secret generic \
  acceptance-git-client \
  --from-file="gitconfig=$temporary_directory/gitconfig" \
  --from-file="ssh-privatekey=$client_key" \
  --from-file="known_hosts=$temporary_directory/known_hosts" \
  --dry-run=client -o yaml |
  "${kubectl_command[@]}" apply -f -
"${kubectl_command[@]}" -n "$run_namespace" create secret generic \
  acceptance-git-client \
  --from-file="ssh-privatekey=$client_key" \
  --from-file="known_hosts=$temporary_directory/known_hosts" \
  --dry-run=client -o yaml |
  "${kubectl_command[@]}" apply -f -
"${kubectl_command[@]}" -n "$run_namespace" create secret generic \
  acceptance-argocd \
  --from-literal='targets.json={"acceptance":{"server":"acceptance-argocd","authToken":"acceptance-argo-token"}}' \
  --dry-run=client -o yaml |
  "${kubectl_command[@]}" apply -f -
"${kubectl_command[@]}" apply -f \
  "$repository_root/deploy/e2e/kind/platform.yaml"
"${kubectl_command[@]}" -n "$system_namespace" set env \
  deployment/acceptance-dex \
  "SLIPRAMP_ACCEPTANCE_OIDC_REDIRECT_URL=$web_url/api/v1/auth/callback"
for deployment in acceptance-git acceptance-dex; do
  "${kubectl_command[@]}" -n "$system_namespace" rollout status \
    "deployment/$deployment" --timeout="${timeout_seconds}s"
done

log "installing Slipramp through the supported Helm charts"
"${kubectl_command[@]}" -n "$system_namespace" create secret generic \
  slipramp-server-secrets \
  --from-literal='database-url=postgres://slipramp:slipramp-acceptance@postgres:5432/slipramp?sslmode=disable' \
  --from-literal='s3-access-key-id=slipramp' \
  --from-literal='s3-secret-access-key=slipramp-acceptance' \
  --from-literal='controller-enrollment-tokens={"kind":"acceptance-enrollment-token"}' \
  --from-literal='artifact-token-secret=acceptance-artifact-token-secret-32' \
  --from-literal='oidc-client-secret=acceptance-client-secret' \
  --dry-run=client -o yaml |
  "${kubectl_command[@]}" apply -f -
"${kubectl_command[@]}" -n "$system_namespace" create secret generic \
  slipramp-controller-credentials \
  --from-literal='enrollment-token=acceptance-enrollment-token' \
  --from-literal='controller-token=acceptance-controller-token' \
  --dry-run=client -o yaml |
  "${kubectl_command[@]}" apply -f -

"$helm_bin" upgrade --install slipramp \
  "$repository_root/deploy/charts/slipramp" \
  --kube-context "kind-$cluster_name" \
  --namespace "$system_namespace" \
  --values "$repository_root/deploy/e2e/kind/slipramp-values.yaml" \
  --set-string "server.oidc.redirectURL=$web_url/api/v1/auth/callback" \
  --wait \
  --timeout "${timeout_seconds}s"
"$helm_bin" upgrade --install slipramp-controller \
  "$repository_root/deploy/charts/slipramp-controller" \
  --kube-context "kind-$cluster_name" \
  --namespace "$system_namespace" \
  --values "$repository_root/deploy/e2e/kind/controller-values.yaml" \
  --wait \
  --timeout "${timeout_seconds}s"
for deployment in slipramp-server slipramp-web slipramp-controller; do
  "${kubectl_command[@]}" -n "$system_namespace" rollout status \
    "deployment/$deployment" --timeout="${timeout_seconds}s"
done

start_server_forward
start_web_forward
start_oidc_forward

log "signing in through OIDC and creating the acceptance API key"
create_product_api_key_via_oidc

log "registering and refreshing the Git fixture"
repository_response=$(curl --fail-with-body --silent --show-error \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $product_api_key" \
  --data '{
    "name":"acceptance",
    "cloneUrl":"ssh://git@acceptance-git.slipramp-system.svc.cluster.local:2222/srv/repository.git",
    "branch":"main",
    "commitIndexing":"observedHeads",
    "refreshCredentialName":"acceptance",
    "checkoutCredentialName":"acceptance"
  }' \
  "$server_url/api/v1/repositories")
repository_id=$(jq -er '.id' <<<"$repository_response")
wait_for_repository

catalog_response=$(api_get "/api/v1/repositories/$repository_id/catalog")
jq -e --arg commit "$fixture_commit" '
  .commit.sha == $commit and
  any(.targets[]; .metadata.name == "acceptance") and
  any(.runtimes[]; .metadata.name == "acceptance")
' <<<"$catalog_response" >/dev/null

log "running preview and checking idempotency"
preview_parameters='{"message":"hello-kind"}'
preview_run=$(create_run preview "$preview_parameters" '{}' acceptance-preview)
preview_retry=$(create_run preview "$preview_parameters" '{}' acceptance-preview)
test "$preview_run" = "$preview_retry"
wait_for_run_outcome "$preview_run" succeeded
preview_response=$(api_get "/api/v1/runs/$preview_run")
jq -e --arg commit "$fixture_commit" '.commit.sha == $commit' \
  <<<"$preview_response" >/dev/null

plan_id=$(artifact_id "$preview_run" plan)
download_artifact "$plan_id" "$temporary_directory/plan.txt"
test "$(cat "$temporary_directory/plan.txt")" = 'plan:hello-kind'
assert_log_contains "$preview_run" 'acceptance preview complete: hello-kind'

log "applying the exact preview artifact"
apply_inputs=$(jq -cn --arg id "$plan_id" '{plan: $id}')
apply_run=$(create_run apply '{}' "$apply_inputs" acceptance-apply)
wait_for_run_outcome "$apply_run" succeeded
apply_response=$(api_get "/api/v1/runs/$apply_run")
jq -e \
  --arg commit "$(jq -r '.commit.sha' <<<"$preview_response")" \
  --arg revision "$(jq -r '.catalogRevisionId' <<<"$preview_response")" \
  --arg artifact "$plan_id" '
    .commit.sha == $commit and
    .catalogRevisionId == $revision and
    .inputs.plan.artifactId == $artifact
  ' <<<"$apply_response" >/dev/null
assert_log_contains "$apply_run" 'acceptance apply consumed: plan:hello-kind'

log "testing queued and running cancellation"
cancel_run_id=$(create_run cancel '{}' '{}' acceptance-running-cancel)
wait_for_run_phase "$cancel_run_id" running
queued_run=$(create_run preview "$preview_parameters" '{}' acceptance-queued-cancel)
wait_for_run_phase "$queued_run" queued
cancel_run "$queued_run"
wait_for_run_outcome "$queued_run" canceled
cancel_run "$cancel_run_id"
wait_for_run_outcome "$cancel_run_id" canceled
assert_log_contains "$cancel_run_id" 'acceptance cancellation observed'
wait_for_job_deletion "$cancel_run_id"

log "testing controller restart recovery"
controller_recovery_run=$(create_run recover '{}' '{}' acceptance-controller-recovery)
wait_for_run_phase "$controller_recovery_run" running
"${kubectl_command[@]}" -n "$system_namespace" rollout restart \
  deployment/slipramp-controller
"${kubectl_command[@]}" -n "$system_namespace" rollout status \
  deployment/slipramp-controller --timeout="${timeout_seconds}s"
wait_for_run_outcome "$controller_recovery_run" succeeded
assert_log_contains "$controller_recovery_run" 'acceptance recovery completed'

log "testing Server restart recovery"
server_recovery_run=$(create_run recover '{}' '{}' acceptance-server-recovery)
wait_for_run_phase "$server_recovery_run" running
"${kubectl_command[@]}" -n "$system_namespace" rollout restart \
  deployment/slipramp-server
"${kubectl_command[@]}" -n "$system_namespace" rollout status \
  deployment/slipramp-server --timeout="${timeout_seconds}s"
start_server_forward
wait_for_run_outcome "$server_recovery_run" succeeded
assert_log_contains "$server_recovery_run" 'acceptance recovery completed'

source "$repository_root/scripts/e2e/kargo-promotions.sh"

log "testing operational Inbox projection and personal acknowledgements"
inbox_principal_id=$(curl --fail-with-body --silent --show-error \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $product_api_key" \
  --data '{"displayName":"Inbox acceptance","role":"operator"}' \
  "$server_url/api/v1/service-accounts" | jq -er '.principalId')
inbox_api_key=$(curl --fail-with-body --silent --show-error \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $product_api_key" \
  --data '{"name":"Inbox acceptance","scopes":["slipramp:read","slipramp:inbox:write"]}' \
  "$server_url/api/v1/service-accounts/$inbox_principal_id/api-keys" | jq -er '.token')

"${kubectl_command[@]}" -n "$system_namespace" scale \
  deployment/slipramp-controller --replicas=0
first_inbox_problem=$(wait_for_cluster_inbox_problem "$product_api_key")
second_principal_problem=$(wait_for_cluster_inbox_problem "$inbox_api_key")
test "$first_inbox_problem" = "$second_principal_problem"

curl --fail --silent --show-error \
  --cookie "$product_cookie_jar" \
  "$web_url/" >"$temporary_directory/inbox-before.html"
grep -Fq 'kind is unreachable' "$temporary_directory/inbox-before.html"
grep -Fq 'Acknowledge all' "$temporary_directory/inbox-before.html"

curl --fail --silent --show-error \
  --cookie "$product_cookie_jar" \
  --data-urlencode "problemId=$first_inbox_problem" \
  "$web_url/?index" >/dev/null
api_get /api/v1/inbox |
  jq -e --arg id "$first_inbox_problem" \
    'all(.problems[]; .id != $id) and (.decisions | length) == 2' >/dev/null
api_get_with_key "$inbox_api_key" /api/v1/inbox |
  jq -e --arg id "$first_inbox_problem" 'any(.problems[]; .id == $id)' >/dev/null

"${kubectl_command[@]}" -n "$system_namespace" rollout restart deployment/slipramp-web
"${kubectl_command[@]}" -n "$system_namespace" rollout status \
  deployment/slipramp-web --timeout="${timeout_seconds}s"
start_web_forward
curl --fail --silent --show-error \
  --cookie "$product_cookie_jar" \
  "$web_url/" >"$temporary_directory/inbox-after.html"
if grep -Fq 'kind is unreachable' "$temporary_directory/inbox-after.html"; then
  printf 'acknowledged Inbox problem reappeared after Web restart\n' >&2
  exit 1
fi
grep -Fq 'Needs a decision' "$temporary_directory/inbox-after.html"

"${kubectl_command[@]}" -n "$system_namespace" scale \
  deployment/slipramp-controller --replicas=1
"${kubectl_command[@]}" -n "$system_namespace" rollout status \
  deployment/slipramp-controller --timeout="${timeout_seconds}s"
wait_for_cluster_inbox_recovery "$inbox_api_key"

"${kubectl_command[@]}" -n "$system_namespace" scale \
  deployment/slipramp-controller --replicas=0
recurrent_inbox_problem=$(wait_for_cluster_inbox_problem "$product_api_key")
test "$recurrent_inbox_problem" != "$first_inbox_problem"
"${kubectl_command[@]}" -n "$system_namespace" scale \
  deployment/slipramp-controller --replicas=1
"${kubectl_command[@]}" -n "$system_namespace" rollout status \
  deployment/slipramp-controller --timeout="${timeout_seconds}s"

log "kind acceptance passed"
printf 'Repository commit: %s\n' "$fixture_commit"
printf 'Preview Run:       %s\n' "$preview_run"
printf 'Apply Run:         %s\n' "$apply_run"
printf 'Canceled Run:      %s\n' "$cancel_run_id"
printf 'Controller recovery Run: %s\n' "$controller_recovery_run"
printf 'Server recovery Run:     %s\n' "$server_recovery_run"
