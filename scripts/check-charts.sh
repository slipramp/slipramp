#!/usr/bin/env bash

set -euo pipefail

repository_root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
helm_bin=${SLIPRAMP_HELM_BIN:-helm}
server_chart="$repository_root/deploy/charts/slipramp"
controller_chart="$repository_root/deploy/charts/slipramp-controller"
testdata="$repository_root/deploy/charts/testdata"
test_directory=$(mktemp -d "${TMPDIR:-/tmp}/slipramp-chart-tests.XXXXXX")

cleanup() {
  rm -rf "$test_directory"
}
trap cleanup EXIT

fail() {
  printf 'chart check failed: %s\n' "$*" >&2
  exit 1
}

assert_contains() {
  local file=$1
  local pattern=$2
  rg -q -- "$pattern" "$file" || fail "$file does not contain $pattern"
}

assert_not_contains() {
  local file=$1
  local pattern=$2
  if rg -q -- "$pattern" "$file"; then
    fail "$file unexpectedly contains $pattern"
  fi
}

assert_render_fails() {
  local description=$1
  shift
  if "$helm_bin" template --kube-version 1.30.0 "$@" >"$test_directory/invalid.out" 2>&1; then
    fail "$description rendered successfully"
  fi
}

"$helm_bin" lint "$server_chart" --values "$testdata/slipramp-required.yaml"
"$helm_bin" lint "$controller_chart" --values "$testdata/controller-required.yaml"

server_render="$test_directory/server.yaml"
server_repeat="$test_directory/server-repeat.yaml"
"$helm_bin" template --kube-version 1.30.0 slipramp "$server_chart" \
  --namespace slipramp-system \
  --values "$testdata/slipramp-required.yaml" >"$server_render"
"$helm_bin" template --kube-version 1.30.0 slipramp "$server_chart" \
  --namespace slipramp-system \
  --values "$testdata/slipramp-required.yaml" >"$server_repeat"
cmp -s "$server_render" "$server_repeat" || fail "Server chart rendering is not deterministic"

assert_not_contains "$server_render" '^kind: Secret$'
assert_contains "$server_render" 'name: slipramp-server-secrets'
assert_contains "$server_render" 'automountServiceAccountToken: false'
assert_contains "$server_render" 'path: /healthz'
assert_contains "$server_render" 'path: /readyz'
assert_contains "$server_render" 'timeoutSeconds: 3'
assert_not_contains "$server_render" '^kind: (ClusterRole|Role|RoleBinding|ServiceAccount)$'

checksum_default=$(rg 'checksum/placements:' "$server_render" | awk '{print $2}')
"$helm_bin" template --kube-version 1.30.0 slipramp "$server_chart" \
  --namespace slipramp-system \
  --values "$testdata/slipramp-required.yaml" \
  --set-string server.placements.primary.jobNamespace=other-runs \
  >"$test_directory/server-placement.yaml"
checksum_changed=$(rg 'checksum/placements:' "$test_directory/server-placement.yaml" | awk '{print $2}')
test "$checksum_default" != "$checksum_changed" || fail "Placement change did not change the rollout checksum"

"$helm_bin" template --kube-version 1.30.0 slipramp "$server_chart" \
  --namespace slipramp-system \
  --values "$testdata/slipramp-required.yaml" \
  --values "$testdata/slipramp-scheduling.yaml" \
  >"$test_directory/server-scheduling.yaml"
assert_contains "$test_directory/server-scheduling.yaml" '^kind: Ingress$'
assert_contains "$test_directory/server-scheduling.yaml" 'ingressClassName: nginx'
assert_contains "$test_directory/server-scheduling.yaml" 'topologySpreadConstraints:'
assert_contains "$test_directory/server-scheduling.yaml" 'name: registry-credentials'
assert_contains "$test_directory/server-scheduling.yaml" 'kubernetes.io/os: linux'

"$helm_bin" template --kube-version 1.30.0 slipramp "$server_chart" \
  --namespace slipramp-system \
  --values "$testdata/slipramp-required.yaml" \
  --set-string server.artifactTokenSecret.value=acceptance-artifact-token-secret-32 \
  --set-string server.artifactTokenSecret.existingSecret.name= \
  --set-string server.artifactTokenSecret.existingSecret.key= \
  >"$test_directory/server-managed-secret.yaml"
assert_contains "$test_directory/server-managed-secret.yaml" '^kind: Secret$'
assert_contains "$test_directory/server-managed-secret.yaml" 'acceptance-artifact-token-secret-32'

controller_render="$test_directory/controller.yaml"
controller_repeat="$test_directory/controller-repeat.yaml"
"$helm_bin" template --kube-version 1.30.0 slipramp-controller "$controller_chart" \
  --namespace slipramp-system \
  --values "$testdata/controller-required.yaml" \
  --values "$testdata/controller-multiple-namespaces.yaml" \
  >"$controller_render"
"$helm_bin" template --kube-version 1.30.0 slipramp-controller "$controller_chart" \
  --namespace slipramp-system \
  --values "$testdata/controller-required.yaml" \
  --values "$testdata/controller-multiple-namespaces.yaml" \
  >"$controller_repeat"
cmp -s "$controller_render" "$controller_repeat" || fail "Controller chart rendering is not deterministic"

test "$(rg -c '^kind: Role$' "$controller_render")" = 2 || fail "Controller chart did not create one Role per execution namespace"
test "$(rg -c '^kind: RoleBinding$' "$controller_render")" = 2 || fail "Controller chart did not create one RoleBinding per execution namespace"
assert_not_contains "$controller_render" '^kind: ClusterRole'
assert_not_contains "$controller_render" '^kind: Secret$'
assert_contains "$controller_render" '^  replicas: 1$'
assert_contains "$controller_render" 'namespace: runs-blue'
assert_contains "$controller_render" 'namespace: runs-green'
assert_contains "$controller_render" 'value: "\[\\"runs-blue\\",\\"runs-green\\"\]"'
assert_contains "$controller_render" 'name: slipramp-controller-credentials'
assert_contains "$controller_render" 'github-readonly'

assert_render_fails "Missing required Server configuration" \
  invalid "$server_chart"
assert_render_fails "Server replica count greater than one" \
  invalid "$server_chart" --values "$testdata/slipramp-required.yaml" --set server.replicaCount=2
assert_render_fails "Server latest image" \
  invalid "$server_chart" --values "$testdata/slipramp-required.yaml" --set-string server.image.tag=latest
assert_render_fails "Server readiness timeout shorter than the application deadline" \
  invalid "$server_chart" --values "$testdata/slipramp-required.yaml" --set server.readinessProbe.timeoutSeconds=2
assert_render_fails "Missing Server database credential" \
  invalid "$server_chart" --values "$testdata/slipramp-required.yaml" \
  --set-string server.database.url.existingSecret.name= \
  --set-string server.database.url.existingSecret.key=
assert_render_fails "Ambiguous Server database credential" \
  invalid "$server_chart" --values "$testdata/slipramp-required.yaml" --set-string server.database.url.value=postgres-secret
assert_render_fails "Missing required Controller configuration" \
  invalid "$controller_chart"
assert_render_fails "Controller replica count greater than one" \
  invalid "$controller_chart" --values "$testdata/controller-required.yaml" --set replicaCount=2
assert_render_fails "Missing Controller credential" \
  invalid "$controller_chart" --values "$testdata/controller-required.yaml" \
  --set-string credentials.bearerToken.existingSecret.name= \
  --set-string credentials.bearerToken.existingSecret.key=
assert_render_fails "Controller without an execution namespace" \
  invalid "$controller_chart" --values "$testdata/controller-required.yaml" --set-json executionNamespaces='[]'
assert_render_fails "Controller latest Harness image" \
  invalid "$controller_chart" --values "$testdata/controller-required.yaml" --set-string harnessImage.tag=latest

printf 'Helm chart checks passed\n'
